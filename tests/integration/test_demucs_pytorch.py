"""End-to-end tests for Demucs PyTorch separation."""
import os
import subprocess
import pytest

CONTAINER = os.environ.get('CONTAINER_NAME', 'onda')
FIXTURE_DIR = os.environ.get('FIXTURE_DIR', '/app/tests/integration/fixtures')


def run_demucs(input_file, output_dir, stems="vocals", timeout=120):
    """Run Demucs PyTorch inside container."""
    cmd = [
        "docker", "exec", CONTAINER,
        "demucs", "-n", "htdemucs_ft",
        "--two-stems", stems,
        "-o", output_dir,
        f"{FIXTURE_DIR}/{input_file}",
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def test_demucs_sine_vocals():
    """Separate vocals from 440 Hz sine wave."""
    out = "/tmp/onda-test-demucs-sine/"
    result = run_demucs("sine_440_5s.flac", out)
    assert result.returncode == 0, f"Demucs failed: {result.stderr}"
    # Verify output file exists
    check = subprocess.run(
        ["docker", "exec", CONTAINER,
         "find", out, "-name", "vocals.wav"],
        capture_output=True, text=True, timeout=10
    )
    assert "vocals.wav" in check.stdout, f"Missing vocals.wav in output"


def test_demucs_chirp_vocals():
    """Separate vocals from chirp signal."""
    out = "/tmp/onda-test-demucs-chirp/"
    result = run_demucs("chirp_5s.flac", out)
    assert result.returncode == 0, f"Demucs chirp failed: {result.stderr}"


def test_demucs_output_structure():
    """Verify output has expected directory structure."""
    out = "/tmp/onda-test-demucs-struct/"
    result = run_demucs("sine_440_5s.flac", out)
    assert result.returncode == 0
    # Demucs output: <out>/htdemucs_ft/<track_name>/vocals.wav, no_vocals.wav
    check = subprocess.run(
        ["docker", "exec", CONTAINER,
         "find", out, "-name", "*.wav", "-type", "f"],
        capture_output=True, text=True, timeout=10
    )
    wavs = [w for w in check.stdout.strip().split("\n") if w]
    assert len(wavs) >= 2, f"Expected >=2 wav files, got {len(wavs)}: {wavs}"
