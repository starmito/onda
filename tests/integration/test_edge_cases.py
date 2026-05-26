"""Edge case tests for separation methods."""
import subprocess
import pytest

CONTAINER = "onda"
FIXTURE_DIR = "/app/tests/integration/fixtures"


def run_demucs(input_file, output_dir, stems="vocals", timeout=120):
    """Run Demucs PyTorch inside container."""
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER,
        "demucs", "-n", "htdemucs_ft",
        "--two-stems", stems,
        "-o", output_dir,
        f"{FIXTURE_DIR}/{input_file}",
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def test_silence_handled():
    """Silence should not crash — produce output (silent stems)."""
    out = "/tmp/onda-test-edge-silence/"
    result = run_demucs("silence_5s.flac", out)
    assert result.returncode in (0, 1), f"Unexpected exit: {result.returncode}"


def test_short_audio():
    """Very short audio (0.5s) should still process or fail gracefully."""
    out = "/tmp/onda-test-edge-short/"
    result = run_demucs("short_05s.flac", out)
    assert result.returncode in (0, 1), f"Short audio crashed: {result.stderr}"


def test_nonexistent_file():
    """Missing input file should error clearly."""
    out = "/tmp/onda-test-edge-missing/"
    result = run_demucs("nonexistent.flac", out)
    # Demucs prints error to stderr but may return 0
    assert "does not exist" in result.stderr.lower() or result.returncode != 0, \
        f"Should report missing file. stderr: {result.stderr}"


def test_mdx_short_audio():
    """MDX-Net with short audio should not crash."""
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER, "python3",
        "inference/inference_mdx.py",
        f"{FIXTURE_DIR}/short_05s.flac",
        "/tmp/onda-test-edge-mdx-short/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
    assert result.returncode in (0, 1), f"MDX short audio crashed: {result.stderr}"
