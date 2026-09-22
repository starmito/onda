"""Tests que garantizan que la imagen lleva todo lo que pipeline.sh invoca.

No requieren docker: leen ``pipeline.sh`` y el ``Dockerfile`` directamente.
Además verifican que un fallo del tracker deja rastro en los logs en vez de
irse a ``/dev/null``.
"""

import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
PIPELINE_SH = REPO_ROOT / "pipeline.sh"
DOCKERFILE = REPO_ROOT / "Dockerfile"
TOOLS_DIR = REPO_ROOT / "tools"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


def _dockerfile_copy_targets():
    """Return mapping dest_path -> source_path from Dockerfile COPY statements.

    Directory copies (source ending with ``/``) are recorded under the dest
    directory key so callers can tell that the whole subtree is copied.
    """
    text = DOCKERFILE.read_text()
    targets = {}
    for m in re.finditer(r"^COPY\s+(.*?)\s+(.*?)\s*$", text, re.MULTILINE):
        sources = m.group(1).split()
        dest = m.group(2).rstrip("/")
        for src in sources:
            if src.endswith("/"):
                targets[dest] = src
            else:
                basename = os.path.basename(src)
                targets[f"{dest}/{basename}"] = src
    return targets


def _pipeline_invoked_paths():
    """Extract absolute file paths that pipeline.sh executes as commands."""
    text = PIPELINE_SH.read_text()
    patterns = [
        r"/app/tools/[a-zA-Z0-9_]+\.py",
        r"/app/inference_[a-zA-Z0-9_]+\.py",
        r"/app/keydetect\.py",
        r"/app/onda/detect_gpu\.sh",
        r"/usr/local/bin/detect_gpu\.sh",
        r"/app/pipeline\.sh",
    ]
    paths = set()
    for pat in patterns:
        paths.update(re.findall(pat, text))
    return paths


class TestDockerfileCopiesTools:
    """El Dockerfile debe transportar tools/ completo a la imagen."""

    def test_dockerfile_copies_tools_directory(self):
        """Se copia el directorio entero, no solo demucs_worker.py."""
        targets = _dockerfile_copy_targets()
        assert "/app/tools" in targets, (
            "Dockerfile no copia el directorio tools/ completo; "
            f"COPY encontrados: {list(targets)}"
        )
        assert targets["/app/tools"] == "tools/"

    def test_every_repo_tool_is_copied(self):
        """Cada fichero de tools/ llega a /app/tools/."""
        targets = _dockerfile_copy_targets()
        assert "/app/tools" in targets
        for f in TOOLS_DIR.iterdir():
            if f.is_file():
                expected = f"/app/tools/{f.name}"
                assert expected in targets or "/app/tools" in targets, (
                    f"{f.name} no se copia a la imagen"
                )


class TestPipelineInvokedPathsExist:
    """Cada ruta absoluta invocada por pipeline.sh existe en repo o imagen."""

    def test_invoked_paths_are_present_in_repo_or_dockerfile(self):
        """Ninguna ruta ejecutada por el pipeline queda huérfana."""
        targets = _dockerfile_copy_targets()
        invoked = _pipeline_invoked_paths()
        assert invoked, "no se extrajeron rutas invocadas de pipeline.sh"

        def _is_copied_in_dockerfile(p: str) -> bool:
            if p in targets:
                return True
            # Directory copies cover every file under the destination directory.
            for dest, src in targets.items():
                if src.endswith("/") and p.startswith(dest + "/"):
                    return True
            return False

        for p in invoked:
            # Determinar ruta relativa en el repo según destino en imagen.
            if p.startswith("/app/tools/"):
                src = p.replace("/app/tools/", "tools/")
            elif p.startswith("/app/"):
                src = p.replace("/app/", "", 1)
            elif p.startswith("/usr/local/bin/"):
                src = p.replace("/usr/local/bin/", "")
            else:
                src = p

            in_repo = (REPO_ROOT / src).exists()
            in_dockerfile = _is_copied_in_dockerfile(p)
            assert in_repo or in_dockerfile, (
                f"{p} es invocado por pipeline.sh pero no existe en el repo "
                f"({src}) ni se copia en el Dockerfile"
            )


class TestTrackerFailureIsVisible:
    """Un fallo del progress tracker debe dejarse ver, no tragarse."""

    def _write_fake_worker(self, bin_dir: Path) -> Path:
        """Fake Demucs worker that emits progress events and fake stems."""
        fake = bin_dir / "demucs_worker.py"
        script = r'''#!/usr/bin/env python3
import argparse
import json
import os
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

print(json.dumps({"event": "progress", "pct": 50}), flush=True)
print(json.dumps({"event": "done", "seconds": 1.0}), flush=True)

track = Path(args.input).stem
outdir = Path(args.out) / args.model / track
outdir.mkdir(parents=True, exist_ok=True)
for stem in ("drums", "bass", "other", "vocals"):
    (outdir / f"{stem}.wav").write_bytes(b"RIFF" + b"\x00" * 100)
'''
        fake.write_text(script)
        fake.chmod(0o755)
        return fake

    def _write_failing_tracker(self, bin_dir: Path) -> Path:
        """Fake tracker that exits with a diagnostic message."""
        fake = bin_dir / "progress_tracker.py"
        fake.write_text(
            '#!/usr/bin/env python3\n'
            'import sys\n'
            'print("FAKE_TRACKER_INTENTIONAL_FAILURE", file=sys.stderr)\n'
            'sys.exit(7)\n'
        )
        fake.chmod(0o755)
        return fake

    def test_tracker_failure_is_logged(self, tmp_path, monkeypatch):
        """Si el tracker falla, el pipeline escribe el error en stderr."""
        _skip_if_missing_bin("bash")

        input_wav = tmp_path / "input.wav"
        input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

        output_dir = tmp_path / "output" / "input"
        status_file = tmp_path / "pipeline_status.json"
        monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

        bin_dir = tmp_path / "helpers"
        bin_dir.mkdir()
        fake_worker = self._write_fake_worker(bin_dir)
        fake_tracker = self._write_failing_tracker(bin_dir)
        monkeypatch.setenv("DEMUCS_WORKER", str(fake_worker))
        monkeypatch.setenv("PROGRESS_TRACKER", str(fake_tracker))

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
            capture_output=True,
            text=True,
        )

        # The pipeline must not hide the tracker failure.
        combined = result.stdout + result.stderr
        assert "Progress tracker failed" in result.stderr, (
            f"tracker failure not logged; stderr:\n{result.stderr}\n"
            f"stdout:\n{result.stdout}"
        )
        assert "FAKE_TRACKER_INTENTIONAL_FAILURE" in combined, (
            "tracker diagnostic not surfaced"
        )
