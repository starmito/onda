"""Test that pipeline.sh reports real Demucs progress parsed from stderr.

This test replaces the ``demucs`` CLI with a fake that emits tqdm-style
progress bars to stderr.  It then runs pipeline.sh in legacy mode and checks
that ``pipeline_status.json`` reflects a smooth progress curve rather than a
jump produced by counting output WAV files.
"""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest


REPO_ROOT = Path(__file__).resolve().parents[2]
PIPELINE_SH = REPO_ROOT / "pipeline.sh"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


@pytest.fixture
def fake_demucs(tmp_path, monkeypatch):
    """Create a fake ``demucs`` command and prepend it to PATH."""
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()

    fake = bin_dir / "demucs"
    fake.write_text(
        r"""#!/usr/bin/env bash
# Fake demucs that emits tqdm-style progress to stderr and writes stems.
output=""
model="htdemucs_ft"
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) output="$2"; shift 2 ;;
        -n) model="$2"; shift 2 ;;
        -d|--device|--shifts|--segment|-j) shift 2 ;;
        *) input="$1"; shift ;;
    esac
done

name=$(basename "${input%.*}")
outdir="${output}/${model}/${name}"
mkdir -p "${outdir}"

# Emit progress bars with carriage returns, as real demucs/tqdm does.
for pct in 10 25 50 75 100; do
    bar=$(printf '%*s' $((pct / 5)) '' | tr ' ' '█')
    printf '\r%3d%%|%-20s| %d/4 [00:00<00:00, 1.00it/s]' \
        "$pct" "$bar" "$((pct / 25 + 1))" >&2
    sleep 0.7
done
printf '\n' >&2

# Create the expected stems so the rest of the pipeline can finish.
touch "${outdir}/drums.wav" \
      "${outdir}/bass.wav" \
      "${outdir}/other.wav" \
      "${outdir}/vocals.wav"
"""
    )
    fake.chmod(0o755)

    monkeypatch.setenv("PATH", f"{bin_dir}{os.pathsep}{os.environ['PATH']}")
    return fake


def test_pipeline_demucs_reports_real_progress(fake_demucs, tmp_path, monkeypatch):
    """pipeline.sh must derive Demucs progress from stderr, not WAV counts."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--device", "cpu",
        "--demucs-keep", "all",
        "--output", str(output_dir),
        str(input_wav),
    ]

    # Poll the status file while the pipeline runs so we can observe intermediate
    # progress updates rather than only the final "complete" state.
    demucs_progress_seen = False
    with subprocess.Popen(
        cmd,
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
                if data.get("step") == "demucs" and 0 < data.get("progress", 0) < 1.0:
                    demucs_progress_seen = True
            # Short sleep to avoid hammering the filesystem.
            import time
            time.sleep(0.05)

    stdout, stderr = proc.communicate()
    assert proc.returncode == 0, stderr or stdout

    assert demucs_progress_seen, (
        "expected intermediate Demucs progress (0 < progress < 1) to be reported; "
        f"last status was: {status_file.read_text() if status_file.exists() else 'missing'}"
    )

    final = json.loads(status_file.read_text())
    assert final.get("status") == "done"
    assert final.get("step") == "complete"
