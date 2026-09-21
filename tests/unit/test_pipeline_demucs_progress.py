"""Test that pipeline.sh reports Demucs progress from the JSON event worker.

This test replaces ``tools/demucs_worker.py`` with a fake that emits the new
JSON event contract. It then runs pipeline.sh and checks that
``pipeline_status.json`` reflects a smooth, monotonic progress curve and that
completion/failure is reported correctly.
"""

import json
import os
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


def _write_fake_worker(
    bin_dir: Path,
    events: list[dict],
    exit_code: int = 0,
    create_stems: bool = True,
    sleep_per_event: float = 0.05,
) -> Path:
    """Write a fake demucs_worker.py that emits the given events and stems."""
    fake = bin_dir / "demucs_worker.py"
    events_json = json.dumps(events, ensure_ascii=False)
    script = f"""#!/usr/bin/env python3
import argparse
import json
import os
import sys
import time
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

events = {events_json!r}
events = json.loads(events)

for ev in events:
    print(json.dumps(ev), flush=True)
    time.sleep({sleep_per_event})

# Mirror the real worker: write the last error event to stderr on failure.
if {exit_code} != 0:
    for ev in reversed(events):
        if ev.get("event") == "error":
            print(f"{{ev['kind']}}: {{ev['msg']}}", file=sys.stderr, flush=True)
            break

if {create_stems!r}:
    track = Path(args.input).stem
    outdir = Path(args.out) / args.model / track
    outdir.mkdir(parents=True, exist_ok=True)
    for stem in ("drums", "bass", "other", "vocals"):
        (outdir / f"{{stem}}.wav").write_bytes(b"RIFF" + b"\\x00" * 100)

sys.exit({exit_code})
"""
    fake.write_text(script)
    fake.chmod(0o755)
    return fake


@pytest.fixture
def fake_worker_env(tmp_path, monkeypatch):
    """Provide a directory where a fake worker can be placed and pointed to."""
    bin_dir = tmp_path / "workers"
    bin_dir.mkdir()
    monkeypatch.setenv("DEMUCS_WORKER", str(bin_dir / "demucs_worker.py"))
    return bin_dir


def _run_pipeline(input_wav: Path, output_dir: Path, status_file: Path) -> subprocess.CompletedProcess:
    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--device", "cuda",
        "--demucs-keep", "all",
        "--output", str(output_dir),
        str(input_wav),
    ]
    return subprocess.run(cmd, capture_output=True, text=True)


def test_pipeline_demucs_reports_monotonic_progress(fake_worker_env, tmp_path, monkeypatch):
    """pipeline.sh must derive Demucs progress from JSON events and never go backwards."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    # Include a backward pct jump (90 -> 70) to verify pipeline monotonicity.
    events = [
        {"event": "preflight", "model": "htdemucs_ft", "samplerate": 44100, "channels": 2, "models": 1},
        {"event": "progress", "pct": 10.0, "model_idx_in_bag": 0, "shift_idx": 0, "state": "start"},
        {"event": "progress", "pct": 50.0, "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"},
        {"event": "progress", "pct": 90.0, "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"},
        {"event": "progress", "pct": 70.0, "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"},
        {"event": "stems", "dir": str(tmp_path), "stems": ["drums", "bass", "other", "vocals"]},
        {"event": "done", "seconds": 1.234},
    ]
    _write_fake_worker(fake_worker_env, events, exit_code=0, sleep_per_event=0.0)

    progress_values = []
    with subprocess.Popen(
        [
            "bash",
            str(PIPELINE_SH),
            "--device", "cuda",
            "--demucs-keep", "all",
            "--output", str(output_dir),
            str(input_wav),
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    ) as proc:
        while proc.poll() is None:
            if status_file.exists():
                try:
                    data = json.loads(status_file.read_text())
                except (json.JSONDecodeError, OSError):
                    data = {}
                if data.get("step") == "demucs":
                    progress_values.append(data.get("progress", 0))
            time.sleep(0.05)

    stdout, stderr = proc.communicate()
    assert proc.returncode == 0, stderr or stdout

    final = json.loads(status_file.read_text())
    assert final.get("status") == "done"
    assert final.get("step") == "complete"

    # Monotonicity: observed demucs progress must never decrease.
    demucs_progress = [p for p in progress_values if p is not None]
    for prev, curr in zip(demucs_progress, demucs_progress[1:]):
        assert curr >= prev, f"progress went backwards: {prev} -> {curr}"

    # Ensure stems were copied to the output dir.
    for stem in ("drums", "bass", "other", "vocals"):
        assert (output_dir / f"{stem}.wav").exists()


def test_pipeline_demucs_fails_on_worker_error(fake_worker_env, tmp_path, monkeypatch):
    """A worker that emits an error event and exits non-zero must fail the step."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    events = [
        {"event": "preflight", "model": "htdemucs_ft", "samplerate": 44100, "channels": 2, "models": 1},
        {"event": "progress", "pct": 25.0, "model_idx_in_bag": 0, "shift_idx": 0, "state": "start"},
        {"event": "error", "kind": "LoadAudioError", "msg": "could not read audio"},
    ]
    _write_fake_worker(fake_worker_env, events, exit_code=21, create_stems=False)

    result = _run_pipeline(input_wav, output_dir, status_file)
    assert result.returncode != 0

    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed"
    assert final.get("step") == "demucs"
    assert final.get("exit_code") == 21

    # Failure log must be persisted.
    failed_log = output_dir / "_failed_demucs" / "stderr.log"
    assert failed_log.exists()


def test_pipeline_demucs_fails_when_done_missing(fake_worker_env, tmp_path, monkeypatch):
    """A worker that exits 0 without a 'done' event must be treated as a failure."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    events = [
        {"event": "preflight", "model": "htdemucs_ft", "samplerate": 44100, "channels": 2, "models": 1},
        {"event": "progress", "pct": 50.0, "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"},
    ]
    _write_fake_worker(fake_worker_env, events, exit_code=0, create_stems=False)

    result = _run_pipeline(input_wav, output_dir, status_file)
    assert result.returncode != 0

    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed"
    assert final.get("step") == "demucs"


def test_pipeline_demucs_fails_when_stems_missing(fake_worker_env, tmp_path, monkeypatch):
    """A worker that exits 0 but writes no stems must be treated as a failure."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    events = [
        {"event": "preflight", "model": "htdemucs_ft", "samplerate": 44100, "channels": 2, "models": 1},
        {"event": "done", "seconds": 0.5},
    ]
    _write_fake_worker(fake_worker_env, events, exit_code=0, create_stems=False)

    result = _run_pipeline(input_wav, output_dir, status_file)
    assert result.returncode != 0

    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed"
    assert final.get("step") == "demucs"
