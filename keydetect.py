#!/usr/bin/env python3
"""Key detection with librosa + Krumhansl-Schmuckler profiles.

Usage:
    python3 keydetect.py <audio_file> [--duration SECONDS]

Prints a JSON object with keys:
    key: str        # note name, e.g. "C", "F#", "Bb"
    scale: str      # "major" or "minor"
    strength: float # correlation value in [0, 1]
    alternatives: list[{key, scale, strength}]  # 2 best runners-up
    dubious: bool   # true if the gap between best and 2nd best is < 0.05
"""

import argparse
import json
import os
import sys

import numpy as np

try:
    import librosa  # type: ignore
except Exception:  # pragma: no cover - librosa is optional at import time
    librosa = None  # type: ignore

# Krumhansl-Schmuckler key profiles (index 0 = C).
MAJOR_PROFILE = np.array(
    [6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88],
    dtype=np.float64,
)
MINOR_PROFILE = np.array(
    [6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17],
    dtype=np.float64,
)

# Note spelling for key roots. Flat keys use flats; sharp keys use sharps.
# Pitch classes for flat major keys: F=5, Bb=10, Eb=3, Ab=8, Db=1.
_MAJOR_ROOT_NAMES = ["C", "Db", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"]
# Pitch classes for flat minor keys: D=2, G=7, C=0, F=5, Bb=10, Eb=3.
_MINOR_ROOT_NAMES = ["C", "C#", "D", "Eb", "E", "F", "F#", "G", "G#", "A", "Bb", "B"]


def _rotate(arr: np.ndarray, shift: int) -> np.ndarray:
    return np.roll(arr, shift)


def _pearson(a: np.ndarray, b: np.ndarray) -> float:
    """Pearson correlation, safe for near-constant vectors."""
    a = a - a.mean()
    b = b - b.mean()
    denom = np.sqrt(np.sum(a**2) * np.sum(b**2))
    if denom < 1e-12:
        return 0.0
    return float(np.clip(np.sum(a * b) / denom, -1.0, 1.0))


def _root_name(pitch_class: int, scale: str) -> str:
    """Return the correctly spelled root for a pitch class and scale."""
    pc = pitch_class % 12
    if scale == "major":
        return _MAJOR_ROOT_NAMES[pc]
    return _MINOR_ROOT_NAMES[pc]


def detect_key(chroma: np.ndarray) -> dict:
    """Return {key, scale, strength, alternatives, dubious} from a (12, frames) chromagram."""
    if chroma.size == 0:
        raise ValueError("empty chromagram")

    # Robust aggregate: median over frames instead of mean.
    chroma_avg = np.median(chroma, axis=1)
    if np.max(chroma_avg) > 0:
        chroma_avg = chroma_avg / np.max(chroma_avg)

    candidates = []
    for shift in range(12):
        major_score = _pearson(chroma_avg, _rotate(MAJOR_PROFILE, shift))
        minor_score = _pearson(chroma_avg, _rotate(MINOR_PROFILE, shift))
        candidates.append(
            {
                "key": _root_name(shift, "major"),
                "scale": "major",
                "strength": max(0.0, major_score),
            }
        )
        candidates.append(
            {
                "key": _root_name(shift, "minor"),
                "scale": "minor",
                "strength": max(0.0, minor_score),
            }
        )

    candidates.sort(key=lambda c: c["strength"], reverse=True)
    best = candidates[0]
    alternatives = candidates[1:3]

    dubious = False
    if len(alternatives) > 0:
        gap = best["strength"] - alternatives[0]["strength"]
        dubious = gap < 0.05

    return {
        "key": best["key"],
        "scale": best["scale"],
        "strength": best["strength"],
        "alternatives": alternatives,
        "dubious": dubious,
    }


def _load_audio(path: str, duration: float | None = None) -> tuple[np.ndarray, int]:
    """Load audio with librosa, falling back to soundfile."""
    if librosa is not None:
        return librosa.load(path, sr=None, mono=True, duration=duration)  # type: ignore

    # pragma: no cover - fallback only
    import soundfile as sf  # type: ignore

    data, sr = sf.read(path, always_2d=False)
    if data.ndim > 1:
        data = np.mean(data, axis=1)
    if duration is not None:
        samples = int(min(len(data), int(duration * sr)))
        data = data[:samples]
    return data.astype(np.float64), sr


def _compute_chroma(y: np.ndarray, sr: int) -> np.ndarray:
    if librosa is None:
        raise RuntimeError("librosa is not available")
    tuning = librosa.estimate_tuning(y=y, sr=sr)
    return librosa.feature.chroma_cqt(y=y, sr=sr, tuning=tuning)  # type: ignore


def analyze_file(path: str, duration: float | None = None) -> dict:
    if not os.path.isfile(path):
        raise FileNotFoundError(f"audio file not found: {path}")

    y, sr = _load_audio(path, duration=duration)
    if len(y) == 0 or np.max(np.abs(y)) < 1e-6:
        raise ValueError("audio file is empty or silent")

    chroma = _compute_chroma(y, sr)
    return detect_key(chroma)


def _parse_args(argv=None):
    parser = argparse.ArgumentParser(description="Detect musical key from an audio file.")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument(
        "--duration",
        type=float,
        default=None,
        help="Maximum seconds to analyze (default: analyze full file)",
    )
    return parser.parse_args(argv)


def main(argv=None):
    args = _parse_args(argv)
    result = analyze_file(args.input, duration=args.duration)
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
