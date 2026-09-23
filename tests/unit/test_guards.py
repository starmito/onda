"""Tests para los guardianes del repo (licencias + dependencias)."""

import os
import shutil
import subprocess

import pytest


@pytest.fixture
def repo_root():
    """Ruta absoluta a la raiz del repositorio."""
    return os.path.dirname(os.path.dirname(os.path.dirname(__file__)))


def _run_guard(repo_root, script_name):
    """Ejecuta un guardián de tools/ y devuelve CompletedProcess."""
    if not shutil.which('bash'):
        pytest.skip('bash no esta disponible en este entorno')

    script_path = os.path.join(repo_root, 'tools', script_name)
    if not os.path.isfile(script_path):
        pytest.skip(f'{script_path} no encontrado')

    return subprocess.run(
        ['bash', script_path],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )


def test_check_deps(repo_root):
    """tools/check-deps.sh debe pasar (requirements fijados vs lock)."""
    result = _run_guard(repo_root, 'check-deps.sh')
    assert result.returncode == 0, (
        f"check-deps.sh fallo (exit {result.returncode}):\n"
        f"STDOUT:\n{result.stdout}\nSTDERR:\n{result.stderr}"
    )


def test_check_licenses(repo_root):
    """tools/check-licenses.sh debe pasar (licencias + go mod tidy)."""
    if not shutil.which('go'):
        pytest.skip('go no esta instalado; no se puede verificar go mod tidy')

    result = _run_guard(repo_root, 'check-licenses.sh')
    assert result.returncode == 0, (
        f"check-licenses.sh fallo (exit {result.returncode}):\n"
        f"STDOUT:\n{result.stdout}\nSTDERR:\n{result.stderr}"
    )


def test_check_gitignore(repo_root):
    """tools/check-gitignore.sh debe pasar (.hermes/ no trackeada)."""
    result = _run_guard(repo_root, 'check-gitignore.sh')
    assert result.returncode == 0, (
        f"check-gitignore.sh fallo (exit {result.returncode}):\n"
        f"STDOUT:\n{result.stdout}\nSTDERR:\n{result.stderr}"
    )

