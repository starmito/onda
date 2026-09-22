"""Tests for the pipeline.sh edge cases fixed in the 'flecos' task.

Covers:
* A Demucs worker that dies (and becomes a zombie) is detected and reported.
* The parent directory of ``pipeline_status.json`` is created automatically.
* No ``*.eta`` sidecar files are left in the output directory.
"""

import json
import shutil
import subprocess
import sys
import time
from pathlib import Path

import pytest


REPO_ROOT = Path(__file__).resolve().parents[2]
PIPELINE_SH = REPO_ROOT / "pipeline.sh"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


def _write_fast_failing_worker(bin_dir: Path, stderr_msg: str = "forced demucs death") -> Path:
    """Write a fake demucs_worker.py that exits immediately with a message."""
    fake = bin_dir / "demucs_worker.py"
    fake.write_text(
        f"""#!/usr/bin/env python3
import sys
sys.stderr.write({stderr_msg!r} + "\\n")
sys.exit(42)
"""
    )
    fake.chmod(0o755)
    return fake


def _write_success_worker(bin_dir: Path) -> Path:
    """Write a fake demucs_worker.py that succeeds and writes the expected stems."""
    fake = bin_dir / "demucs_worker.py"
    fake.write_text(
        r"""#!/usr/bin/env python3
import argparse
import json
import sys
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--model", required=True)
parser.add_argument("--device", required=True)
parser.add_argument("--input", required=True)
parser.add_argument("--out", required=True)
parser.add_argument("--shifts", type=int, default=1)
parser.add_argument("--segment", type=int, default=0)
parser.add_argument("--jobs", type=int, default=0)
args = parser.parse_args()

print(json.dumps({"event": "preflight", "model": args.model,
                  "samplerate": 44100, "channels": 2, "models": 1}), flush=True)
print(json.dumps({"event": "progress", "pct": 100.0,
                  "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"}), flush=True)

track = Path(args.input).stem
outdir = Path(args.out) / args.model / track
outdir.mkdir(parents=True, exist_ok=True)
for stem in ("drums", "bass", "other", "vocals"):
    (outdir / f"{stem}.wav").write_bytes(b"RIFF" + b"\x00" * 100)

print(json.dumps({"event": "stems", "dir": str(outdir),
                  "stems": ["drums", "bass", "other", "vocals"]}), flush=True)
print(json.dumps({"event": "done", "seconds": 0.5}), flush=True)
"""
    )
    fake.chmod(0o755)
    return fake


@pytest.fixture
def fake_worker_env(tmp_path, monkeypatch):
    """Provide a directory where a fake worker can be placed and pointed to."""
    bin_dir = tmp_path / "workers"
    bin_dir.mkdir()
    monkeypatch.setenv("DEMUCS_WORKER", str(bin_dir / "demucs_worker.py"))
    return bin_dir


def test_demucs_fast_failure_is_reaped_and_reported(
    fake_worker_env, tmp_path, monkeypatch
):
    """A demucs worker that exits fast (becoming a zombie) must fail quickly."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    _write_fast_failing_worker(fake_worker_env, stderr_msg="forced demucs death")

    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--device", "cuda",
        "--demucs-keep", "all",
        "--output", str(output_dir),
        str(input_wav),
    ]

    start = time.monotonic()
    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=30,
        start_new_session=True,
    )
    elapsed = time.monotonic() - start

    # The previous bug kept the loop running forever because kill -0 sees zombies.
    assert elapsed < 5, f"pipeline hung for {elapsed:.1f}s"

    assert result.returncode != 0, result.stdout

    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed"
    assert final.get("step") == "demucs"
    assert final.get("exit_code") == 42
    assert "forced demucs death" in final.get("error", "")

    diagnostics_dir = output_dir / "_failed_demucs"
    stderr_log = diagnostics_dir / "stderr.log"
    assert diagnostics_dir.is_dir(), f"diagnostics dir missing: {diagnostics_dir}"
    assert stderr_log.exists(), f"persisted stderr log missing: {stderr_log}"
    assert "forced demucs death" in stderr_log.read_text()

    assert "❌ Paso demucs fallo. Ultimas lineas:" in result.stdout
    assert "forced demucs death" in result.stdout


def test_status_dir_is_created_automatically(fake_worker_env, tmp_path, monkeypatch):
    """pipeline.sh must create the parent directory of pipeline_status.json."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_dir = tmp_path / "missing" / "nested"
    status_file = status_dir / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))
    assert not status_dir.exists()

    _write_success_worker(fake_worker_env)

    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--device", "cuda",
        "--demucs-keep", "all",
        "--output", str(output_dir),
        str(input_wav),
    ]

    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, result.stdout
    assert status_file.exists(), "status file was not created inside a missing directory"


def test_no_eta_sidecar_is_left_in_output(fake_worker_env, tmp_path, monkeypatch):
    """No *.eta files must remain in the output directory after a run."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    output_dir.mkdir(parents=True, exist_ok=True)
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    # Simulate a stray ETA sidecar left by an older implementation.
    (output_dir / "pipeline_status.json.eta").write_text("120")
    (output_dir / "something.eta").write_text("leftover")
    (Path(str(status_file) + ".eta")).write_text("120")

    _write_success_worker(fake_worker_env)

    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--device", "cuda",
        "--demucs-keep", "all",
        "--output", str(output_dir),
        str(input_wav),
    ]

    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, result.stdout
    assert not (output_dir / "pipeline_status.json.eta").exists()
    assert not (output_dir / "something.eta").exists()
    assert not list(output_dir.glob("*.eta"))
    assert not Path(str(status_file) + ".eta").exists()
