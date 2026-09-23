"""Tests para las herramientas de benchmark de Onda.

Estos tests no lanzan trabajos reales ni tocan la GPU; solo verifican que
los scripts existen, responden a --help y que el comparador decide bien
sobre JSON sintéticos.
"""

import json
import shutil
import subprocess
import sys

import pytest


@pytest.fixture
def repo_root():
    """Ruta absoluta a la raiz del repositorio."""
    import pathlib

    return pathlib.Path(__file__).resolve().parents[2]


@pytest.fixture
def synth_dir(repo_root):
    """Directorio dentro del repo para ficheros sintéticos temporales."""
    import pathlib

    d = repo_root / ".hermes" / "bench" / "_test_synth"
    d.mkdir(parents=True, exist_ok=True)
    yield d
    # Limpieza selectiva de los ficheros creados por este test.
    for f in d.glob("*.json"):
        f.unlink(missing_ok=True)


def _write_json(path, total_seconds, vram_peak, energy_vocals):
    data = {
        "versions": {
            "onda_backend": "v3.5.6",
            "onda_pipeline": "v3.5.6",
            "onda_frontend": "v3.5.6",
            "torch": "2.14.0",
            "torchvision": "0.29.0",
            "onnxruntime": "unknown",
            "demucs": "4.1.0",
        },
        "summary": {
            "total_seconds": total_seconds,
            "vram_peak_mb": vram_peak,
            "vram_mean_mb": 1200,
            "step_seconds": {"vocal": total_seconds / 2, "demucs": total_seconds / 2},
            "stems": [
                {"name": "vocals", "energy_rms_db": energy_vocals},
                {"name": "drums", "energy_rms_db": -15.0},
            ],
        },
    }
    path.write_text(json.dumps(data), encoding="utf-8")


def test_bench_baseline_exists(repo_root):
    """tools/bench-baseline.sh existe y es ejecutable."""
    script = repo_root / "tools" / "bench-baseline.sh"
    assert script.is_file(), f"No existe {script}"
    assert script.stat().st_mode & 0o111, f"{script} no es ejecutable"


def test_bench_compare_exists(repo_root):
    """tools/bench_compare.py existe y es ejecutable."""
    script = repo_root / "tools" / "bench_compare.py"
    assert script.is_file(), f"No existe {script}"
    assert script.stat().st_mode & 0o111, f"{script} no es ejecutable"


def test_bench_baseline_help(repo_root):
    """bench-baseline.sh --help devuelve 0 e imprime uso."""
    script = repo_root / "tools" / "bench-baseline.sh"
    result = subprocess.run(
        ["bash", str(script), "--help"],
        cwd=str(repo_root),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"--help falló: {result.stderr}"
    assert "Uso:" in result.stdout, "No aparece la ayuda de uso"
    assert "--label" in result.stdout, "No aparece --label en la ayuda"


def test_bench_compare_help(repo_root):
    """bench_compare.py --help devuelve 0 e imprime uso."""
    script = repo_root / "tools" / "bench_compare.py"
    result = subprocess.run(
        [sys.executable, str(script), "--help"],
        cwd=str(repo_root),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"--help falló: {result.stderr}"
    assert "base" in result.stdout and "new" in result.stdout, "Faltan argumentos en la ayuda"


def test_bench_compare_equal(repo_root, synth_dir):
    """Dos benchmarks idénticos deben dar veredicto 'igual' y salir 0."""
    base = synth_dir / "base_equal.json"
    new = synth_dir / "new_equal.json"
    _write_json(base, total_seconds=60.0, vram_peak=1500, energy_vocals=-10.0)
    _write_json(new, total_seconds=60.0, vram_peak=1500, energy_vocals=-10.0)

    script = repo_root / "tools" / "bench_compare.py"
    result = subprocess.run(
        [sys.executable, str(script), str(base), str(new)],
        cwd=str(repo_root),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"Esperaba 0 (igual), salió {result.returncode}:\n{result.stdout}\n{result.stderr}"
    assert "igual" in result.stdout.lower(), f"No aparece 'igual' en la salida:\n{result.stdout}"


def test_bench_compare_change(repo_root, synth_dir):
    """Un benchmark con diferencias claras debe dar veredicto 'cambio' y salir 1."""
    base = synth_dir / "base_change.json"
    new = synth_dir / "new_change.json"
    _write_json(base, total_seconds=60.0, vram_peak=1500, energy_vocals=-10.0)
    # Cambios que superan todos los umbrales por defecto.
    _write_json(new, total_seconds=100.0, vram_peak=2000, energy_vocals=-5.0)

    script = repo_root / "tools" / "bench_compare.py"
    result = subprocess.run(
        [sys.executable, str(script), str(base), str(new)],
        cwd=str(repo_root),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 1, f"Esperaba 1 (cambio), salió {result.returncode}:\n{result.stdout}\n{result.stderr}"
    assert "cambio" in result.stdout.lower(), f"No aparece 'cambio' en la salida:\n{result.stdout}"


def test_bench_compare_version_mismatch(repo_root, synth_dir):
    """Si las versiones difieren, el comparador avisa pero no cambia el veredicto de métricas iguales."""
    base = synth_dir / "base_version.json"
    new = synth_dir / "new_version.json"
    _write_json(base, total_seconds=60.0, vram_peak=1500, energy_vocals=-10.0)
    _write_json(new, total_seconds=60.0, vram_peak=1500, energy_vocals=-10.0)

    new_data = json.loads(new.read_text(encoding="utf-8"))
    new_data["versions"]["onda_backend"] = "v3.5.7"
    new.write_text(json.dumps(new_data), encoding="utf-8")

    script = repo_root / "tools" / "bench_compare.py"
    result = subprocess.run(
        [sys.executable, str(script), str(base), str(new)],
        cwd=str(repo_root),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"Esperaba 0 (métricas iguales), salió {result.returncode}"
    assert "v3.5.6" in result.stdout and "v3.5.7" in result.stdout, "No se reportó el desajuste de versiones"
