"""Test that pipeline.sh cleans up its own temporary files even when a job fails.

The pipeline creates per-step directories (``_step_*``) and a routed-stems
folder (``_routed``) while running. These must disappear both on success and
on failure so a crashed job never leaves garbage behind.
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


def _fake_demucs_worker(bin_dir: Path, fail_after: int | None = None) -> Path:
    """Return a fake worker script.

    If ``fail_after`` is set, the worker writes progress events up to that
    percentage and then exits with a non-zero code, leaving behind the
    temporary files it created.
    """
    fake_worker = bin_dir / "demucs_worker.py"
    fake_worker.write_text(
        textwrap.dedent(
            f"""
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

            print(json.dumps({{"event": "preflight", "model": args.model,
                              "samplerate": 44100, "channels": 2, "models": 1}}), flush=True)
            for pct in range(10, 101, 10):
                print(json.dumps({{"event": "progress", "pct": float(pct),
                                  "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"}}), flush=True)
                time.sleep(0.01)
                if {fail_after} is not None and pct >= {fail_after}:
                    sys.exit(1)

            track = Path(args.input).stem
            outdir = Path(args.out) / args.model / track
            outdir.mkdir(parents=True, exist_ok=True)
            for stem in ("drums", "bass", "other", "vocals"):
                (outdir / f"{{stem}}.wav").write_bytes(b"RIFF" + b"\\x00" * 100)

            print(json.dumps({{"event": "stems", "dir": str(outdir),
                              "stems": ["drums", "bass", "other", "vocals"]}}), flush=True)
            print(json.dumps({{"event": "done", "seconds": 1.0}}), flush=True)
            """
        )
    )
    fake_worker.chmod(0o755)
    return fake_worker


def test_steps_mode_cleans_temps_on_failure(tmp_path: Path, monkeypatch):
    """A failing chained job must not leave _step_* or _routed directories."""
    _skip_if_missing_bin("bash")

    input_wav = _fake_input_wav(tmp_path)
    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"

    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    bin_dir = tmp_path / "workers"
    bin_dir.mkdir()
    fake_worker = _fake_demucs_worker(bin_dir, fail_after=50)
    monkeypatch.setenv("DEMUCS_WORKER", str(fake_worker))

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

    assert result.returncode != 0, result.stdout

    # The pipeline must clean its own temporary directories even on failure.
    assert not list(output_dir.glob("_step_*")), "leftover _step_* temp directory after failure"
    assert not (output_dir / "_routed").exists(), "leftover _routed directory after failure"

    # The user-facing output files must not be present because the step failed.
    assert not list(output_dir.glob("*.wav")), "unexpected output wav files after failure"


def test_steps_mode_cleans_temps_on_success(tmp_path: Path, monkeypatch):
    """A successful chained job must also leave no temporary directories."""
    _skip_if_missing_bin("bash")

    input_wav = _fake_input_wav(tmp_path)
    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"

    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    bin_dir = tmp_path / "workers"
    bin_dir.mkdir()
    fake_worker = _fake_demucs_worker(bin_dir, fail_after=None)
    monkeypatch.setenv("DEMUCS_WORKER", str(fake_worker))

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

    assert not list(output_dir.glob("_step_*")), "leftover _step_* temp directory after success"
    assert not (output_dir / "_routed").exists(), "leftover _routed directory after success"
    assert list(output_dir.glob("*.wav")), "expected output wav files after success"
