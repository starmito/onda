"""onda onnx_mdx — MDXNet ONNX source separation.

Headless reimplementation of UVR's SeperateMDX logic for .onnx models using
onnxruntime and the STFT helpers from lib_v5.
"""

import glob
import json
import os
import sys
import urllib.request
import warnings
from typing import Any, Dict, Optional

import numpy as np
import librosa
import soundfile as sf
import torch

# onnxruntime may not be present in the host test runner; allow import to fail
# gracefully so pure-logic tests (config resolution, CLI parsing, etc.) can run.
try:
    import onnxruntime as ort
except Exception:  # pragma: no cover - runtime dependency
    ort = None


def _load_config(config_path: str) -> Dict[str, Any]:
    """Load a MDXNet config (JSON or YAML) into a plain dict."""
    ext = os.path.splitext(config_path)[1].lower()
    with open(config_path) as f:
        if ext in (".yaml", ".yml"):
            import yaml
            return yaml.full_load(f) or {}
        return json.load(f)


def _has_onnx_config_fields(cfg: Dict[str, Any]) -> bool:
    """Return True if the dict contains the fields required for ONNX inference."""
    keys = ("dim_f", "dim_t", "n_fft", "hop_length")
    if all(k in cfg for k in keys):
        return True
    # Also accept nested audio/inference style used by MDX-C YAMLs.
    audio = cfg.get("audio", {})
    inference = cfg.get("inference", {})
    dim_f = audio.get("dim_f", cfg.get("dim_f"))
    dim_t = inference.get("dim_t", cfg.get("dim_t"))
    n_fft = audio.get("n_fft", cfg.get("n_fft"))
    hop_length = audio.get("hop_length", inference.get("hop_length", cfg.get("hop_length")))
    return dim_f is not None and dim_t is not None and n_fft is not None and hop_length is not None


def _lookup_onnx_config_name(onnx_name: str) -> Optional[str]:
    """Query TRvlvr's download_checks.json for the MDXNet config of a model."""
    catalog_url = (
        "https://raw.githubusercontent.com/TRvlvr/application_data/main/filelists/"
        "download_checks.json"
    )
    try:
        with urllib.request.urlopen(catalog_url, timeout=20) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except Exception:
        return None

    for section in ("mdx_download_list", "mdx_download_vip_list"):
        for entry in data.get(section, {}).values():
            if not isinstance(entry, dict):
                continue
            for model_name, config_name in entry.items():
                if model_name.lower() == onnx_name.lower() and config_name.endswith(
                    (".json", ".yaml", ".yml")
                ):
                    return config_name
    return None


def _resolve_onnx_config(
    model_dir: str, onnx_name: str, cache_root: Optional[str] = None
) -> Optional[str]:
    """Find or download the MDXNet config for an ONNX model.

    Search order:
      1. JSON/YAML next to the model with matching base name.
      2. Any JSON/YAML in the model directory that contains MDXNet fields.
      3. UVR cache directory, downloading from the application_data repo if needed.
    """
    base = os.path.splitext(onnx_name)[0]

    # 1) Config next to the model.
    for ext in (".json", ".yaml", ".yml"):
        candidate = os.path.join(model_dir, f"{base}{ext}")
        if os.path.isfile(candidate):
            try:
                if _has_onnx_config_fields(_load_config(candidate)):
                    return candidate
            except Exception:
                pass

    # 2) Any config in the model directory.
    for pattern in ("*.json", "*.yaml", "*.yml"):
        for candidate in sorted(glob.glob(os.path.join(model_dir, pattern))):
            try:
                if _has_onnx_config_fields(_load_config(candidate)):
                    return candidate
            except Exception:
                pass

    # 3) UVR cache directory.
    if cache_root is None:
        repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
        cache_root = os.path.join(
            repo_root, "models", "MDX_Net_Models", "model_data", "mdx_net_configs"
        )
    os.makedirs(cache_root, exist_ok=True)

    config_name = _lookup_onnx_config_name(onnx_name)
    if config_name:
        cached = os.path.join(cache_root, config_name)
        if os.path.isfile(cached):
            return cached
        url = (
            "https://raw.githubusercontent.com/TRvlvr/application_data/main/"
            f"mdx_model_data/mdx_net_configs/{config_name}"
        )
        try:
            urllib.request.urlretrieve(url, cached)
            if os.path.isfile(cached):
                return cached
        except Exception as e:
            print(f"   ⚠️  Could not download config {config_name}: {e}")

    return None


def _write_progress(progress_file: Optional[str], chunk: int, total: int):
    if not progress_file:
        return
    progress = chunk / total if total > 0 else 0.0
    try:
        with open(progress_file, "w") as pf:
            pf.write(
                '{"step":"mdxnet","progress":%.4f,"chunk":%d,"total_chunks":%d}'
                % (progress, chunk, total)
            )
            pf.flush()
    except Exception:
        pass


def _write_pipeline_status(
    status_file: Optional[str],
    step: str,
    progress: float,
    chunk: int,
    total: int,
    device: str,
):
    if not status_file:
        return
    try:
        if os.path.exists(status_file):
            with open(status_file) as f:
                data = json.load(f)
        else:
            data = {}
        data.update(
            {
                "status": "running",
                "step": step,
                "progress": progress,
                "chunk": chunk,
                "total_chunks": total,
                "device": device,
            }
        )
        with open(status_file, "w") as f:
            json.dump(data, f)
            f.flush()
    except Exception:
        pass


def _prepare_mix(audio_path: str):
    """Load audio as (channels, samples) float32 at 44.1 kHz."""
    mix, sr = librosa.load(audio_path, mono=False, sr=44100)
    if mix.ndim == 1:
        mix = np.stack([mix, mix], axis=0)
    elif mix.shape[0] > 2:
        mix = mix[:2]
    return mix.astype(np.float32), sr


class OnnxMDX:
    """MDXNet ONNX inference with overlap-add chunking.

    Mirrors UVR's SeperateMDX behaviour for .onnx checkpoints: STFT → ONNX
    session → ISTFT with Hann-windowed overlap-add.
    """

    def __init__(
        self,
        config: Dict[str, Any],
        model_path: str,
        device: torch.device,
        overlap: float = 0.25,
    ):
        self.config = config
        self.model_path = model_path
        self.device = device
        self.overlap = overlap
        self.compensate = config.get("compensate", 1.0)

        # Config may be flat (UVR JSON) or nested (audio/inference).
        if "audio" in config and "inference" in config:
            audio = config["audio"]
            inference = config["inference"]
            self.dim_f = int(audio["dim_f"])
            self.dim_t = int(inference["dim_t"])
            self.n_fft = int(audio["n_fft"])
            self.hop_length = int(audio["hop_length"])
        else:
            self.dim_f = int(config["dim_f"])
            self.dim_t = int(config["dim_t"])
            self.n_fft = int(config["n_fft"])
            self.hop_length = int(config["hop_length"])

        self.adjust = 1.0
        self.initialize_model_settings()

        if ort is None:
            raise RuntimeError("onnxruntime is not installed")

        providers = (
            ["CUDAExecutionProvider", "CPUExecutionProvider"]
            if torch.cuda.is_available() and str(device) != "cpu"
            else ["CPUExecutionProvider"]
        )
        self.session = ort.InferenceSession(model_path, providers=providers)
        self.input_name = self.session.get_inputs()[0].name

    def initialize_model_settings(self):
        self.n_bins = self.n_fft // 2 + 1
        self.trim = self.n_fft // 2
        self.chunk_size = self.hop_length * (self.dim_t - 1)
        self.gen_size = self.chunk_size - 2 * self.trim
        self.stft = STFT(self.n_fft, self.hop_length, self.dim_f, self.device)

    def demix(
        self,
        mix: np.ndarray,
        progress_file: Optional[str] = None,
        pipeline_status: Optional[str] = None,
    ) -> np.ndarray:
        """Run MDXNet ONNX separation with overlap-add chunking.

        Returns the target stem waveform with shape (1, channels, samples).
        """
        org_mix = mix
        chunk_size = self.chunk_size
        gen_size = self.gen_size

        pad = gen_size + self.trim - ((mix.shape[-1]) % gen_size)
        mixture = np.concatenate(
            (
                np.zeros((2, self.trim), dtype="float32"),
                mix,
                np.zeros((2, pad), dtype="float32"),
            ),
            1,
        )

        step = int((1 - self.overlap) * chunk_size)
        if step == 0:
            step = chunk_size

        result = np.zeros((1, 2, mixture.shape[-1]), dtype=np.float32)
        divider = np.zeros((1, 2, mixture.shape[-1]), dtype=np.float32)
        total_chunks = (mixture.shape[-1] + step - 1) // step

        _write_progress(progress_file, 0, total_chunks)
        _write_pipeline_status(
            pipeline_status, "mdxnet", 0.0, 0, total_chunks, str(self.device)
        )

        for chunk_idx, i in enumerate(range(0, mixture.shape[-1], step)):
            start = i
            end = min(i + chunk_size, mixture.shape[-1])
            chunk_size_actual = end - start

            window = np.hanning(chunk_size_actual)
            window = np.tile(window[None, None, :], (1, 2, 1))

            mix_part_ = mixture[:, start:end]
            if end != i + chunk_size:
                pad_size = (i + chunk_size) - end
                mix_part_ = np.concatenate(
                    (mix_part_, np.zeros((2, pad_size), dtype="float32")), axis=-1
                )

            mix_part = torch.tensor([mix_part_], dtype=torch.float32).to(self.device)

            with torch.no_grad():
                tar_waves = self.run_model(mix_part)

            tar_waves[..., :chunk_size_actual] *= window
            divider[..., start:end] += window
            result[..., start:end] += tar_waves[..., : end - start]

            _write_progress(progress_file, chunk_idx + 1, total_chunks)
            _write_pipeline_status(
                pipeline_status,
                "mdxnet",
                (chunk_idx + 1) / total_chunks if total_chunks > 0 else 0.0,
                chunk_idx + 1,
                total_chunks,
                str(self.device),
            )
            if (chunk_idx + 1) % 10 == 0:
                print(f"  {chunk_idx + 1}/{total_chunks} chunks...")

        tar_waves = result / divider
        tar_waves = tar_waves[:, :, self.trim : -self.trim]
        tar_waves = tar_waves[:, :, : mix.shape[-1]]
        source = tar_waves[:, 0:1]
        source = source * self.compensate
        return source

    def run_model(self, mix: torch.Tensor) -> np.ndarray:
        spek = self.stft(mix.to(self.device)) * self.adjust
        spek[:, :, :3, :] *= 0
        spec_pred = self.session.run(None, {self.input_name: spek.cpu().numpy()})[0]
        return self.stft.inverse(torch.tensor(spec_pred).to(self.device)).cpu().detach().numpy()


class STFT:
    """Minimal STFT helper matching lib_v5/tfc_tdf_v3.py."""

    def __init__(self, n_fft: int, hop_length: int, dim_f: int, device):
        self.n_fft = n_fft
        self.hop_length = hop_length
        self.window = torch.hann_window(window_length=self.n_fft, periodic=True)
        self.dim_f = dim_f
        self.device = device

    def __call__(self, x: torch.Tensor) -> torch.Tensor:
        window = self.window.to(x.device)
        batch_dims = x.shape[:-2]
        c, t = x.shape[-2:]
        x = x.reshape([-1, t])
        x = torch.stft(
            x,
            n_fft=self.n_fft,
            hop_length=self.hop_length,
            window=window,
            center=True,
            return_complex=False,
        )
        x = x.permute([0, 3, 1, 2])
        x = x.reshape([*batch_dims, c, 2, -1, x.shape[-1]])
        x = x.reshape([*batch_dims, c * 2, -1, x.shape[-1]])
        return x[..., : self.dim_f, :]

    def inverse(self, x: torch.Tensor) -> torch.Tensor:
        window = self.window.to(x.device)
        batch_dims = x.shape[:-3]
        c, f, t = x.shape[-3:]
        n = self.n_fft // 2 + 1
        f_pad = torch.zeros([*batch_dims, c, n - f, t]).to(x.device)
        x = torch.cat([x, f_pad], -2)
        x = x.reshape([*batch_dims, c // 2, 2, n, t])
        x = x.reshape([-1, 2, n, t])
        x = x.permute([0, 2, 3, 1])
        x = x[..., 0] + x[..., 1] * 1.0j
        x = torch.istft(
            x,
            n_fft=self.n_fft,
            hop_length=self.hop_length,
            window=window,
            center=True,
        )
        x = x.reshape([*batch_dims, 2, -1])
        return x


def run_onnx_mdx(args):
    """Run MDXNet ONNX separation from CLI args.

    Expected args attributes:
      - model: path to the .onnx model (or directory containing it)
      - config: optional explicit JSON/YAML config path
      - input: input audio path
      - output: output directory
      - overlap: overlap factor (default 0.25)
      - device: "cuda" or "cpu" (default cuda)
      - progress_file: optional per-chunk progress JSON path
      - pipeline_status: optional pipeline_status.json path
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

    # Resolve config.
    config_path = getattr(args, "config", None)
    if not config_path:
        config_path = _resolve_onnx_config(model_dir, onnx_name)
    if not config_path or not os.path.isfile(config_path):
        print(f"ERROR: No MDXNet config found for {onnx_name}")
        print(
            "       Place a JSON/YAML with dim_f, dim_t, n_fft and hop_length next to the model, "
            "or ensure the model name is present in the UVR catalog."
        )
        sys.exit(1)

    config = _load_config(config_path)
    if not _has_onnx_config_fields(config):
        print(f"ERROR: Config {config_path} is missing MDXNet fields (dim_f/dim_t/n_fft/hop_length)")
        sys.exit(1)

    device = torch.device(args.device if torch.cuda.is_available() else "cpu")
    print("🎛️  onda onnx_mdx — MDXNet ONNX")
    print(f"   Model: {onnx_name}")
    print(f"   Config: {os.path.basename(config_path)}")
    print(f"   Device: {device}")

    audio, sr = _prepare_mix(args.input)
    print(f"   Audio: {audio.shape[1] / sr:.1f}s, {audio.shape[1]} samples")

    overlap = getattr(args, "overlap", 0.25)
    if isinstance(overlap, int) and overlap > 1:
        # pipeline.sh passes an integer overlap factor (e.g. 4 → 0.25).
        overlap = 1.0 / overlap
    progress_file = getattr(args, "progress_file", None)
    pipeline_status = getattr(args, "pipeline_status", None)

    separator = OnnxMDX(config, model_path, device, overlap=overlap)
    source = separator.demix(audio, progress_file=progress_file, pipeline_status=pipeline_status)

    os.makedirs(args.output, exist_ok=True)
    basename = os.path.splitext(os.path.basename(args.input))[0]

    # UVR MDXNet ONNX models produce a single target stem (usually vocals).
    target = config.get("target_instrument", "vocals").lower()
    if "training" in config:
        target = config["training"].get("target_instrument", target).lower()

    stem = source[0]
    if stem.shape[0] == 1:
        stem = np.repeat(stem, 2, axis=0)
    out = os.path.join(args.output, f"{basename}_{target}.wav")
    sf.write(out, stem.T, sr)
    print(f"   ✓ {out}")

    if target == "vocals":
        inst = audio - stem
        out = os.path.join(args.output, f"{basename}_instrumental.wav")
        sf.write(out, inst.T, sr)
        print(f"   ✓ {out} (subtraction)")

    print(f"✅ Done! Output in {args.output}/")


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Headless MDXNet ONNX source separation.")
    parser.add_argument("model", help="Model directory or .onnx path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument("output", nargs="?", default="output_onnx_mdx", help="Output directory")
    parser.add_argument("overlap", nargs="?", type=float, default=0.25, help="Overlap factor")
    parser.add_argument("--config", help="Explicit JSON/YAML config path")
    parser.add_argument("--device", default="cuda", choices=["cuda", "cpu"], help="Device")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    _args = parser.parse_args()
    run_onnx_mdx(_args)
