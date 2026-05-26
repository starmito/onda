"""End-to-end tests for MDX-Net ONNX separation."""
import subprocess
import os

CONTAINER = "onda"
SCRIPT = "inference/inference_mdx.py"
FIXTURE_DIR = "/app/tests/integration/fixtures"
OUTPUT = "/tmp/onda-test-mdx"

def run_mdx(input_file, output_dir=None):
    """Run MDX-Net inside container."""
    out = output_dir or f"{OUTPUT}"
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER,
        "python3", SCRIPT,
        f"{FIXTURE_DIR}/{input_file}",
        out,
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=120)

def test_mdx_sine():
    """Separate vocals from sine wave using MDX-Net."""
    result = run_mdx("sine_440_5s.flac", "/tmp/onda-test-mdx-sine/")
    assert result.returncode == 0, f"MDX-Net failed: {result.stderr}"

def test_mdx_chirp():
    """Separate vocals from chirp using MDX-Net."""
    result = run_mdx("chirp_5s.flac", "/tmp/onda-test-mdx-chirp/")
    assert result.returncode == 0, f"MDX-Net chirp failed: {result.stderr}"
