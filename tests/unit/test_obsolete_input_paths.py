"""Detect obsolete input/output paths in build/orchestration files.

The application uses the configurable data root (``ONDA_DATA_DIR``), whose
default value is ``/app/data``. Any hard-coded ``/input/``, ``/output/``,
``/app/input/`` or ``/app/output/`` in build/orchestration files is therefore
obsolete and should fail. Only paths under ``/app/data/`` (the default root)
are allowed.
"""

import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]

# Files that orchestrate builds or deployments. Runtime scripts that legitimately
# translate host ./input/ to container paths via ONDA_DATA_DIR (e.g. pipeline.sh)
# are NOT included here; this check targets build/orchestration drift.
BUILD_FILES = [
    ROOT / "Makefile",
    ROOT / "build.sh",
    ROOT / "deploy.sh",
    ROOT / "onda.sh",
    ROOT / "entrypoint.sh",
    ROOT / "docker-compose.yml",
    ROOT / "docker-compose.cuda.yml",
]

# Any .sh file under scripts/ is considered a build/orchestration helper.
BUILD_FILES.extend((ROOT / "scripts").glob("*.sh"))

# Match /input/ and /output/ except when they live under /app/data/ (the default
# data root). This flags both the obsolete bare /input/ and the legacy
# /app/input/ symlink paths.
_OBSOLETE_INPUT_RE = re.compile(r"(?<!/app/data)/input/")
_OBSOLETE_OUTPUT_RE = re.compile(r"(?<!/app/data)/output/")


@pytest.mark.parametrize("path", BUILD_FILES, ids=lambda p: p.name)
def test_no_obsolete_input_paths(path: Path):
    assert path.exists(), f"{path} no existe"
    text = path.read_text(encoding="utf-8")
    obsolete = []
    for lineno, line in enumerate(text.splitlines(), start=1):
        if _OBSOLETE_INPUT_RE.search(line) or _OBSOLETE_OUTPUT_RE.search(line):
            obsolete.append((lineno, line.strip()))
    assert not obsolete, (
        f"{path.name} contiene referencias obsoletas a /input/ o /output/ "
        f"(debe usar ONDA_DATA_DIR, por defecto /app/data): {obsolete}"
    )
