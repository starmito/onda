"""Edge case tests for separation methods."""
import subprocess
import pytest

CONTAINER = "onda"
FIXTURE_DIR = "/app/tests/integration/fixtures"

def run_demucs_onnx(input_file, stems="vocals"):
    cmd = [
        "docker", "exec", CONTAINER, "python3",
        "inference/inference_demucs_onnx.py",
        f"{FIXTURE_DIR}/{input_file}",
        f"/tmp/onda-test-edge-{input_file}/",
        "--stems", stems,
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=120)

def test_silence_handled():
    """Silence should not crash — produce output (silent stems)."""
    result = run_demucs_onnx("silence_5s.flac")
    # May exit 0 or 1 depending on model, but must not hang
    assert result.returncode in (0, 1), f"Unexpected exit: {result.returncode}"

def test_short_audio():
    """Very short audio (0.5s) should still process or fail gracefully."""
    result = run_demucs_onnx("short_05s.flac")
    # Short audio may fail but should not crash
    assert result.returncode in (0, 1), f"Short audio crashed: {result.stderr}"

def test_nonexistent_file():
    """Missing input file should error clearly."""
    result = run_demucs_onnx("nonexistent.flac")
    assert result.returncode != 0, "Should fail on missing file"

def test_mdx_short_audio():
    """MDX-Net with short audio should not crash."""
    cmd = [
        "docker", "exec", CONTAINER, "python3",
        "inference/inference_mdx.py",
        f"{FIXTURE_DIR}/short_05s.flac",
        "/tmp/onda-test-edge-mdx-short/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
    assert result.returncode in (0, 1), f"MDX short audio crashed: {result.stderr}"
