"""onda vocal — RoFormer source separation.
Extracts vocals (or target stem) using overlap-add inference.
Supports MelBand RoFormer and BS RoFormer models.
"""

import os
import sys
import warnings

import yaml
import torch
import torch.nn as nn
import numpy as np
import librosa
import soundfile as sf


def _load_config_value(config, keys, default=None):
    """Read a nested value from a config dict using a list of keys."""
    value = config
    for key in keys:
        if isinstance(value, dict):
            value = value.get(key)
        else:
            return default
    return value if value is not None else default


def run_vocal(args):
    """Run RoFormer separation from CLI args."""
    model_path = args.model
    config_path = args.config

    if not os.path.isfile(model_path):
        print(f"ERROR: Model not found: {model_path}")
        sys.exit(1)

    model_dir = os.path.dirname(model_path)
    ckpt_name = os.path.basename(model_path)

    # Auto-detect config next to the checkpoint.
    if not config_path:
        prefix = ckpt_name.replace(".ckpt", "")
        candidates = [
            f for f in os.listdir(model_dir)
            if f.endswith(".yaml") and f.startswith(prefix)
        ]
        if not candidates:
            candidates = [
                f for f in os.listdir(model_dir) if f.endswith(".yaml")
            ]
        if not candidates:
            print(f"ERROR: No .yaml config found in {model_dir}")
            sys.exit(1)
        config_path = os.path.join(model_dir, candidates[0])

    # Load project module path
    project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    sys.path.insert(0, project_root)
    from lib_v5.mel_band_roformer import MelBandRoformer
    from lib_v5.bs_roformer import BSRoformer

    warnings.filterwarnings("ignore")

    with open(config_path) as f:
        config = yaml.full_load(f)

    # Detect model type
    if "num_bands" in config["model"]:
        model_cls = MelBandRoformer
        model_type = "MelBandRoformer"
    elif "freqs_per_bands" in config["model"]:
        model_cls = BSRoformer
        model_type = "BSRoformer"
    else:
        print("ERROR: Unknown model type in config")
        sys.exit(1)

    print(f"🌊 onda vocal — {model_type}")
    print(f"   Model: {ckpt_name}")
    print(f"   Config: {os.path.basename(config_path)}")

    device = torch.device(args.device if torch.cuda.is_available() else "cpu")
    print(f"   Device: {device}")

    # Load model
    model = model_cls(**config["model"])
    ckpt = torch.load(model_path, map_location="cpu", weights_only=True)
    model.load_state_dict(ckpt, strict=False)
    model = model.to(device).eval()
    print(f"   Params: {sum(p.numel() for p in model.parameters()) / 1e6:.1f}M")

    # Load audio
    audio, sr = librosa.load(args.input, sr=44100, mono=False)
    if audio.ndim == 1:
        audio = np.stack([audio, audio], axis=0)
    elif audio.shape[0] > 2:
        audio = audio[:2]
    print(f"   Audio: {audio.shape[1] / sr:.1f}s, {audio.shape[1]} samples")

    mix = torch.tensor(audio, dtype=torch.float32).to(device)

    # Inference params: CLI overrides take precedence, then model config.
    dim_t = getattr(args, "dim_t", 0)
    if dim_t is None or dim_t <= 0:
        dim_t = _load_config_value(config, ["inference", "dim_t"], 0)
    if dim_t is None or dim_t <= 0:
        dim_t = config["inference"]["dim_t"]

    batch_size = getattr(args, "batch_size", 0)
    if batch_size is None or batch_size <= 0:
        batch_size = _load_config_value(config, ["inference", "batch_size"], 1)

    chunk_size = getattr(args, "chunk_size", 0)
    if chunk_size is None or chunk_size < 0:
        chunk_size = _load_config_value(config, ["inference", "chunk_size"], 0)

    hop = config["audio"]["hop_length"]
    C = hop * (dim_t - 1)
    overlap = args.overlap
    step = C // overlap

    instruments = config["training"].get("instruments", ["vocals", "other"])
    num_stems = config["model"].get("num_stems", 1)
    target = config["training"].get(
        "target_instrument", instruments[0] if instruments else "vocals"
    )
    S = num_stems

    print(f"   Stems: {S} (target: {target})")
    print(f"   dim_t: {dim_t}, Chunk: {C} samples, Overlap: {overlap}, Step: {step}, Batch: {batch_size}, Chunk size: {chunk_size}")

    # External time-based chunking (seconds). 0 / missing = process whole song.
    chunk_seconds = float(chunk_size) if chunk_size and chunk_size > 0 else 0.0

    # Padding
    pad_len = C - step
    if audio.shape[1] > 2 * pad_len:
        mix = nn.functional.pad(mix, (pad_len, pad_len), mode="reflect")

    result = torch.zeros((S,) + tuple(mix.shape), dtype=torch.float32, device=device)
    counter = torch.zeros((S,) + tuple(mix.shape), dtype=torch.float32, device=device)

    fade_size = C // 10
    fadein = torch.linspace(0, 1, fade_size, device=device)
    fadeout = torch.linspace(1, 0, fade_size, device=device)

    def _process_full():
        batch_data, batch_starts = [], []
        total = int(np.ceil(mix.shape[1] / step))
        chunk_idx = 0

        print(f"   Processing {total} chunks...")

        with torch.inference_mode():
            i = 0
            while i < mix.shape[1]:
                part = mix[:, i : i + C]
                length = part.shape[-1]
                if length < C:
                    part = nn.functional.pad(
                        part,
                        (0, C - length),
                        mode="reflect" if length > C // 2 + 1 else "constant",
                        value=0,
                    )

                batch_data.append(part)
                batch_starts.append((i, length))
                i += step

                if len(batch_data) >= batch_size or i >= mix.shape[1]:
                    arr = torch.stack(batch_data, dim=0)
                    x = model(arr)
                    if isinstance(x, dict):
                        x = x.get("output", x.get("audio", list(x.values())[0]))

                    for j in range(len(batch_starts)):
                        start, orig_len = batch_starts[j]
                        win = torch.ones(C, device=device)
                        if chunk_idx > 0:
                            win[:fade_size] *= fadein
                        if start + C < mix.shape[1] - C + step:
                            win[-fade_size:] *= fadeout

                        for s in range(S):
                            stem_out = x[j, s : s + 1] if x.dim() >= 3 else x[j]
                            if stem_out.dim() == 1:
                                stem_out = stem_out.unsqueeze(0)
                            # Multi-stem models return [B, S, C, T]; collapse
                            # leading size-1 dims without swallowing real channels.
                            while stem_out.dim() > 2 and stem_out.shape[0] == 1:
                                stem_out = stem_out.squeeze(0)
                            out_len = stem_out.shape[-1]
                            common = min(out_len, C, result.shape[-1] - start)
                            end = start + common
                            result[s, :, start:end] += (
                                stem_out[:, :common] * win[:common]
                            )
                            counter[s, :, start:end] += win[:common]
                        chunk_idx += 1

                    if chunk_idx % 20 == 0:
                        print(f"   {chunk_idx}/{total} chunks...")
                    batch_data, batch_starts = [], []

    if chunk_seconds > 0:
        # Simple chunked processing: split audio into fixed-length windows.
        total_samples = audio.shape[1]
        chunk_samples = max(int(round(chunk_seconds * sr)), C + step)
        if chunk_samples < total_samples:
            print(f"   Time-based chunking: {chunk_seconds}s ({chunk_samples} samples)")
            num_chunks = int(np.ceil(total_samples / (chunk_samples - C)))
            result_global = np.zeros((S, audio.shape[0], total_samples), dtype=np.float32)
            for idx in range(num_chunks):
                start = idx * (chunk_samples - C)
                end = min(start + chunk_samples, total_samples)
                audio_chunk = audio[:, start:end]
                mix_chunk = torch.tensor(audio_chunk, dtype=torch.float32).to(device)
                chunk_pad = C - step
                if audio_chunk.shape[1] > 2 * chunk_pad:
                    mix_chunk = nn.functional.pad(mix_chunk, (chunk_pad, chunk_pad), mode="reflect")
                # Reset per-chunk accumulators
                result[:] = 0
                counter[:] = 0
                _process_full()
                if audio_chunk.shape[1] > 2 * chunk_pad:
                    result_chunk = result[:, :, chunk_pad:-chunk_pad].cpu().numpy()
                else:
                    result_chunk = result.cpu().numpy()
                result_global[:, :, start:end] = result_chunk[:, :, : end - start]
                print(f"   chunk {idx + 1}/{num_chunks} done")
            result = torch.tensor(result_global, dtype=torch.float32, device=device)
        else:
            _process_full()
    else:
        _process_full()

    if audio.shape[1] > 2 * pad_len:
        result = result[:, :, pad_len:-pad_len]
    result = result.cpu().numpy()

    os.makedirs(args.output, exist_ok=True)
    basename = os.path.splitext(os.path.basename(args.input))[0]

    # Save stems
    for s in range(S):
        stem = result[s]
        if stem.shape[0] == 1:
            stem = np.repeat(stem, 2, axis=0)
        name = (
            target
            if S == 1
            else (instruments[s] if s < len(instruments) else f"stem_{s}")
        )
        out = os.path.join(args.output, f"{basename}_{name}.wav")
        sf.write(out, stem.T, sr)
        print(f"   ✓ {out}")

    # Derive instrumental via subtraction
    stems_lower = [str(ins).lower() for ins in instruments]
    target_lower = str(target).lower() if target else ""
    if target_lower == "vocals" or "vocals" in stems_lower:
        v_idx = stems_lower.index("vocals") if "vocals" in stems_lower else 0
        inst = audio - result[v_idx]
        out = os.path.join(args.output, f"{basename}_instrumental.wav")
        sf.write(out, inst.T, sr)
        print(f"   ✓ {out} (subtraction)")

    print(f"✅ Done! Output in {args.output}/")
