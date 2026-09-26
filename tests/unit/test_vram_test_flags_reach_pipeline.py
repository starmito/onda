"""Integration test: VRAM-test overrides reach inference_universal.py.

The backend now exports VOCAL_DIM_T / VOCAL_BATCH_SIZE / VOCAL_NUM_OVERLAP /
VOCAL_CHUNK_SIZE for every RoFormer/vocal step. pipeline.sh must respect those
variables so that a temporary override (e.g. segment_size=2048 vs 512) actually
changes the inference parameters and, therefore, the reported total_chunks.
"""

import json
import shutil
import subprocess
import textwrap
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
PIPELINE_SH = REPO_ROOT / "pipeline.sh"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


def test_vram_test_flags_change_total_chunks(tmp_path, monkeypatch):
    """Different VOCAL_DIM_T values must produce different total_chunks."""
    _skip_if_missing_bin("bash")

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
              dim_t: 1101
              num_overlap: 4
              batch_size: 1
            training:
              instruments: [vocals, other]
            """
        )
    )

    fake_inference = tmp_path / "fake_inference_universal.py"
    fake_inference.write_text(
        textwrap.dedent(
            """\
            import os
            import json
            import sys

            dim_t = int(os.environ.get("VOCAL_DIM_T", "1101"))
            batch = int(os.environ.get("VOCAL_BATCH_SIZE", "1"))
            overlap = int(os.environ.get("VOCAL_NUM_OVERLAP", "4"))
            chunk = int(os.environ.get("VOCAL_CHUNK_SIZE", "0"))
            # Deterministic total_chunks that changes with dim_t.
            total_chunks = max(1, dim_t // 100)

            status_path = os.environ.get("PIPELINE_STATUS_FILE", "pipeline_status.json")
            with open(status_path, "w") as f:
                json.dump(
                    {
                        "status": "running",
                        "progress": 0.5,
                        "total_chunks": total_chunks,
                        "dim_t": dim_t,
                        "batch": batch,
                        "overlap": overlap,
                        "chunk": chunk,
                    },
                    f,
                )
            print(f"total_chunks={total_chunks}")
            sys.exit(0)
            """
        )
    )

    patched_pipeline = tmp_path / "pipeline.sh"
    patched_pipeline.write_text(
        PIPELINE_SH.read_text().replace(
            "/app/inference_universal.py", str(fake_inference)
        )
    )

    input_wav = tmp_path / "input.wav"
    input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

    def run_with_flags(dim_t: int, batch: int, overlap: int, chunk: int) -> dict:
        output_dir = tmp_path / f"output_{dim_t}"
        status_file = tmp_path / f"pipeline_status_{dim_t}.json"
        monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))
        monkeypatch.setenv("ONDA_ALLOW_CPU", "1")
        monkeypatch.setenv("VOCAL_DIM_T", str(dim_t))
        monkeypatch.setenv("VOCAL_BATCH_SIZE", str(batch))
        monkeypatch.setenv("VOCAL_NUM_OVERLAP", str(overlap))
        monkeypatch.setenv("VOCAL_CHUNK_SIZE", str(chunk))

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
        print(result.stdout)
        assert result.returncode == 0, result.stdout
        assert status_file.exists(), "pipeline_status.json was not created"
        return json.loads(status_file.read_text())

    st2048 = run_with_flags(2048, 2, 6, 0)
    st512 = run_with_flags(512, 2, 6, 0)

    assert st2048["dim_t"] == 2048, f"VOCAL_DIM_T not honored: {st2048}"
    assert st2048["batch"] == 2, f"VOCAL_BATCH_SIZE not honored: {st2048}"
    assert st2048["overlap"] == 6, f"VOCAL_NUM_OVERLAP not honored: {st2048}"
    assert st2048["chunk"] == 0, f"VOCAL_CHUNK_SIZE not honored: {st2048}"

    assert st512["dim_t"] == 512, f"VOCAL_DIM_T not honored: {st512}"
    assert st512["batch"] == 2, f"VOCAL_BATCH_SIZE not honored: {st512}"
    assert st512["overlap"] == 6, f"VOCAL_NUM_OVERLAP not honored: {st512}"
    assert st512["chunk"] == 0, f"VOCAL_CHUNK_SIZE not honored: {st512}"

    assert st2048["total_chunks"] != st512["total_chunks"], (
        f"total_chunks did not differ: 2048->{st2048['total_chunks']}, 512->{st512['total_chunks']}"
    )
