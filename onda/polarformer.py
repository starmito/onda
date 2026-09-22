"""onda polarformer — BS PolarFormer ONNX source separation.

Headless reimplementation of the BS PolarFormer ONNX inference from
bgkb/bs_polarformer (MIT).  STFT and iSTFT live outside the ONNX graph; the
model consumes interleaved stereo STFT features of shape ``[B, T, 4100]`` and
produces a complex mask of shape ``[B, 1, 2050, T, 2]``.

Audio flow for a chunk:

    stereo waveform (2, samples)
    → torch.stft (n_fft=2048, hop=512, win=2048)
    → real/imag (2, 1025, T, 2)
    → interleave freq×channel → (1, 2050, T, 2)
    → interleave real/imag per frame → (1, T, 4100)
    → ONNX
    → mask (1, 1, 2050, T, 2)
    → multiply complex mask with original STFT
    → iSTFT → vocals waveform
    → other = mix - vocals
"""

import glob
import json
import os
import sys
import time
import warnings
from typing import Any, Dict, Optional, Tuple

import numpy as np
import librosa
import soundfile as sf
import torch

# onnxruntime-gpu is installed under /opt/pytorch-backends/cuda inside the
# production image; on the host test runner it may be absent.  Allow import to
# fail gracefully so pure-logic tests can still import the module.
try:
    import onnxruntime as ort
except Exception:  # pragma: no cover - runtime dependency
    ort = None

# Make tools/progress_tracker.py importable from the onda package location.
_PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(_PROJECT_ROOT, 'tools'))
try:
    import progress_tracker
except Exception:  # pragma: no cover - tolerate missing tracker in isolated tests
    progress_tracker = None

_MODULE_START = time.time()


def _load_config(config_path: str) -> Dict[str, Any]:
    """Load a PolarFormer config (JSON or YAML) into a plain dict."""
    ext = os.path.splitext(config_path)[1].lower()
    with open(config_path, encoding="utf-8") as f:
        if ext in (".yaml", ".yml"):
            import yaml
            return yaml.full_load(f) or {}
        return json.load(f)


def _is_polarformer_config(cfg: Dict[str, Any]) -> bool:
    """Return True when the config belongs to a BS PolarFormer model.

    Detection priority:
      1. ``model.use_pope: True`` (the canonical PolarFormer flag).
      2. ``model.freqs_per_bands`` and ``model.use_pope`` present.
      3. The checkpoint/onnx name is matched elsewhere by the caller.
    """
    model = cfg.get("model", {}) if isinstance(cfg, dict) else {}
    if model.get("use_pope") is True:
        return True
    if "freqs_per_bands" in model and "use_pope" in model:
        return True
    return False


def _resolve_polarformer_config(
    model_dir: str, onnx_name: str, explicit_config: Optional[str] = None
) -> Optional[str]:
    """Find the PolarFormer YAML/JSON config for a model directory.

    Search order:
      1. Explicitly supplied config path.
      2. JSON/YAML next to the ONNX model with a matching base name.
      3. Any JSON/YAML in the model directory that looks like PolarFormer.
      4. model_configs/<model_name>.json under the repo root.
    """
    if explicit_config is not None and os.path.isfile(explicit_config):
        return explicit_config

    base = os.path.splitext(onnx_name)[0]

    # 1) Same base name.
    for ext in (".yaml", ".yml", ".json"):
        candidate = os.path.join(model_dir, f"{base}{ext}")
        if os.path.isfile(candidate):
            try:
                if _is_polarformer_config(_load_config(candidate)) or ext in (
                    ".yaml",
                    ".yml",
                ):
                    return candidate
            except Exception:
                pass

    # 2) Any config in the model directory.
    for pattern in ("*.yaml", "*.yml", "*.json"):
        for candidate in sorted(glob.glob(os.path.join(model_dir, pattern))):
            try:
                if _is_polarformer_config(_load_config(candidate)):
                    return candidate
            except Exception:
                pass

    # 3) Repo-level model_configs JSON (UVR-style fallback).
    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    model_name = os.path.basename(model_dir)
    for cfg_root in (
        os.path.join(repo_root, "config", "model_configs"),
        os.path.join(repo_root, "model_configs"),
    ):
        for candidate in (
            os.path.join(cfg_root, f"{model_name}.json"),
            os.path.join(cfg_root, f"{base}.json"),
        ):
            if os.path.isfile(candidate):
                return candidate

    return None


def _write_progress(progress_file: Optional[str], chunk: int, total: int):
    if not progress_file:
        return
    # Public progress values always use the 0-100 (percentage) convention.
    progress = (chunk / total * 100.0) if total > 0 else 0.0
    try:
        with open(progress_file, "w", encoding="utf-8") as pf:
            pf.write(
                '{"step":"polarformer","progress":%.4f,"chunk":%d,"total_chunks":%d}'
                % (progress, chunk, total)
            )
            pf.flush()
    except Exception:
        pass


def _write_pipeline_status(
    status_file: Optional[str],
    step_idx: int,
    total_steps: int,
    progress: float,
    chunk: int,
    total: int,
    device: str,
):
    """Report progress to pipeline_status.json through the tracker.

    ``progress`` is a 0-1 fraction and is converted to the tracker's 0-100
    contract before writing.
    """
    if not status_file or progress_tracker is None:
        return
    try:
        start_time = float(os.environ.get('PIPELINE_START_TIME', _MODULE_START))
        elapsed = time.time() - start_time
        progress_0_100 = progress * 100.0
        extra = {
            "chunk": chunk,
            "total_chunks": total,
            "device": str(device),
        }
        progress_tracker.update_step_status(
            status_file,
            step_idx,
            "processing",
            progress_0_100,
            elapsed,
            total_steps,
            extra=extra,
            step_name="polarformer",
        )
    except Exception:
        pass


def _stft_features(
    audio: torch.Tensor,
    n_fft: int,
    hop_length: int,
    win_length: int,
) -> Tuple[torch.Tensor, torch.Tensor, torch.Tensor]:
    """Compute the interleaved STFT features consumed by the ONNX model.

    Args:
        audio: (channels, samples) float32 waveform.
        n_fft, hop_length, win_length: STFT parameters.

    Returns:
        x: (1, T, F*channels*2) ONNX input, where F = n_fft//2+1.
        stft_repr: (1, F*channels, T, 2) real/imag representation kept for
            applying the mask.
        window: Hann window used for STFT/iSTFT.
    """
    channels = audio.shape[0]
    window = torch.hann_window(win_length, device=audio.device)
    stft_complex = torch.stft(
        audio,
        n_fft=n_fft,
        hop_length=hop_length,
        win_length=win_length,
        window=window,
        return_complex=True,
        center=True,
    )  # (channels, F, T)
    F, T = stft_complex.shape[-2], stft_complex.shape[-1]
    stft_real = torch.view_as_real(stft_complex)  # (channels, F, T, 2)
    # Interleave frequency per channel: (f0_L, f0_R, f1_L, f1_R, ...)
    stft_repr = stft_real.permute(1, 0, 2, 3).reshape(1, F * channels, T, 2)
    # Interleave real/imag per time frame.
    x = stft_repr.permute(0, 2, 1, 3).reshape(1, T, F * channels * 2)
    return x, stft_repr, window


def _apply_mask(
    stft_repr: torch.Tensor,
    mask: np.ndarray,
    n_fft: int,
    hop_length: int,
    win_length: int,
    window: torch.Tensor,
    length: int,
    zero_dc: bool = True,
) -> torch.Tensor:
    """Apply the ONNX mask and run iSTFT.

    Args:
        stft_repr: (1, F*channels, T, 2) real/imag STFT.
        mask: (1, 1, F*channels, T, 2) real/imag mask from ONNX.
        length: expected output length for each channel.

    Returns:
        waveform: (channels, length) float32.
    """
    device = stft_repr.device
    channels = stft_repr.shape[1] // (n_fft // 2 + 1)
    F = n_fft // 2 + 1

    mask_t = torch.from_numpy(mask).to(device)
    stft_c = torch.view_as_complex(stft_repr.unsqueeze(1).contiguous())  # (1,1,F*C,T)
    mask_c = torch.view_as_complex(mask_t.contiguous())  # (1,1,F*C,T)
    masked_c = stft_c * mask_c  # (1,1,F*C,T)

    # Split interleaved freq×channel back to (channels, F, T).
    masked_c = masked_c.squeeze(1)  # (1, F*C, T)
    T = masked_c.shape[-1]
    masked_c = masked_c.reshape(1, F, channels, T).permute(0, 2, 1, 3)  # (1, C, F, T)
    masked_c = masked_c.reshape(channels, F, T)

    if zero_dc:
        masked_c[:, 0, :] = 0.0

    recon = torch.istft(
        masked_c,
        n_fft=n_fft,
        hop_length=hop_length,
        win_length=win_length,
        window=window,
        return_complex=False,
        length=length,
    )  # (channels, length)
    return recon


class PolarFormerONNX:
    """BS PolarFormer ONNX inference with optional chunked overlap-add."""

    def __init__(
        self,
        config: Dict[str, Any],
        model_path: str,
        device: torch.device,
    ):
        self.config = config
        self.model_path = model_path
        self.device = device

        model = config.get("model", {})
        audio = config.get("audio", {})
        inference = config.get("inference", {})

        self.sr = int(audio.get("sample_rate", 44100))
        self.n_fft = int(model.get("stft_n_fft", audio.get("n_fft", 2048)))
        self.hop_length = int(model.get("stft_hop_length", audio.get("hop_length", 512)))
        self.win_length = int(model.get("stft_win_length", audio.get("n_fft", 2048)))
        self.stereo = bool(model.get("stereo", True))
        self.chunk_size = int(inference.get("chunk_size", 882000))
        self.num_overlap = int(inference.get("num_overlap", 2))
        self.batch_size = int(inference.get("batch_size", 4))

        if ort is None:
            raise RuntimeError("onnxruntime is not installed")

        providers = (
            ["CUDAExecutionProvider", "CPUExecutionProvider"]
            if torch.cuda.is_available() and str(device) != "cpu"
            else ["CPUExecutionProvider"]
        )
        self.session = ort.InferenceSession(model_path, providers=providers)
        inputs = self.session.get_inputs()
        self.input_name = inputs[0].name

    def _run_onnx(self, x: torch.Tensor) -> np.ndarray:
        """Run a single ONNX inference on CPU/GPU-agnostic input."""
        return self.session.run(None, {self.input_name: x.cpu().numpy()})[0]

    def demix(
        self,
        audio: np.ndarray,
        progress_file: Optional[str] = None,
        pipeline_status: Optional[str] = None,
        step_idx: int = 0,
        total_steps: int = 1,
    ) -> np.ndarray:
        """Run PolarFormer ONNX separation on a stereo waveform.

        Args:
            audio: (channels, samples) float32 waveform.

        Returns:
            vocals: (channels, samples) float32 waveform.
        """
        total_samples = audio.shape[-1]
        sr = self.sr
        step = self.chunk_size // self.num_overlap
        if step == 0:
            step = self.chunk_size

        result = np.zeros((audio.shape[0], total_samples), dtype=np.float32)
        count = np.zeros(total_samples, dtype=np.float32)

        # Build chunks.
        chunks = []
        positions = []
        for start in range(0, total_samples, step):
            end = min(start + self.chunk_size, total_samples)
            chunk = audio[:, start:end]
            if chunk.shape[-1] < self.chunk_size:
                pad = np.zeros(
                    (chunk.shape[0], self.chunk_size - chunk.shape[-1]),
                    dtype=np.float32,
                )
                chunk = np.concatenate([chunk, pad], axis=-1)
            chunks.append(chunk)
            positions.append((start, end))

        total_chunks = len(chunks)
        _write_progress(progress_file, 0, total_chunks)
        _write_pipeline_status(
            pipeline_status, step_idx, total_steps, 0.0,
            0, total_chunks, str(self.device)
        )

        for batch_start in range(0, total_chunks, self.batch_size):
            batch_end = min(batch_start + self.batch_size, total_chunks)
            for idx in range(batch_start, batch_end):
                chunk = chunks[idx]
                start, end = positions[idx]
                actual_len = end - start

                chunk_t = torch.tensor(chunk, dtype=torch.float32, device=self.device)
                x, stft_repr, window = _stft_features(
                    chunk_t, self.n_fft, self.hop_length, self.win_length
                )
                mask = self._run_onnx(x)
                recon = _apply_mask(
                    stft_repr,
                    mask,
                    self.n_fft,
                    self.hop_length,
                    self.win_length,
                    window,
                    length=chunk.shape[-1],
                    zero_dc=True,
                )
                recon_np = recon.cpu().numpy()

                result[:, start:end] += recon_np[:, :actual_len]
                count[start:end] += 1.0

            done = batch_end
            if done % max(1, self.batch_size) == 0 or done == total_chunks:
                print(f"  {done}/{total_chunks} chunks...")
            _write_progress(progress_file, done, total_chunks)
            _write_pipeline_status(
                pipeline_status,
                step_idx,
                total_steps,
                done / total_chunks if total_chunks > 0 else 0.0,
                done,
                total_chunks,
                str(self.device),
            )

        count = np.maximum(count, 1.0)
        result = result / count[np.newaxis, :]
        return result


def _prepare_audio(audio_path: str, sr: int):
    """Load audio as (channels, samples) float32 at the target sample rate."""
    audio, loaded_sr = librosa.load(audio_path, sr=sr, mono=False)
    if audio.ndim == 1:
        audio = np.stack([audio, audio], axis=0)
    elif audio.shape[0] > 2:
        audio = audio[:2]
    return audio.astype(np.float32), loaded_sr


def run_polarformer(args):
    """Run PolarFormer ONNX separation from CLI args.

    Expected args attributes:
      - model: path to the .onnx model or directory containing it.
      - config: optional explicit JSON/YAML config path.
      - input: input audio path.
      - output: output directory.
      - overlap: integer overlap factor (default 2).
      - device: "cuda" or "cpu" (default cuda).
      - progress_file: optional per-chunk progress JSON path.
      - pipeline_status: optional pipeline_status.json path.
      - chunk_size: optional override in samples.
      - batch_size: optional override.
    """
    warnings.filterwarnings("ignore")

    model_path = args.model
    if os.path.isdir(model_path):
        onnx_files = sorted([f for f in os.listdir(model_path) if f.endswith(".onnx")])
        if not onnx_files:
            print(f"ERROR: No .onnx found in {model_path}")
            sys.exit(1)
        model_dir = model_path
        onnx_name = onnx_files[0]
        model_path = os.path.join(model_dir, onnx_name)
    else:
        model_dir = os.path.dirname(model_path)
        onnx_name = os.path.basename(model_path)

    if not os.path.isfile(model_path):
        print(f"ERROR: Model not found: {model_path}")
        sys.exit(1)

    config_path = _resolve_polarformer_config(
        model_dir, onnx_name, getattr(args, "config", None)
    )
    if not config_path or not os.path.isfile(config_path):
        print(f"ERROR: No PolarFormer config found for {onnx_name}")
        print(
            "       Place the model YAML/JSON next to the ONNX file, or name the "
            "directory after the model (e.g. bs_polarformer)."
        )
        sys.exit(1)

    config = _load_config(config_path)
    device = torch.device(args.device if torch.cuda.is_available() else "cpu")

    print("🎛️  onda polarformer — BS PolarFormer ONNX")
    print(f"   Model: {onnx_name}")
    print(f"   Config: {os.path.basename(config_path)}")
    print(f"   Device: {device}")

    separator = PolarFormerONNX(config, model_path, device)

    audio, sr = _prepare_audio(args.input, separator.sr)
    print(f"   Audio: {audio.shape[-1] / sr:.1f}s, {audio.shape[-1]} samples")

    # Apply CLI overrides.
    overlap = getattr(args, "overlap", None)
    if overlap is not None:
        try:
            separator.num_overlap = max(1, int(overlap))
        except (TypeError, ValueError):
            pass
    chunk_size = getattr(args, "chunk_size", None)
    if chunk_size is not None:
        try:
            separator.chunk_size = max(1, int(chunk_size))
        except (TypeError, ValueError):
            pass
    batch_size = getattr(args, "batch_size", None)
    if batch_size is not None:
        try:
            separator.batch_size = max(1, int(batch_size))
        except (TypeError, ValueError):
            pass

    print(
        f"   Params: chunk_size={separator.chunk_size}, "
        f"num_overlap={separator.num_overlap}, batch_size={separator.batch_size}"
    )

    vocals = separator.demix(
        audio,
        progress_file=getattr(args, "progress_file", None),
        pipeline_status=getattr(args, "pipeline_status", None),
        step_idx=getattr(args, "step_idx", 0),
        total_steps=getattr(args, "total_steps", 1),
    )

    os.makedirs(args.output, exist_ok=True)
    basename = os.path.splitext(os.path.basename(args.input))[0]

    stem = vocals
    if stem.shape[0] == 1:
        stem = np.repeat(stem, 2, axis=0)
    out_vocals = os.path.join(args.output, f"{basename}_vocals.wav")
    sf.write(out_vocals, stem.T, sr)
    print(f"   ✓ {out_vocals}")

    other = audio - stem
    out_other = os.path.join(args.output, f"{basename}_instrumental.wav")
    sf.write(out_other, other.T, sr)
    print(f"   ✓ {out_other} (subtraction)")

    print(f"✅ Done! Output in {args.output}/")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Headless BS PolarFormer ONNX source separation."
    )
    parser.add_argument("model", help="Model directory or .onnx path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument(
        "output", nargs="?", default="output_polarformer", help="Output directory"
    )
    parser.add_argument(
        "overlap", nargs="?", type=int, default=2, help="Overlap factor (default: 2)"
    )
    parser.add_argument(
        "--device", default="cuda", choices=["cuda", "cpu"], help="Device (default: cuda)"
    )
    parser.add_argument("--config", help="Explicit JSON/YAML config path")
    parser.add_argument("--chunk-size", type=int, help="Chunk size in samples")
    parser.add_argument("--batch-size", type=int, help="Inference batch size")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    _args = parser.parse_args()
    run_polarformer(_args)
