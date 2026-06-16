#!/usr/bin/env python3
"""
Infer model architecture from a UVR checkpoint and write a complete model.yaml.

Usage:
    python3 infer_model_arch.py <model_directory>

Scans the directory for .ckpt or .pth files, reads the state dict keys
to detect architecture type (BSRoformer / MelBandRoformer) and extracts
all parameters needed by inference_universal.py.

Updates model.yaml in place, preserving any existing inference params.
"""

import sys, os, json, re, yaml

# ── Helpers ──────────────────────────────────────────────────────────────

def _read_state_dict_keys(path):
    """Load only state dict keys without full tensor data (fast, low memory)."""
    import torch
    # Use torch.load with weights_only to be safe; we only inspect keys
    ckpt = torch.load(path, map_location='cpu', weights_only=True)
    if isinstance(ckpt, dict):
        return ckpt
    # Some checkpoints package state_dict under a wrapper
    for key in ('state_dict', 'model', 'params', 'net', 'model_state_dict'):
        if hasattr(ckpt, key):
            return getattr(ckpt, key)
        if isinstance(ckpt, dict) and key in ckpt:
            return ckpt[key]
    return ckpt  # Hopefully a dict


def _detect_architecture(sd):
    """Detect BSRoformer vs MelBandRoformer from state dict keys."""
    # BSRoformer has band_split.to_features.N.1.weight
    # MelBandRoformer has similar but with mel-specific keys
    keys = set(sd.keys()) if isinstance(sd, dict) else set()

    has_band_split = any('band_split.to_features.' in k and '.1.weight' in k for k in keys)
    has_freq_indices = any('freq_indices' in k for k in keys)
    has_mel_filters = any('freqs_per_band' in k for k in keys)

    if has_band_split:
        if has_mel_filters and has_freq_indices:
            return 'mel_band_roformer'
        return 'bs_roformer'
    return 'unknown'


def _extract_bands(sd):
    """Extract freqs_per_bands from band_split.to_features layers."""
    bands = {}
    for k, v in sd.items():
        m = re.match(r'band_split\.to_features\.(\d+)\.1\.weight', k)
        if m:
            idx = int(m.group(1))
            dim_input = v.shape[1]  # = 2 * freqs * audio_channels
            bands[idx] = dim_input

    if not bands:
        return None, 1

    sorted_dims = [bands[i] for i in sorted(bands.keys())]

    # Detect stereo vs mono: sum should be 1025 (freq bins from STFT n_fft=2048)
    # Mono: dim_input = 2*freqs  →  sum(dim_input//2) = 1025
    # Stereo: dim_input = 4*freqs →  sum(dim_input//4) = 1025
    sum_mono = sum(d // 2 for d in sorted_dims)
    sum_stereo = sum(d // 4 for d in sorted_dims)

    if sum_stereo == 1025:
        return [d // 4 for d in sorted_dims], 2  # stereo
    elif sum_mono == 1025:
        return [d // 2 for d in sorted_dims], 1  # mono
    else:
        # Fallback: assume mono
        return [d // 2 for d in sorted_dims], 1


def _extract_dim(sd):
    """Extract transformer dimension from norm.gamma."""
    for k in sd:
        if re.match(r'layers\.\d+\.\d+\.layers\.\d+\.\d+\.norm\.gamma', k):
            return sd[k].shape[0]
        if re.match(r'layers\.\d+\.\d+\.norm\.gamma', k):
            return sd[k].shape[0]
        if re.match(r'final_norm\.gamma', k):
            return sd[k].shape[0]
    # Fallback: try to_qkv weight shape
    for k in sd:
        if re.match(r'layers\.\d+\.\d+\.layers\.\d+\.\d+\.to_qkv\.weight', k):
            return sd[k].shape[1]
    return None


def _extract_depth(sd):
    """Count layer groups (layers.N.*)."""
    indices = set()
    for k in sd:
        m = re.match(r'layers\.(\d+)\.', k)
        if m:
            indices.add(int(m.group(1)))
    return max(indices) + 1 if indices else None


def _extract_heads(sd):
    """Extract number of attention heads."""
    for k, v in sd.items():
        if re.match(r'layers\.\d+\.\d+\.layers\.\d+\.\d+\.to_gates\.weight', k):
            return v.shape[0]
        if re.match(r'layers\.\d+\.\d+\.to_gates\.weight', k):
            return v.shape[0]
    return None


def _extract_dim_head(sd, dim, heads):
    """Derive dim_head from to_qkv weight."""
    if not dim or not heads:
        return 64
    for k, v in sd.items():
        if re.match(r'layers\.\d+\.\d+\.layers\.\d+\.\d+\.to_qkv\.weight', k):
            return v.shape[0] // heads // 3
        if re.match(r'layers\.\d+\.\d+\.to_qkv\.weight', k):
            return v.shape[0] // heads // 3
    return 64


def _extract_num_stems(sd):
    """Count mask_estimators."""
    indices = set()
    for k in sd:
        m = re.match(r'mask_estimators\.(\d+)\.', k)
        if m:
            indices.add(int(m.group(1)))
    return max(indices) + 1 if indices else 1


def _extract_transformer_depths(sd):
    """Extract time_transformer_depth and freq_transformer_depth."""
    time_layers = set()
    freq_layers = set()
    for k in sd:
        m = re.match(r'layers\.0\.0\.layers\.(\d+)\.', k)
        if m:
            time_layers.add(int(m.group(1)))
        m = re.match(r'layers\.0\.1\.layers\.(\d+)\.', k)
        if m:
            freq_layers.add(int(m.group(1)))
    return (max(time_layers) + 1) if time_layers else 1, (max(freq_layers) + 1) if freq_layers else 1


# ── Main ─────────────────────────────────────────────────────────────────

def infer_model_arch(model_dir):
    """Infer model architecture from checkpoint and write enriched model.yaml."""
    # Find checkpoint file
    ckpt_path = None
    for fname in sorted(os.listdir(model_dir)):
        if fname.endswith('.ckpt') or fname.endswith('.pth'):
            ckpt_path = os.path.join(model_dir, fname)
            break

    if not ckpt_path:
        print(f"NO_CKPT: no .ckpt or .pth in {model_dir}")
        return False

    print(f"Reading {ckpt_path}...")

    try:
        sd = _read_state_dict_keys(ckpt_path)
    except Exception as e:
        print(f"CKPT_ERROR: {e}")
        return False

    arch = _detect_architecture(sd)
    if arch == 'unknown':
        print(f"UNKNOWN_ARCH: could not detect architecture from {ckpt_path}")
        return False

    print(f"Detected: {arch}")

    # Extract parameters
    dim = _extract_dim(sd)
    depth = _extract_depth(sd)
    heads = _extract_heads(sd)
    dim_head = _extract_dim_head(sd, dim, heads)
    num_stems = _extract_num_stems(sd)
    time_depth, freq_depth = _extract_transformer_depths(sd)
    freqs_per_bands, audio_channels = _extract_bands(sd)
    stereo = audio_channels == 2

    # Read existing YAML to preserve inference params
    existing = {}
    yaml_path = None
    for fname in os.listdir(model_dir):
        if fname.endswith('.yaml'):
            yaml_path = os.path.join(model_dir, fname)
            break

    if yaml_path:
        with open(yaml_path) as f:
            existing = yaml.full_load(f) or {}

    # Build the complete model config
    if arch == 'bs_roformer':
        model_config = {
            'model': {
                'type': 'bs_roformer',
                'dim': dim,
                'depth': depth,
                'stereo': stereo,
                'num_stems': num_stems,
                'time_transformer_depth': time_depth,
                'freq_transformer_depth': freq_depth,
                'heads': heads,
                'dim_head': dim_head,
                'freqs_per_bands': freqs_per_bands,
                'attn_dropout': 0.0,
                'ff_dropout': 0.0,
                'flash_attn': True,
            }
        }
    elif arch == 'mel_band_roformer':
        model_config = {
            'model': {
                'type': 'mel_band_roformer',
                'dim': dim,
                'depth': depth,
                'stereo': stereo,
                'num_stems': num_stems,
                'time_transformer_depth': time_depth,
                'freq_transformer_depth': freq_depth,
                'heads': heads,
                'dim_head': dim_head,
                'num_bands': len(freqs_per_bands) if freqs_per_bands else 60,
                'attn_dropout': 0.0,
                'ff_dropout': 0.0,
                'flash_attn': True,
            }
        }

    # Preserve existing inference params
    if 'inference' in existing:
        model_config['inference'] = existing['inference']
    else:
        model_config['inference'] = {'num_overlap': 4, 'batch_size': 1, 'dim_t': 256}

    if 'audio' in existing:
        model_config['audio'] = existing['audio']
    else:
        model_config['audio'] = {'hop_length': 512}

    if 'training' in existing:
        model_config['training'] = existing['training']
    else:
        model_config['training'] = {'instruments': ['vocals'], 'target_instrument': 'vocals'}

    # Write YAML
    yaml_path = yaml_path or os.path.join(model_dir, 'model.yaml')
    with open(yaml_path, 'w') as f:
        yaml.dump(model_config, f, default_flow_style=None, sort_keys=False, allow_unicode=True)

    print(f"YAML written: {yaml_path}")
    print(f"  Architecture: {arch}")
    print(f"  dim={dim}, depth={depth}, heads={heads}, dim_head={dim_head}")
    print(f"  stereo={stereo}, num_stems={num_stems}")
    if freqs_per_bands:
        print(f"  bands={len(freqs_per_bands)}, total_freqs={sum(freqs_per_bands)}")
    return True


if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("Usage: python3 infer_model_arch.py <model_directory>")
        sys.exit(1)

    model_dir = sys.argv[1]
    if not os.path.isdir(model_dir):
        print(f"Not a directory: {model_dir}")
        sys.exit(1)

    success = infer_model_arch(model_dir)
    sys.exit(0 if success else 1)
