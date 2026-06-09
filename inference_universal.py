#!/usr/bin/env python3
"""Headless RoFormer inference — supports MelBand & BS RoFormer.
Usage: python3 inference_universal.py [model_dir] [input_audio] [output_dir]"""
import sys, os, yaml, warnings
import torch, torch.nn as nn
import numpy as np
import librosa, soundfile as sf

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from lib_v5.mel_band_roformer import MelBandRoformer
from lib_v5.bs_roformer import BSRoformer
warnings.filterwarnings("ignore")

def _write_progress(progress_file, chunk, total):
    """Write per-chunk progress to a JSON file for real-time tracking.
    Format: {"step": "viperx", "progress": 0.45, "chunk": 45, "total_chunks": 100}
    """
    progress = chunk / total if total > 0 else 0.0
    try:
        with open(progress_file, 'w') as pf:
            pf.write('{"step":"viperx","progress":%.4f,"chunk":%d,"total_chunks":%d}' % (progress, chunk, total))
            pf.flush()
    except Exception:
        pass  # Non-critical; don't crash the pipeline over a progress write failure

def separate(model_dir, input_path, output_dir="output", progress_file=None, num_overlap=None):
    ckpts = sorted([f for f in os.listdir(model_dir) if f.endswith('.ckpt')])
    yamls = sorted([f for f in os.listdir(model_dir) if f.endswith('.yaml')])
    if not ckpts or not yamls:
        print(f"ERROR: No .ckpt/.yaml in {model_dir}"); return False
    
    ckpt_name = ckpts[0]
    prefix = ckpt_name.replace('.ckpt', '')
    yaml_name = next((y for y in yamls if y.startswith(prefix)), yamls[0])
    
    with open(os.path.join(model_dir, yaml_name)) as f:
        config = yaml.full_load(f)
    
    # Detect model type
    if 'num_bands' in config['model']:
        model_cls, model_type = MelBandRoformer, "MelBandRoformer"
    elif 'freqs_per_bands' in config['model']:
        model_cls, model_type = BSRoformer, "BSRoformer"
    else:
        print("ERROR: Unknown model"); return False
    
    print(f"Model: {ckpt_name} | Type: {model_type}")
    
    device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
    print(f"Device: {device}")
    
    model = model_cls(**config['model'])
    ckpt = torch.load(os.path.join(model_dir, ckpt_name), map_location='cpu', weights_only=True)
    model.load_state_dict(ckpt, strict=False)
    model = model.to(device).eval()
    print(f"Params: {sum(p.numel() for p in model.parameters())/1e6:.1f}M")
    
    # Load audio
    audio, sr = librosa.load(input_path, sr=44100, mono=False)
    if audio.ndim == 1:
        audio = np.stack([audio, audio], axis=0)
    elif audio.shape[0] > 2:
        audio = audio[:2]
    print(f"Audio: {audio.shape[1]/sr:.1f}s, {audio.shape[1]} samples")
    
    mix = torch.tensor(audio, dtype=torch.float32).to(device)
    
    dim_t = _CLI_DIM_T if hasattr(sys.modules[__name__], '_CLI_DIM_T') and _CLI_DIM_T is not None else config['inference']['dim_t']
    chunk_size = dim_t
    hop = config['audio']['hop_length']
    C = hop * (chunk_size - 1)
    overlap = num_overlap if num_overlap is not None else (int(sys.argv[4]) if len(sys.argv) > 4 else config['inference']['num_overlap'])
    step = C // overlap
    batch_size = _CLI_BATCH_SIZE if hasattr(sys.modules[__name__], '_CLI_BATCH_SIZE') and _CLI_BATCH_SIZE is not None else config['inference']['batch_size']
    
    instruments = config['training'].get('instruments', ['vocals', 'other'])
    num_stems = config['model'].get('num_stems', 1)
    target = config['training'].get('target_instrument', instruments[0] if instruments else 'vocals')
    
    # FIX: if num_stems=1, only target stem exists — the rest is derived via subtraction
    S = num_stems
    print(f"Stems: {S} (target: {target}), dim_t: {dim_t}, chunk: {C}samples, step: {step}, overlap: {overlap}, batch: {batch_size}")
    
    pad_len = C - step
    if audio.shape[1] > 2 * pad_len:
        mix = nn.functional.pad(mix, (pad_len, pad_len), mode='reflect')
    
    result = torch.zeros((S,) + tuple(mix.shape), dtype=torch.float32, device=device)
    counter = torch.zeros((S,) + tuple(mix.shape), dtype=torch.float32, device=device)
    
    fade_size = C // 10
    fadein = torch.linspace(0, 1, fade_size, device=device)
    fadeout = torch.linspace(1, 0, fade_size, device=device)
    
    batch_data, batch_starts = [], []
    total = int(np.ceil(mix.shape[1] / step))
    chunk_idx = 0
    
    print(f"Processing {total} chunks...")
    # Write initial progress
    if progress_file:
        _write_progress(progress_file, 0, total)
    
    with torch.inference_mode():
        i = 0
        while i < mix.shape[1]:
            part = mix[:, i:i + C]
            length = part.shape[-1]
            if length < C:
                part = nn.functional.pad(part, (0, C - length),
                    mode='reflect' if length > C//2+1 else 'constant', value=0)
            
            batch_data.append(part)
            batch_starts.append((i, length))
            i += step
            
            if len(batch_data) >= batch_size or i >= mix.shape[1]:
                arr = torch.stack(batch_data, dim=0)
                x = model(arr)
                if isinstance(x, dict):
                    x = x.get('output', x.get('audio', list(x.values())[0]))
                
                for j in range(len(batch_starts)):
                    start, orig_len = batch_starts[j]
                    win = torch.ones(C, device=device)
                    if chunk_idx > 0:
                        win[:fade_size] *= fadein
                    if start + C < mix.shape[1] - C + step:
                        win[-fade_size:] *= fadeout
                    
                    for s in range(S):
                        stem_out = x[j, s:s+1] if x.dim() >= 3 else x[j]
                        if stem_out.dim() == 1:
                            stem_out = stem_out.unsqueeze(0)
                        out_len = stem_out.shape[-1]
                        common = min(out_len, C, result.shape[-1] - start)
                        end = start + common
                        result[s, :, start:end] += stem_out[:, :common] * win[:common]
                        counter[s, :, start:end] += win[:common]
                    chunk_idx += 1
                
                # Write progress on EVERY chunk (not every 10) for real-time tracking
                if chunk_idx % 10 == 0:
                    print(f"  {chunk_idx}/{total} chunks...")
                if progress_file:
                    _write_progress(progress_file, chunk_idx, total)
                batch_data, batch_starts = [], []
    
    result = result / (counter + 1e-8)
    if audio.shape[1] > 2 * pad_len:
        result = result[:, :, pad_len:-pad_len]
    result = result.cpu().numpy()
    
    os.makedirs(output_dir, exist_ok=True)
    basename = os.path.splitext(os.path.basename(input_path))[0]
    
    # Save model output stems
    for s in range(S):
        stem = result[s]
        if stem.shape[0] == 1:
            stem = np.repeat(stem, 2, axis=0)
        name = target if S == 1 else instruments[s] if s < len(instruments) else f"stem_{s}"
        out = os.path.join(output_dir, f"{basename}_{name}.wav")
        sf.write(out, stem.T, sr)
        print(f"  ✓ {out}")
    
    # Derive instrumental via subtraction if model extracts vocals
    if target.lower() == 'vocals' or 'vocals' in [ins.lower() for ins in instruments]:
        inst = audio - result[0]
        out = os.path.join(output_dir, f"{basename}_instrumental.wav")
        sf.write(out, inst.T, sr)
        print(f"  ✓ {out} (subtraction)")
    
    print("Done!")
    return True

if __name__ == '__main__':
    # Parse args: model_dir input_path output_dir [overlap] [dim_t] [--batch-size N] [--progress-file FILE]
    args = sys.argv[1:]
    progress_file = None
    cli_batch_size = None
    cli_dim_t = None
    
    # Parse named flags
    filtered = []
    i = 0
    while i < len(args):
        if args[i] == '--progress-file' and i+1 < len(args):
            progress_file = args[i+1]
            i += 2
        elif args[i] == '--batch-size' and i+1 < len(args):
            cli_batch_size = int(args[i+1])
            i += 2
        elif args[i] == '--dim-t' and i+1 < len(args):
            cli_dim_t = int(args[i+1])
            i += 2
        else:
            filtered.append(args[i])
            i += 1
    args = filtered

    model_dir = args[0] if len(args) > 0 else 'models/VR_Models/MelBand_Karaoke'
    input_path = args[1] if len(args) > 1 else 'prueba_onda.mp3'
    output_dir = args[2] if len(args) > 2 else 'output'
    
    # Inject CLI overrides into separate() via globals
    import inference_universal
    inference_universal._CLI_BATCH_SIZE = cli_batch_size
    inference_universal._CLI_DIM_T = cli_dim_t
    num_overlap = int(args[3]) if len(args) > 3 else None
    
    sys.exit(0 if separate(model_dir, input_path, output_dir, progress_file, 
                          num_overlap=num_overlap) else 1)