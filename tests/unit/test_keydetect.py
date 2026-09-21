import os
import sys
import tempfile
import wave

import numpy as np
import pytest

# Ensure the repo root (where keydetect.py lives) is on the path.
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

# conftest.py injects lightweight mocks for torch/librosa. Remove them here so
# key detection tests exercise the real libraries.
for _mock_name in ("librosa", "torch", "torch.nn", "torch.cuda", "torch.nn.functional"):
    sys.modules.pop(_mock_name, None)

try:
    import librosa  # noqa: F401
except Exception as exc:
    pytest.skip(f"librosa not available: {exc}", allow_module_level=True)

import keydetect  # noqa: E402


def _pitch_class(note: str) -> int:
    """Return pitch class for a note name (sharps or flats accepted)."""
    names = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]
    aliases = {"Db": 1, "Eb": 3, "Gb": 6, "Ab": 8, "Bb": 10}
    if note in aliases:
        return aliases[note]
    return names.index(note)


def _make_chroma(root: str, scale: str, frames: int = 200, noise: float = 0.02) -> np.ndarray:
    """Build a synthetic chromagram that strongly suggests a given key."""
    root_pc = _pitch_class(root)
    major_scale = [0, 2, 4, 5, 7, 9, 11]
    minor_scale = [0, 2, 3, 5, 7, 8, 10]
    scale_pcs = [(root_pc + interval) % 12 for interval in (major_scale if scale == "major" else minor_scale)]

    weights = np.ones(12) * 0.1
    # tonic, third, fifth
    third = scale_pcs[2]
    fifth = scale_pcs[4]
    weights[root_pc] = 1.0
    weights[third] = 0.9
    weights[fifth] = 0.95
    for pc in scale_pcs:
        if pc not in (root_pc, third, fifth):
            weights[pc] = 0.7

    # Normalize and add a little frame-to-frame variation.
    chroma = np.tile(weights / weights.max(), (frames, 1)).T
    chroma = chroma + np.random.default_rng(42).normal(0, noise, chroma.shape)
    chroma = np.clip(chroma, 0, None)
    return chroma


@pytest.mark.parametrize(
    "root,scale",
    [
        ("C", "major"),
        ("A", "minor"),
        ("G", "major"),
        ("E", "minor"),
        ("F#", "major"),
        ("D", "minor"),
        ("Bb", "major"),
    ],
)
def test_detect_key_synthetic(root: str, scale: str):
    chroma = _make_chroma(root, scale)
    result = keydetect.detect_key(chroma)
    assert result["key"] == root, f"expected {root} {scale}, got {result['key']} {result['scale']}"
    assert result["scale"] == scale
    assert 0.0 <= result["strength"] <= 1.0
    assert isinstance(result["alternatives"], list)
    assert len(result["alternatives"]) == 2
    for alt in result["alternatives"]:
        assert "key" in alt and "scale" in alt and "strength" in alt
    assert isinstance(result["dubious"], bool)


def test_detect_key_flat_spelling():
    """Bb major must be spelled 'Bb', not 'A#'."""
    chroma = _make_chroma("Bb", "major")
    result = keydetect.detect_key(chroma)
    assert result["key"] == "Bb"
    assert result["scale"] == "major"


def test_analyze_file_silence_fails_cleanly():
    """A silent/empty file must raise ValueError, not crash."""
    # conftest injects a torch mock that breaks scipy.signal; remove it here.
    for _mock_name in ("torch", "torch.nn", "torch.cuda", "torch.nn.functional"):
        sys.modules.pop(_mock_name, None)

    with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
        path = tmp.name
    try:
        # Write a 0.5 s silent mono WAV using only the standard library.
        with wave.open(path, "wb") as wav:
            wav.setnchannels(1)
            wav.setsampwidth(2)
            wav.setframerate(22050)
            wav.writeframes(b"\x00" * 22050)

        with pytest.raises(ValueError):
            keydetect.analyze_file(path)
    finally:
        os.remove(path)


def test_analyze_file_missing_fails_cleanly():
    with pytest.raises(FileNotFoundError):
        keydetect.analyze_file("/nonexistent/path/no_such_file.wav")
