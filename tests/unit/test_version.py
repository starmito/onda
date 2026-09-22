"""Tests for the onda package version metadata.

Guardian de la Fase 1.2 del plan pre-v4.0: la version vive en CUATRO sitios
(`VERSION`, `onda/_version.py`, `frontend/package.json`, `pyproject.toml`) y el
tag de release va en el commit de release, nunca en el merge a `main`.

Si alguno de los sitios se descuadra —o el tag apunta a un merge o a una rama
perdida— la suite falla. Cuando la version en curso aun no tiene tag, los tests
del tag se saltan en vez de fallar, para no romper la suite a mitad de un bump.
"""

import json
import os
import re
import subprocess

import pytest


@pytest.fixture
def project_root():
    """Return the absolute project root."""
    return os.path.dirname(os.path.dirname(os.path.dirname(__file__)))


def _norm(version):
    """Normaliza una version quitando el prefijo 'v' para poder compararlas."""
    return (version or "").strip().lstrip("v")


def _read_python_version(root):
    """Lee __version__ de onda/_version.py."""
    version_path = os.path.join(root, "onda", "_version.py")
    assert os.path.isfile(version_path), f"version file not found: {version_path}"
    with open(version_path, "r", encoding="utf-8") as f:
        content = f.read()
    match = re.search(r'^__version__\s*=\s*["\']([^"\']+)["\']', content, re.MULTILINE)
    assert match is not None, "__version__ not found in onda/_version.py"
    return match.group(1)


def _git(root, *args):
    """Ejecuta git dentro del repo; devuelve (returncode, stdout, stderr)."""
    try:
        result = subprocess.run(
            ["git", *args], cwd=root, capture_output=True, text=True, timeout=20
        )
    except (FileNotFoundError, subprocess.TimeoutExpired) as exc:  # pragma: no cover
        pytest.skip(f"git no disponible: {exc}")
    return result.returncode, result.stdout.strip(), result.stderr.strip()


def test_version_file_exists(project_root):
    """onda/_version.py must exist and declare __version__."""
    version = _read_python_version(project_root)
    assert version, "__version__ must not be empty"
    assert version.startswith("v"), f"version should start with 'v', got {version!r}"
    assert re.match(r"^v\d+\.\d+\.\d+", version), f"version {version!r} does not look like vX.Y.Z"


def test_version_matches_version_file(project_root):
    """onda/_version.py and the top-level VERSION file must agree."""
    py_version = _read_python_version(project_root)

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
    import sys

    code = "import onda; print(onda.__version__)"
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


def test_all_version_sites_agree(project_root):
    """Los CUATRO sitios de version tienen que decir lo mismo."""
    sites = {"onda/_version.py": _read_python_version(project_root)}

    with open(os.path.join(project_root, "VERSION"), "r", encoding="utf-8") as f:
        sites["VERSION"] = f.read().strip()

    package_json = os.path.join(project_root, "frontend", "package.json")
    assert os.path.isfile(package_json), "frontend/package.json missing"
    with open(package_json, "r", encoding="utf-8") as f:
        sites["frontend/package.json"] = json.load(f).get("version", "")

    pyproject = os.path.join(project_root, "pyproject.toml")
    assert os.path.isfile(pyproject), "pyproject.toml missing"
    with open(pyproject, "r", encoding="utf-8") as f:
        content = f.read()
    match = re.search(r'^version\s*=\s*["\']([^"\']+)["\']', content, re.MULTILINE)
    assert match is not None, "version no encontrada en pyproject.toml"
    sites["pyproject.toml"] = match.group(1)

    normalized = {name: _norm(value) for name, value in sites.items()}
    unique = set(normalized.values())
    assert len(unique) == 1, f"versiones descuadradas entre ficheros: {normalized}"
    assert re.match(r"^\d+\.\d+\.\d+", unique.pop()), "la version no tiene forma X.Y.Z"


def test_release_tag_matches_version(project_root):
    """El tag del release: existe para esta version, no es un merge y vive en main."""
    with open(os.path.join(project_root, "VERSION"), "r", encoding="utf-8") as f:
        version = f.read().strip()

    if _git(project_root, "rev-parse", "--is-inside-work-tree")[0] != 0:
        pytest.skip("no es un checkout de git")

    expected = f"onda-{version}"
    if not _git(project_root, "tag", "-l", expected)[1]:
        pytest.skip(f"aun no hay tag {expected} para la version en curso")

    tag_commit = _git(project_root, "rev-list", "-n", "1", expected)[1]
    assert tag_commit, f"el tag {expected} no resuelve a un commit"

    line = _git(project_root, "rev-list", "--parents", "-n", "1", tag_commit)[1]
    parents = line.split()[1:]
    assert len(parents) <= 1, (
        f"el tag {expected} apunta a un commit de merge ({tag_commit[:8]}); "
        "el tag va en el commit de release, no en el merge a main"
    )

    if _git(project_root, "rev-parse", "--verify", "main")[0] != 0:
        pytest.skip("este checkout no tiene rama main")

    rc, _, _ = _git(project_root, "merge-base", "--is-ancestor", tag_commit, "main")
    assert rc == 0, (
        f"el tag {expected} ({tag_commit[:8]}) NO esta dentro de main; "
        "apunta a una rama que nunca se mezclo"
    )
