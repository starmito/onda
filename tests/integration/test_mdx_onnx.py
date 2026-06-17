"""End-to-end tests for MDX-Net ONNX separation."""
import subprocess
import os

CONTAINER = os.environ.get('CONTAINER_NAME', 'onda')
SCRIPT = "inference/inference_mdx.py"
FIXTURE_DIR = os.environ.get('FIXTURE_DIR', '/app/tests/integration/fixtures')
OUTPUT = "/tmp/onda-test-mdx"
MODEL_PATH = os.environ.get('MDX_MODEL_PATH', '/app/models/MDX_Net_Models/Kim_Vocal_2.onnx')

def run_mdx(input_file, output_dir=None):
    """Run MDX-Net inside container."""
    out = output_dir or f"{OUTPUT}"
    cmd = [
        "docker", "exec", CONTAINER,
        "python3", SCRIPT,
        MODEL_PATH,
        f"{FIXTURE_DIR}/{input_file}",
        out,
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=120)

def test_mdx_sine():
    """Separate vocals from sine wave using MDX-Net."""
    import tempfile
    out = tempfile.mkdtemp(prefix="onda-test-mdx-sine-")
    result = run_mdx("sine_440_5s.flac", out)
    assert result.returncode == 0, f"MDX-Net failed: {result.stderr}"

def test_mdx_chirp():
    """Separate vocals from chirp using MDX-Net."""
    import tempfile
    out = tempfile.mkdtemp(prefix="onda-test-mdx-chirp-")
    result = run_mdx("chirp_5s.flac", out)
    assert result.returncode == 0, f"MDX-Net chirp failed: {result.stderr}"
