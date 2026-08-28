"""Tests for the onda package version metadata."""

import os
import re

import pytest


@pytest.fixture
def project_root():
    """Return the absolute project root."""
    return os.path.dirname(os.path.dirname(os.path.dirname(__file__)))


def test_version_file_exists(project_root):
    """onda/_version.py must exist and declare __version__."""
    version_path = os.path.join(project_root, "onda", "_version.py")
    assert os.path.isfile(version_path), f"version file not found: {version_path}"

    with open(version_path, "r", encoding="utf-8") as f:
        content = f.read()

    match = re.search(r'^__version__\s*=\s*["\']([^"\']+)["\']', content, re.MULTILINE)
    assert match is not None, "__version__ not found in onda/_version.py"
    version = match.group(1)
    assert version, "__version__ must not be empty"
    assert version.startswith("v"), f"version should start with 'v', got {version!r}"
    assert re.match(r"^v\d+\.\d+\.\d+", version), f"version {version!r} does not look like vX.Y.Z"


def test_version_matches_version_file(project_root):
    """onda/_version.py and the top-level VERSION file must agree."""
    version_path = os.path.join(project_root, "onda", "_version.py")
    with open(version_path, "r", encoding="utf-8") as f:
        py_version_match = re.search(
            r'^__version__\s*=\s*["\']([^"\']+)["\']', f.read(), re.MULTILINE
        )
    assert py_version_match is not None
    py_version = py_version_match.group(1)

    top_version_path = os.path.join(project_root, "VERSION")
    assert os.path.isfile(top_version_path), "top-level VERSION file missing"
    with open(top_version_path, "r", encoding="utf-8") as f:
        top_version = f.read().strip()

    assert py_version == top_version, (
        f"onda/_version.py ({py_version}) != VERSION ({top_version})"
    )


def test_version_import(project_root):
    """Importing onda must expose __version__ matching the version file."""
    # Run in a subprocess so the project-root VERSION file is authoritative.
    import subprocess
    import sys

    code = (
        "import onda; "
        "print(onda.__version__)"
    )
    result = subprocess.run(
        [sys.executable, "-c", code],
        cwd=project_root,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"import failed: {result.stderr}"
    version = result.stdout.strip()
    assert re.match(r"^v\d+\.\d+\.\d+", version), f"imported version {version!r} invalid"

    with open(os.path.join(project_root, "VERSION"), "r", encoding="utf-8") as f:
        assert version == f.read().strip()
