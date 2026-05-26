#!/usr/bin/env python3
"""
MDX-Net ONNX inference for Onda.
Based on UVR-MDX-Net architecture (ConvTDFNet).
Uses overlap-add with fixed chunk size.
"""

import sys
import os
import numpy as np
import onnxruntime as ort
import soundfile as sf

# MDX-Net model parameters (for Kim_Vocal_2 / UVR_MDXNET_Main)
N_FFT = 6144
HOP = 1024
DIM_F = 3072  # N_FFT // 2
DIM_T = 256   # Fixed time frames per chunk
OVERLAP = 8   # Default overlap factor


def separate_mdx(model_path: str, input_path: str, output_dir: str, overlap: int = OVERLAP) -> None:
    """Separate vocals from instrumental using MDX-Net ONNX model."""
    
    # Load ONNX model
    providers = []
    if 'CUDAExecutionProvider' in ort.get_available_providers():
        providers.append('CUDAExecutionProvider')
        device = 'cuda'
    else:
        device = 'cpu'
    providers.append('CPUExecutionProvider')
    
    session = ort.InferenceSession(model_path, providers=providers)
    input_name = session.get_inputs()[0].name
    print(f"Device: {device}")
    print(f"Model input: {input_name}")
    
    # Load audio (stereo, 44.1kHz)
    audio, sr = sf.read(input_path)
    if audio.ndim == 1:
        audio = np.stack([audio, audio], axis=1)
    if sr != 44100:
        import librosa
        audio = librosa.resample(audio.T, orig_sr=sr, target_sr=44100).T
        sr = 44100
    audio = audio.T.astype(np.float32)  # (2, samples)
    
    num_samples = audio.shape[1]
    print(f"Audio: {num_samples/sr:.1f}s, {sr}Hz, {audio.shape[0]}ch")
    
    # Normalize
    peak = np.max(np.abs(audio))
    if peak > 0:
        audio = audio / peak
    
    # STFT for stereo
    n_frames = 1 + (num_samples - N_FFT) // HOP
    
    # Pre-allocate spectrograms
    spec_left = np.zeros((DIM_F, n_frames), dtype=np.complex64)
    spec_right = np.zeros((DIM_F, n_frames), dtype=np.complex64)
    
    # Manual STFT (to avoid librosa/torch overhead)
    window = np.hanning(N_FFT)
    for i in range(n_frames):
        start = i * HOP
        frame_l = audio[0, start:start+N_FFT] * window
        frame_r = audio[1, start:start+N_FFT] * window
        spec_left[:, i] = np.fft.rfft(frame_l)[:DIM_F]
        spec_right[:, i] = np.fft.rfft(frame_r)[:DIM_F]
    
    # Build 4-channel input: (real_left, imag_left, real_right, imag_right)
    input_4ch = np.stack([
        spec_left.real.astype(np.float32),
        spec_left.imag.astype(np.float32),
        spec_right.real.astype(np.float32),
        spec_right.imag.astype(np.float32),
    ], axis=0)  # (4, DIM_F, n_frames)
    
    # Pad to multiple of DIM_T
    pad_frames = (DIM_T - (n_frames % DIM_T)) % DIM_T
    if pad_frames > 0:
        input_4ch = np.pad(input_4ch, ((0, 0), (0, 0), (0, pad_frames)), mode='constant')
    
    total_chunks = input_4ch.shape[2] // DIM_T
    print(f"Total chunks: {total_chunks} ({DIM_T} frames each)")
    
    # Initialize output mask accumulator
    mask_accum = np.zeros((4, DIM_F, input_4ch.shape[2]), dtype=np.float32)
    
    for i in range(total_chunks):
        start = i * DIM_T
        chunk = input_4ch[:, :, start:start+DIM_T]  # (4, DIM_F, DIM_T)
        chunk_input = chunk[np.newaxis, :, :, :]  # (1, 4, DIM_F, DIM_T)
        
        result = session.run(None, {input_name: chunk_input})
        mask_accum[:, :, start:start+DIM_T] = result[0][0]  # (4, DIM_F, DIM_T)
    
    # Remove padding
    if pad_frames > 0:
        mask_accum = mask_accum[:, :, :n_frames]
    
    # Apply mask to spectrograms
    mask_vocals = mask_accum[:2]  # (2, DIM_F, n_frames) — real+imag para vocals izquierdo
    mask_vocals_r = mask_accum[2:]  # real+imag para vocals derecho
    
    vocals_left_spec = spec_left * (mask_vocals[0] + 1j * mask_vocals[1])
    vocals_right_spec = spec_right * (mask_vocals_r[0] + 1j * mask_vocals_r[1])
    
    # ISTFT
    vocals_left = np.zeros(num_samples, dtype=np.float32)
    vocals_right = np.zeros(num_samples, dtype=np.float32)
    norm = np.zeros(num_samples, dtype=np.float32)
    
    for i in range(n_frames):
        start = i * HOP
        frame_l = np.fft.irfft(vocals_left_spec[:, i], n=N_FFT)[:N_FFT] * window
        frame_r = np.fft.irfft(vocals_right_spec[:, i], n=N_FFT)[:N_FFT] * window
        vocals_left[start:start+N_FFT] += frame_l
        vocals_right[start:start+N_FFT] += frame_r
        norm[start:start+N_FFT] += window ** 2
    
    # Normalize by window overlap
    mask_nz = norm > 1e-8
    vocals_left[mask_nz] /= norm[mask_nz]
    vocals_right[mask_nz] /= norm[mask_nz]
    
    # Build stereo output
    vocals = np.stack([vocals_left, vocals_right], axis=1)
    instrumental = audio.T[:num_samples] - vocals
    
    # Denormalize
    vocals = vocals * peak
    instrumental = instrumental * peak
    
    # Save
    base = os.path.splitext(os.path.basename(input_path))[0]
    os.makedirs(output_dir, exist_ok=True)
    
    vocal_path = os.path.join(output_dir, f"{base}_Vocals.wav")
    inst_path = os.path.join(output_dir, f"{base}_instrumental.wav")
    
    sf.write(vocal_path, vocals, sr)
    sf.write(inst_path, instrumental, sr)
    
    print(f"Output: {vocal_path} ({os.path.getsize(vocal_path)/1024:.0f}KB)")
    print(f"Output: {inst_path} ({os.path.getsize(inst_path)/1024:.0f}KB)")
    print("Done.")


if __name__ == '__main__':
    if len(sys.argv) < 4:
        print("Usage: python inference_mdx.py <model.onnx> <input.wav> <output_dir> [overlap]")
        sys.exit(1)
    
    model = sys.argv[1]
    input_file = sys.argv[2]
    output = sys.argv[3]
    ov = int(sys.argv[4]) if len(sys.argv) > 4 else OVERLAP
    
    separate_mdx(model, input_file, output, ov)
