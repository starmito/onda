"""Tests for the Demucs worker liveness check hardened in task M.

Covers:
* A worker that dies immediately is detected and the step fails quickly.
* A PID that is still alive but no longer belongs to our worker (simulated
  recycled PID) is not mistaken for the worker, and the step closes honestly
  when outputs are complete.
* The happy path (worker finishes normally) is not broken.
"""

import json
import shutil
import subprocess
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
print(json.dumps({"event": "progress", "pct": 50.0,
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


def _write_identity_changing_worker(bin_dir: Path) -> Path:
    """Write a fake worker that finishes its work and then changes identity.

    After writing the done event and creating the stems it exec(3)s ``sleep``.
    The PID stays alive, but it is no longer the Demucs worker: the old
    ``_process_is_alive`` (kill -0 + ps) would keep waiting forever, while the
    hardened version detects the cmdline change and closes the step.
    """
    fake = bin_dir / "demucs_worker.py"
    fake.write_text(
        r"""#!/usr/bin/env python3
import argparse
import json
import os
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
print(json.dumps({"event": "progress", "pct": 75.0,
                  "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"}), flush=True)

track = Path(args.input).stem
outdir = Path(args.out) / args.model / track
outdir.mkdir(parents=True, exist_ok=True)
for stem in ("drums", "bass", "other", "vocals"):
    (outdir / f"{stem}.wav").write_bytes(b"RIFF" + b"\x00" * 100)

print(json.dumps({"event": "stems", "dir": str(outdir),
                  "stems": ["drums", "bass", "other", "vocals"]}), flush=True)
print(json.dumps({"event": "done", "seconds": 0.5}), flush=True)

# Flush and replace this process image with a different program.  The PID is
# kept alive, but it no longer belongs to the Demucs worker.
sys.stdout.flush()
sys.stderr.flush()
os.execvp("sleep", ["sleep", "1"])
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


def test_demucs_worker_dies_immediately_is_reaped_and_reported(
    fake_worker_env, tmp_path, monkeypatch
):
    """A worker that exits fast must fail quickly instead of hanging forever."""
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

    # The previous bug kept the loop running forever because kill -0 sees any
    # alive process with the same PID.
    assert elapsed < 5, f"pipeline hung for {elapsed:.1f}s"

    assert result.returncode != 0, result.stdout

    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed"
    assert final.get("step") == "demucs"
    assert final.get("exit_code") == 42
    assert "forced demucs death" in final.get("error", "")


def test_demucs_worker_identity_change_is_detected_and_step_closes_honestly(
    fake_worker_env, tmp_path, monkeypatch
):
    """A PID that stops being our worker must not keep the step in running."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    _write_identity_changing_worker(fake_worker_env)

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

    # Without the cmdline/fd identity checks the watcher would keep waiting for
    # the ``sleep`` process until the outer test timeout killed it.
    assert elapsed < 15, f"pipeline hung for {elapsed:.1f}s"

    assert result.returncode == 0, result.stdout

    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "done"
    assert final.get("step") == "complete"

    # Stems must have been copied/linked to the output directory.
    for stem in ("drums", "bass", "other", "vocals"):
        assert (output_dir / f"{stem}.wav").exists(), f"missing stem: {stem}"


def test_demucs_worker_happy_path_still_works(fake_worker_env, tmp_path, monkeypatch):
    """A normal worker that finishes cleanly must still succeed."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

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
        start_new_session=True,
    )

    assert result.returncode == 0, result.stdout

    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "done"
    assert final.get("step") == "complete"

    for stem in ("drums", "bass", "other", "vocals"):
        assert (output_dir / f"{stem}.wav").exists(), f"missing stem: {stem}"
