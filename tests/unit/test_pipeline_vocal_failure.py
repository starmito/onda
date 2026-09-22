"""Test that pipeline.sh reports failed status when the vocal inference process dies.

The failure described in the bug report leaves pipeline_status.json stuck on
``status: "running"`` when ``inference_universal.py`` crashes (e.g. the length
mismatch).  We simulate a vocal model directory and a fake inference script that
exits with a non-zero code and an error message, then verify that the pipeline
updates the status to ``failed`` with the reason instead of staying ``running``.
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


def test_pipeline_reports_failed_when_vocal_inference_dies(tmp_path, monkeypatch):
    """If inference_universal.py exits non-zero, status must be failed with a reason."""
    _skip_if_missing_bin("bash")

    # Create a fake model directory that passes RoFormer detection.
    model_dir = tmp_path / "FakeVocalModel"
    model_dir.mkdir()
    (model_dir / "model.ckpt").write_bytes(b"fakeckpt")
    (model_dir / "model.yaml").write_text(
        textwrap.dedent(
            """\
            model:
              num_bands: 4
            audio:
              hop_length: 512
            inference:
              dim_t: 256
              num_overlap: 4
              batch_size: 1
            training:
              instruments: [vocals, other]
            """
        )
    )

    # Fake inference_universal.py that dies immediately with a clear message.
    fake_inference = tmp_path / "fake_inference_universal.py"
    fake_inference.write_text(
        textwrap.dedent(
            """\
            import sys
            sys.stderr.write("FATAL: forced vocal inference death\\n")
            sys.exit(7)
            """
        )
    )

    # Copy pipeline.sh and patch the hard-coded /app/inference_universal.py path.
    patched_pipeline = tmp_path / "pipeline.sh"
    patched_pipeline.write_text(
        PIPELINE_SH.read_text().replace(
            "/app/inference_universal.py", str(fake_inference)
        )
    )

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    output_dir = tmp_path / "output" / "input"
    status_file = tmp_path / "pipeline_status.json"
    monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))
    monkeypatch.setenv("ONDA_ALLOW_CPU", "1")

    cmd = [
        "bash",
        str(patched_pipeline),
        "--vocal-model", str(model_dir),
        "--vocal-keep", "instrumental",
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

    assert result.returncode != 0, result.stdout
    assert status_file.exists(), "pipeline_status.json was not created"
    final = json.loads(status_file.read_text())
    assert final.get("status") == "failed", f"status stayed {final.get('status')!r}"
    assert final.get("step") == "vocal"
    assert final.get("exit_code") != 0
    assert "forced vocal inference death" in result.stdout
