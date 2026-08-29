"""onda mdx — MDX23C source separation.

Headless reimplementation of UVR's SeperateMDXC.demix() logic using the
TFC_TDF_net architecture from lib_v5/tfc_tdf_v3.py.
"""

import glob
import os
import sys
import json
import urllib.request
import warnings
from typing import Optional

import yaml
import torch
import numpy as np
import librosa
import soundfile as sf


def _dotdict(obj):
    """Recursively convert dicts to attribute-accessible objects.

    Mirrors ml_collections.ConfigDict so TFC_TDF_net can use
    ``config.audio.n_fft`` style access without adding a dependency.
    """
    if isinstance(obj, dict):
        return _DotDict({k: _dotdict(v) for k, v in obj.items()})
    if isinstance(obj, (list, tuple)):
        return type(obj)(_dotdict(v) for v in obj)
    return obj


class _DotDict:
    def __init__(self, data):
        self._data = data
        for k, v in data.items():
            setattr(self, k, v)

    def __getitem__(self, key):
        return self._data[key]

    def __contains__(self, key):
        return key in self._data

    def __repr__(self):
        return repr(self._data)


def _load_config(config_path: str):
    """Load a YAML config and return an attribute-accessible object."""
    with open(config_path) as f:
        raw = yaml.full_load(f)
    # Prefer ml_collections.ConfigDict when available (matches UVR behaviour).
    try:
        from ml_collections import ConfigDict
        return ConfigDict(raw)
    except Exception:
        return _dotdict(raw)


def _resolve_mdx_c_config(model_dir: str, ckpt_name: str, cache_root: Optional[str] = None):
    """Find or download the MDX-C YAML config for a checkpoint.

    UVR stores these configs under ``models/MDX_Net_Models/model_data/mdx_c_configs/``
    and downloads missing ones from the application_data repository.  We follow the
    same convention, but also accept a YAML placed next to the checkpoint.
    """
    base = os.path.splitext(ckpt_name)[0]

    # 1) YAML next to the checkpoint.
    local_candidates = [
        os.path.join(model_dir, f"{base}.yaml"),
        os.path.join(model_dir, f"{base}.yml"),
    ]
    for p in local_candidates:
        if os.path.isfile(p):
            return p

    # Generic YAML in the model directory.
    for ext in ("*.yaml", "*.yml"):
        for p in sorted(glob.glob(os.path.join(model_dir, ext))):
            return p

    # 2) UVR cache directory.
    if cache_root is None:
        repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
        cache_root = os.path.join(repo_root, "models", "MDX_Net_Models", "model_data", "mdx_c_configs")
    os.makedirs(cache_root, exist_ok=True)

    # Try to discover the config name from the online UVR catalog.
    config_name = _lookup_config_name(ckpt_name)
    if config_name:
        cached = os.path.join(cache_root, config_name)
        if os.path.isfile(cached):
            return cached
        url = f"https://raw.githubusercontent.com/TRvlvr/application_data/main/mdx_model_data/mdx_c_configs/{config_name}"
        try:
            urllib.request.urlretrieve(url, cached)
            if os.path.isfile(cached):
                return cached
        except Exception as e:
            print(f"   ⚠️  Could not download config {config_name}: {e}")

    return None


def _lookup_config_name(ckpt_name: str) -> Optional[str]:
    """Query TRvlvr's download_checks.json for the MDX-C config of a checkpoint."""
    catalog_url = "https://raw.githubusercontent.com/TRvlvr/application_data/main/filelists/download_checks.json"
    try:
        with urllib.request.urlopen(catalog_url, timeout=20) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except Exception:
        return None

    for section in ("mdx23_download_list", "mdx23c_download_list", "mdx23c_download_vip_list"):
        for entry in data.get(section, {}).values():
            if isinstance(entry, dict):
                for ckpt, config in entry.items():
                    if ckpt.lower() == ckpt_name.lower() and config.endswith((".yaml", ".yml")):
                        return config
    return None


def _write_progress(progress_file: Optional[str], chunk: int, total: int):
    if not progress_file:
        return
    progress = chunk / total if total > 0 else 0.0
    try:
        with open(progress_file, "w") as pf:
            pf.write(
                '{"step":"mdx","progress":%.4f,"chunk":%d,"total_chunks":%d}'
                % (progress, chunk, total)
            )
            pf.flush()
    except Exception:
        pass


def _write_pipeline_status(status_file: Optional[str], step: str, progress: float,
                           chunk: int, total: int, device: str):
    if not status_file:
        return
    try:
        if os.path.exists(status_file):
            with open(status_file) as f:
                data = json.load(f)
        else:
            data = {}
        data.update({
            "status": "running",
            "step": step,
            "progress": progress,
            "chunk": chunk,
            "total_chunks": total,
            "device": device,
        })
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


def _demix(mix: np.ndarray, config, model_path: str, device: torch.device,
           overlap: int = 8, batch_size: int = 1, segment_size: Optional[int] = None,
           progress_file: Optional[str] = None, pipeline_status: Optional[str] = None):
    """Run MDX-C inference with overlap-add chunking.

    Mirrors SeperateMDXC.demix() from separate.py.
    """
    project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    sys.path.insert(0, os.path.join(project_root, "lib_v5"))
    from lib_v5.tfc_tdf_v3 import TFC_TDF_net

    model = TFC_TDF_net(config, device=device)
    model.load_state_dict(torch.load(model_path, map_location="cpu"))
    model.to(device).eval()

    try:
        S = model.num_target_instruments
    except Exception:
        S = model.module.num_target_instruments

    dim_t = config.inference.dim_t if segment_size is None else segment_size
    hop_length = config.audio.hop_length
    chunk_size = hop_length * (dim_t - 1)
    hop_size = chunk_size // overlap

    mix_tensor = torch.tensor(mix, dtype=torch.float32)
    mix_shape = mix_tensor.shape[1]
    pad_size = hop_size - (mix_shape - chunk_size) % hop_size

    pad_front = torch.zeros(2, chunk_size - hop_size)
    pad_back = torch.zeros(2, pad_size + chunk_size - hop_size)
    mix_tensor = torch.cat([pad_front, mix_tensor, pad_back], dim=1)

    chunks = mix_tensor.unfold(1, chunk_size, hop_size).transpose(0, 1)
    batches = [chunks[i : i + batch_size] for i in range(0, len(chunks), batch_size)]

    X = torch.zeros(S, *mix_tensor.shape) if S > 1 else torch.zeros_like(mix_tensor)
    X = X.to(device)

    total_batches = len(batches)
    _write_progress(progress_file, 0, total_batches)
    _write_pipeline_status(pipeline_status, "mdx", 0.0, 0, total_batches, str(device))

    with torch.no_grad():
        cnt = 0
        for bidx, batch in enumerate(batches):
            x = model(batch.to(device))
            for w in x:
                X[..., cnt * hop_size : cnt * hop_size + chunk_size] += w
                cnt += 1
            _write_progress(progress_file, bidx + 1, total_batches)
            _write_pipeline_status(
                pipeline_status, "mdx",
                (bidx + 1) / total_batches if total_batches > 0 else 0.0,
                bidx + 1, total_batches, str(device)
            )
            if (bidx + 1) % 10 == 0:
                print(f"  {bidx + 1}/{total_batches} batches...")

    estimated_sources = X[..., chunk_size - hop_size : -(pad_size + chunk_size - hop_size)] / overlap
    del X
    torch.cuda.empty_cache()

    estimated_sources = estimated_sources.cpu().numpy()
    if S > 1:
        instruments = list(config.training.instruments)
        return {name: estimated_sources[i] for i, name in enumerate(instruments)}
    return estimated_sources


def run_mdx(args):
    """Run MDX-C separation from CLI args.

    Expected args attributes:
      - model: path to the .ckpt checkpoint (or directory containing it)
      - config: optional explicit YAML config path
      - input: input audio path
      - output: output directory
      - overlap: overlap factor (default 8)
      - batch_size: inference batch size (default 1)
      - device: "cuda" or "cpu" (default cuda)
      - progress_file: optional per-chunk progress JSON path
      - pipeline_status: optional pipeline_status.json path
    """
    warnings.filterwarnings("ignore")

    model_path = args.model
    if os.path.isdir(model_path):
        ckpts = sorted([f for f in os.listdir(model_path) if f.endswith(".ckpt")])
        if not ckpts:
            print(f"ERROR: No .ckpt found in {model_path}")
            sys.exit(1)
        model_dir = model_path
        ckpt_name = ckpts[0]
        model_path = os.path.join(model_dir, ckpt_name)
    else:
        model_dir = os.path.dirname(model_path)
        ckpt_name = os.path.basename(model_path)

    if not os.path.isfile(model_path):
        print(f"ERROR: Model not found: {model_path}")
        sys.exit(1)

    # Resolve config.
    config_path = args.config
    if not config_path:
        config_path = _resolve_mdx_c_config(model_dir, ckpt_name)
    if not config_path or not os.path.isfile(config_path):
        print(f"ERROR: No MDX-C YAML config found for {ckpt_name}")
        sys.exit(1)

    config = _load_config(config_path)

    device = torch.device(args.device if torch.cuda.is_available() else "cpu")
    print(f"🎛️  onda mdx — MDX-C")
    print(f"   Model: {ckpt_name}")
    print(f"   Config: {os.path.basename(config_path)}")
    print(f"   Device: {device}")

    audio, sr = _prepare_mix(args.input)
    print(f"   Audio: {audio.shape[1] / sr:.1f}s, {audio.shape[1]} samples")

    overlap = getattr(args, "overlap", 8)
    batch_size = getattr(args, "batch_size", 1)
    progress_file = getattr(args, "progress_file", None)
    pipeline_status = getattr(args, "pipeline_status", None)

    sources = _demix(
        audio, config, model_path, device,
        overlap=overlap, batch_size=batch_size,
        progress_file=progress_file, pipeline_status=pipeline_status
    )

    os.makedirs(args.output, exist_ok=True)
    basename = os.path.splitext(os.path.basename(args.input))[0]

    def _normalize_name(n: str) -> str:
        # Match inference_universal.py naming: lowercase vocals/instrumental.
        lower = n.lower()
        if lower in ("vocals", "vocal"):
            return "vocals"
        if lower in ("instrumental", "inst", "other"):
            return "instrumental"
        return lower

    if isinstance(sources, dict):
        for name, stem in sources.items():
            if stem.shape[0] == 1:
                stem = np.repeat(stem, 2, axis=0)
            out = os.path.join(args.output, f"{basename}_{_normalize_name(name)}.wav")
            sf.write(out, stem.T, sr)
            print(f"   ✓ {out}")
    else:
        stem = sources
        if stem.shape[0] == 1:
            stem = np.repeat(stem, 2, axis=0)
        target = config.training.get("target_instrument", "vocals")
        target = _normalize_name(target)
        out = os.path.join(args.output, f"{basename}_{target}.wav")
        sf.write(out, stem.T, sr)
        print(f"   ✓ {out}")
        # Derive instrumental via subtraction.
        if target == "vocals":
            inst = audio - stem
            out = os.path.join(args.output, f"{basename}_instrumental.wav")
            sf.write(out, inst.T, sr)
            print(f"   ✓ {out} (subtraction)")

    print(f"✅ Done! Output in {args.output}/")


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(
        description="Headless MDX-C (MDX-Net) source separation."
    )
    parser.add_argument("model", help="Model directory or .ckpt path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument("output", default="output_mdx", help="Output directory")
    parser.add_argument("overlap", nargs="?", type=int, default=8,
                        help="Overlap factor (default: 8)")
    parser.add_argument("--batch-size", type=int, default=1,
                        help="Inference batch size (default: 1)")
    parser.add_argument("--config", help="Explicit YAML config path")
    parser.add_argument("--device", default="cuda", choices=["cuda", "cpu"],
                        help="Device (default: cuda)")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    _args = parser.parse_args()
    run_mdx(_args)
