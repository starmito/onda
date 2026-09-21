"""Test that pipeline.sh preserves diagnostics when a step fails.

When the first step fails (here, a missing vocal model path), the pipeline
must update pipeline_status.json to status "failed" with step, error and
exit_code, copy the diagnostic information to a persistent directory, and
print the last error lines to the pipeline log.
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
MISSING_MODEL = "/nonexistent/models/IntentionallyMissingModel"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


def test_pipeline_preserves_diagnostics_on_step_failure(tmp_path, monkeypatch):
    """pipeline.sh must report failure details and keep the diagnostics."""
    _skip_if_missing_bin("bash")

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

    cmd = [
        "bash",
        str(PIPELINE_SH),
        "--vocal-model", MISSING_MODEL,
        "--output", str(output_dir),
        str(input_wav),
    ]

    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )

    # The pipeline must still exit with a non-zero status.
    assert result.returncode != 0, result.stdout

    # Status file must report the failure with step, error and exit_code.
    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed"
    assert final.get("step") == "vocal"
    assert final.get("exit_code") != 0
    assert MISSING_MODEL in final.get("error", "")

    # The failure message must be visible in the pipeline log.
    assert "❌ Paso vocal fallo. Ultimas lineas:" in result.stdout
    assert MISSING_MODEL in result.stdout

    # The diagnostics directory must contain the persisted stderr log.
    diagnostics_dir = output_dir / "_failed_vocal"
    stderr_log = diagnostics_dir / "stderr.log"
    assert diagnostics_dir.is_dir(), f"diagnostics dir missing: {diagnostics_dir}"
    assert stderr_log.exists(), f"persisted stderr log missing: {stderr_log}"
    assert MISSING_MODEL in stderr_log.read_text()
