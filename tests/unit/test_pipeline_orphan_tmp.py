"""Test that pipeline.sh removes orphaned atomic-write .tmp files on startup/cleanup.

The tracker writes ``pipeline_status.json`` and ``pipeline_status.json.tracker.json``
atomically via a ``.tmp`` + ``os.replace`` dance. If the process is killed mid-write,
the ``.tmp`` file is left behind forever. These tests verify that ``pipeline.sh``
cleans both orphan temp files both in legacy mode and in ``--steps`` mode.
"""

import json
import shutil
import subprocess
import sys
import textwrap
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
PIPELINE_SH = REPO_ROOT / "pipeline.sh"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


def _fake_input_wav(tmp_path: Path) -> Path:
    wav = tmp_path / "input.wav"
    wav.write_bytes(b"RIFF" + b"\x00" * 100)
    return wav


def test_orphan_status_tmp_is_removed_on_legacy_startup(tmp_path, monkeypatch):
    """A leftover pipeline_status.json.tmp must disappear when a new job starts."""
    _skip_if_missing_bin("bash")

    input_wav = _fake_input_wav(tmp_path)
    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"

    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))
    monkeypatch.setenv("ONDA_ALLOW_CPU", "1")

    # Simulate a crash in the middle of an atomic write.
    orphan_tmp = tmp_path / "pipeline_status.json.tmp"
    orphan_tmp.write_text("{" )
    orphan_tracker_tmp = tmp_path / "pipeline_status.json.tracker.json.tmp"
    orphan_tracker_tmp.write_text("{")

    # Use a non-existent model so the pipeline aborts quickly, but cleanup at
    # startup still runs.
    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--vocal-model", "/nonexistent/model",
        "--output", str(output_dir),
        "--device", "cpu",
        str(input_wav),
    ]

    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=60,
    )

    # The pipeline must fail because the model does not exist.
    assert result.returncode != 0, result.stdout
    # The orphaned temp files must have been cleaned up, not left behind.
    assert not orphan_tmp.exists(), "orphan pipeline_status.json.tmp was not removed"
    assert not orphan_tracker_tmp.exists(), "orphan tracker .tmp was not removed"


def test_orphan_status_tmp_is_removed_on_steps_startup(tmp_path, monkeypatch):
    """A leftover pipeline_status.json.tmp must disappear in --steps mode too."""
    _skip_if_missing_bin("bash")

    input_wav = _fake_input_wav(tmp_path)
    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"

    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    # Fake Demucs worker that succeeds quickly without touching real models.
    bin_dir = tmp_path / "workers"
    bin_dir.mkdir()
    fake_worker = bin_dir / "demucs_worker.py"
    fake_worker.write_text(
        textwrap.dedent(
            r"""
            #!/usr/bin/env python3
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

            print(json.dumps({"event": "preflight", "model": args.model,
                              "samplerate": 44100, "channels": 2, "models": 1}), flush=True)
            for pct in range(10, 101, 10):
                print(json.dumps({"event": "progress", "pct": float(pct),
                                  "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"}), flush=True)
                time.sleep(0.01)

            track = Path(args.input).stem
            outdir = Path(args.out) / args.model / track
            outdir.mkdir(parents=True, exist_ok=True)
            for stem in ("drums", "bass", "other", "vocals"):
                (outdir / f"{stem}.wav").write_bytes(b"RIFF" + b"\x00" * 100)

            print(json.dumps({"event": "stems", "dir": str(outdir),
                              "stems": ["drums", "bass", "other", "vocals"]}), flush=True)
            print(json.dumps({"event": "done", "seconds": 1.0}), flush=True)
            """
        )
    )
    fake_worker.chmod(0o755)
    monkeypatch.setenv("DEMUCS_WORKER", str(fake_worker))

    # Orphan temp files from a previous crashed atomic write.
    orphan_tmp = tmp_path / "pipeline_status.json.tmp"
    orphan_tmp.write_text("{")
    orphan_tracker_tmp = tmp_path / "pipeline_status.json.tracker.json.tmp"
    orphan_tracker_tmp.write_text("{")

    steps = [
        {
            "type": "demucs",
            "model": "htdemucs_ft",
            "stems": {
                "drums": {"action": "save"},
                "bass": {"action": "save"},
                "other": {"action": "save"},
                "vocals": {"action": "save"},
            },
        },
    ]

    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--device", "cuda",
        "--steps", json.dumps(steps),
        "--output", str(output_dir),
        str(input_wav),
    ]

    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, result.stdout
    assert not orphan_tmp.exists(), "orphan pipeline_status.json.tmp was not removed in --steps mode"
    assert not orphan_tracker_tmp.exists(), "orphan tracker .tmp was not removed in --steps mode"
    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "done", f"unexpected final status {final.get('status')!r}"
