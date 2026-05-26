#!/usr/bin/env python3
"""
MDX-Net ONNX inference script for Onda.
Based on UVR-MDX-Net architecture (ConvTDFNet).
"""

import sys
import os
import numpy as np
import onnxruntime as ort
import soundfile as sf
import librosa

def separate_mdx(model_path: str, input_path: str, output_dir: str, overlap: int = 8) -> None:
    """Separate vocals from instrumental using MDX-Net ONNX model."""
    
    device = 'cuda' if 'CUDAExecutionProvider' in ort.get_available_providers() else 'cpu'
    print(f"Device: {device}")
    
    # Load ONNX model
    session = ort.InferenceSession(model_path, providers=[f'{device.upper()}ExecutionProvider', 'CPUExecutionProvider'])
    
    input_name = session.get_inputs()[0].name
    input_shape = session.get_inputs()[0].shape
    print(f"Model input: {input_name}, shape: {input_shape}")
    
    # Load audio
    audio, sr = sf.read(input_path)
    if audio.ndim == 1:
        audio = np.stack([audio, audio], axis=1)  # Mono → stereo
    audio = audio.T  # (channels, samples)
    num_samples = audio.shape[1]
    print(f"Audio: {num_samples/sr:.1f}s, {sr}Hz, {audio.shape[0]}ch")
    
    # MDX-Net expects specific parameters
    # dim_f, dim_t, n_fft depend on the specific model
    # Default values for Kim_Vocal_2 / UVR_MDXNET_Main:
    dim_f = 2048
    dim_t = 8
    n_fft = 6144
    hop = 1024
    
    # Compute STFT
    spec = librosa.stft(audio[0].astype(np.float32), n_fft=n_fft, hop_length=hop)
    # Convert to magnitude
    mag = np.abs(spec)
    phase = np.angle(spec)
    
    # Reshape for model: (1, 2, freq_bins, time_frames)
    # The model expects stereo input
    spec_left = librosa.stft(audio[0].astype(np.float32), n_fft=n_fft, hop_length=hop)
    spec_right = librosa.stft(audio[1].astype(np.float32), n_fft=n_fft, hop_length=hop)
    
    input_spec = np.stack([
        np.stack([np.abs(spec_left), np.abs(spec_right)]),
    ], axis=0).astype(np.float32)  # (1, 2, freq_bins, time_frames)
    
    print(f"Input spec shape: {input_spec.shape}")
    
    # Run inference
    outputs = session.run(None, {input_name: input_spec})
    mask = outputs[0]  # (1, 2, freq_bins, time_frames)
    
    # Apply mask to get vocals
    vocal_mask = mask[0]  # (2, freq_bins, time_frames)
    
    # Reconstruct vocals
    vocals_left_spec = spec_left * vocal_mask[0]
    vocals_right_spec = spec_right * vocal_mask[1]
    
    vocals_left = librosa.istft(vocals_left_spec, hop_length=hop)
    vocals_right = librosa.istft(vocals_right_spec, hop_length=hop)
    
    # Ensure same length
    min_len = min(len(vocals_left), len(vocals_right), num_samples)
    vocals = np.stack([vocals_left[:min_len], vocals_right[:min_len]], axis=1)
    instrumental = audio.T[:min_len] - vocals
    
    # Generate output filename
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
    ov = int(sys.argv[4]) if len(sys.argv) > 4 else 8
    
    separate_mdx(model, input_file, output, ov)
