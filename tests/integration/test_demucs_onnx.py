"""End-to-end tests for Demucs ONNX separation."""
import os
import subprocess
import pytest

OUTPUT = "/tmp/onda-test-demucs-onnx"
SCRIPT = "inference/inference_demucs_onnx.py"
CONTAINER = "onda"
FIXTURE_DIR = "/app/tests/integration/fixtures"

def run_onnx(input_file, stems="vocals"):
    """Run Demucs ONNX inside container."""
    cmd = [
        "docker", "exec", CONTAINER,
        "python3", SCRIPT,
        f"{FIXTURE_DIR}/{input_file}",
        OUTPUT,
        "--stems", stems,
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=120)

def test_onnx_runtime_info():
    """Verify --runtime shows available providers."""
    result = subprocess.run(
        ["docker", "exec", CONTAINER, "python3", SCRIPT, "--runtime"],
        capture_output=True, text=True, timeout=15
    )
    assert result.returncode == 0, f"runtime failed: {result.stderr}"
    assert "onnxruntime:" in result.stdout
    assert "Providers" in result.stdout

def test_onnx_sine_vocals():
    """Separate vocals from a 440 Hz sine wave."""
    result = run_onnx("sine_440_5s.flac", "vocals")
    assert result.returncode == 0, f"Demucs ONNX failed: {result.stderr}"
    assert "Demucs ONNX:" in result.stdout, "Missing RTF output"
    # Verify output file exists inside container
    output_file = f"{OUTPUT}/vocals.wav"
    check = subprocess.run(
        ["docker", "exec", CONTAINER, "test", "-f", output_file],
        capture_output=True
    )
    assert check.returncode == 0, f"Output file missing: {output_file}"

def test_onnx_list_models():
    """Verify --list-models works."""
    result = subprocess.run(
        ["docker", "exec", CONTAINER, "python3", SCRIPT, "--list-models"],
        capture_output=True, text=True, timeout=15
    )
    assert result.returncode == 0, f"list-models failed: {result.stderr}"
    assert "htdemucs_ft" in result.stdout, "Expected htdemucs_ft model listed"
