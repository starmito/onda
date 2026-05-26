#!/usr/bin/env python3
"""Generate synthetic test audio files for integration tests."""
import numpy as np
import soundfile as sf
import os

SAMPLE_RATE = 44100
OUT_DIR = os.path.join(os.path.dirname(__file__), "fixtures")

def generate_sine(freq=440, duration=5.0, sr=SAMPLE_RATE):
    t = np.linspace(0, duration, int(sr * duration), endpoint=False)
    return (np.sin(2 * np.pi * freq * t) * 0.8).astype(np.float32)

def generate_chirp(duration=5.0, sr=SAMPLE_RATE):
    t = np.linspace(0, duration, int(sr * duration), endpoint=False)
    freq = 100 + (2000 - 100) * t / duration
    phase = 2 * np.pi * np.cumsum(freq) / sr
    return (np.sin(phase) * 0.8).astype(np.float32)

def generate_silence(duration=5.0, sr=SAMPLE_RATE):
    return np.zeros(int(sr * duration), dtype=np.float32)

def generate_short(duration=0.5, sr=SAMPLE_RATE):
    return generate_sine(880, duration, sr)

def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    
    fixtures = {
        "sine_440_5s.flac": generate_sine(440, 5.0),
        "chirp_5s.flac": generate_chirp(5.0),
        "silence_5s.flac": generate_silence(5.0),
        "short_05s.flac": generate_short(0.5),
    }
    
    for name, audio in fixtures.items():
        path = os.path.join(OUT_DIR, name)
        sf.write(path, audio, SAMPLE_RATE)
        print(f"  Created: {path} ({len(audio)/SAMPLE_RATE:.1f}s)")
    
    print(f"\nGenerated {len(fixtures)} test fixtures in {OUT_DIR}")

if __name__ == "__main__":
    main()
