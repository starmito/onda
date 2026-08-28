"""Detect obsolete /input/ paths in build/orchestration files.

The API uses /app/input/; any bare /input/ reference in Makefile or build
scripts is obsolete and should fail. /app/input/ is explicitly allowed.
"""

import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]

# Files that orchestrate builds or deployments. Runtime scripts that legitimately
# translate host ./input/ to container /app/input/ (e.g. pipeline.sh) are NOT
# included here; this check targets build/orchestration drift.
BUILD_FILES = [
    ROOT / "Makefile",
    ROOT / "build.sh",
    ROOT / "deploy.sh",
    ROOT / "onda.sh",
    ROOT / "entrypoint.sh",
    ROOT / "docker-compose.yml",
    ROOT / "docker-compose.cuda.yml",
    ROOT / "docker-compose.rocm.yml",
]

# Any .sh file under scripts/ is considered a build/orchestration helper.
BUILD_FILES.extend((ROOT / "scripts").glob("*.sh"))

# Match /input/ but not when preceded by /app (i.e. /app/input/ is OK).
_OBSOLETE_INPUT_RE = re.compile(r"(?<!/app)/input/")


@pytest.mark.parametrize("path", BUILD_FILES, ids=lambda p: p.name)
def test_no_obsolete_input_paths(path: Path):
    assert path.exists(), f"{path} no existe"
    text = path.read_text(encoding="utf-8")
    obsolete = []
    for lineno, line in enumerate(text.splitlines(), start=1):
        if _OBSOLETE_INPUT_RE.search(line):
            obsolete.append((lineno, line.strip()))
    assert not obsolete, (
        f"{path.name} contiene referencias obsoletas a /input/ "
        f"(debe ser /app/input/): {obsolete}"
    )
