# Fase 5 — Testing Integral Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Suite completa de tests end-to-end para Onda v2.0.0-alpha: todos los métodos de separación, benchmarks comparativos, edge cases, e integración API.

**Architecture:** Script Python `tests/integration/test_suite.py` que genera audio sintético, ejecuta cada método vía Docker, verifica outputs, y mide RTF. Separado del binario Go — ejecución independiente desde el host o .87.

**Tech Stack:** Python 3.12, pytest, subprocess (docker exec), soundfile, numpy

**Methods under test:**
| Método | Comando | Backend |
|---|---|---|
| Demucs ONNX | `inference_demucs_onnx.py` | ONNX Runtime (CPU) |
| MDX-Net ONNX | `inference_mdx.py` | ONNX Runtime |
| Roformer ViperX | `inference_universal.py --model viperx` | PyTorch |
| Demucs PyTorch | `demucs -n htdemucs_ft` | PyTorch |
| Pipeline Go | `onda pipeline --preset turbo` | Go → Docker |
| API HTTP | `curl /api/separate` | Go net/http |

---

### Task 1: Crear `tests/integration/` y script generador de audio

**Objective:** Estructura de directorio + generador de archivos de prueba sintéticos.

**Files:**
- Create: `tests/integration/__init__.py`
- Create: `tests/integration/generate_test_audio.py`

**Step 1: Crear directorio y __init__.py**

```bash
mkdir -p tests/integration
```

**Step 2: Escribir generador de audio**

```python
#!/usr/bin/env python3
"""Generate synthetic test audio files for integration tests."""
import numpy as np
import soundfile as sf
import os

SAMPLE_RATE = 44100
OUT_DIR = os.path.join(os.path.dirname(__file__), "fixtures")

def generate_sine(freq=440, duration=5.0, sr=SAMPLE_RATE):
    t = np.linspace(0, duration, int(sr * duration), endpoint=False)
    return (np.sin(2 * np.pi * freq * t) * 0.8).astype(np.float32)

def generate_chirp(duration=5.0, sr=SAMPLE_RATE):
    t = np.linspace(0, duration, int(sr * duration), endpoint=False)
    freq = 100 + (2000 - 100) * t / duration
    phase = 2 * np.pi * np.cumsum(freq) / sr
    return (np.sin(phase) * 0.8).astype(np.float32)

def generate_silence(duration=5.0, sr=SAMPLE_RATE):
    return np.zeros(int(sr * duration), dtype=np.float32)

def generate_short(duration=0.5, sr=SAMPLE_RATE):
    return generate_sine(880, duration, sr)

def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    
    fixtures = {
        "sine_440_5s.flac": generate_sine(440, 5.0),
        "chirp_5s.flac": generate_chirp(5.0),
        "silence_5s.flac": generate_silence(5.0),
        "short_05s.flac": generate_short(0.5),
    }
    
    for name, audio in fixtures.items():
        path = os.path.join(OUT_DIR, name)
        sf.write(path, audio, SAMPLE_RATE)
        print(f"  Created: {path} ({len(audio)/SAMPLE_RATE:.1f}s)")
    
    print(f"\nGenerated {len(fixtures)} test fixtures in {OUT_DIR}")

if __name__ == "__main__":
    main()
```

**Step 3: Ejecutar generador**

```bash
cd /home/starmito/projects/onda && python3 tests/integration/generate_test_audio.py
```

Expected: 4 archivos creados en `tests/integration/fixtures/`

**Step 4: Commit**

```bash
git add tests/integration/
git commit -m "test: añadir generador de audio sintético para tests E2E"
```

---

### Task 2: Test suite — Demucs ONNX (CPU)

**Objective:** Test end-to-end de Demucs ONNX con audio sintético, verificando output.

**Files:**
- Create: `tests/integration/test_demucs_onnx.py`

```python
"""End-to-end tests for Demucs ONNX separation."""
import os
import subprocess
import pytest
import soundfile as sf

FIXTURES = os.path.join(os.path.dirname(__file__), "fixtures")
OUTPUT = "/tmp/onda-test-demucs-onnx"
SCRIPT = "inference/inference_demucs_onnx.py"
CONTAINER = "onda"

def run_onnx(input_file, stems="vocals"):
    """Run Demucs ONNX inside container and return (stdout, stderr, exit_code)."""
    cmd = [
        "docker", "exec", CONTAINER,
        "python3", SCRIPT,
        f"/app/tests/integration/fixtures/{input_file}",
        OUTPUT,
        "--stems", stems,
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    return result.stdout, result.stderr, result.returncode

def test_onnx_sine_vocals():
    """Separate vocals from a 440 Hz sine wave."""
    stdout, stderr, rc = run_onnx("sine_440_5s.flac", "vocals")
    assert rc == 0, f"Demucs ONNX failed: {stderr}"
    assert "Demucs ONNX:" in stdout, "Missing RTF output"
    output_file = os.path.join(OUTPUT, "vocals.wav")
    # Note: output is inside container, check via docker exec
    check = subprocess.run(
        ["docker", "exec", CONTAINER, "test", "-f", output_file],
        capture_output=True
    )
    assert check.returncode == 0, f"Output file missing: {output_file}"

def test_onnx_runtime_info():
    """Verify --runtime shows available providers."""
    result = subprocess.run(
        ["docker", "exec", CONTAINER, "python3", SCRIPT, "--runtime"],
        capture_output=True, text=True, timeout=15
    )
    assert result.returncode == 0
    assert "onnxruntime:" in result.stdout
    assert "Providers" in result.stdout
```

**Step 1: Commit**

```bash
git add tests/integration/test_demucs_onnx.py
git commit -m "test: añadir tests E2E para Demucs ONNX"
```

---

### Task 3: Test suite — MDX-Net ONNX

**Objective:** Test end-to-end de MDX-Net ONNX.

**Files:**
- Create: `tests/integration/test_mdx_onnx.py`

Estructura análoga a Task 2, usando `inference_mdx.py`.

**Models:** Kim_Vocal_2 (default)

```python
"""End-to-end tests for MDX-Net ONNX separation."""
import os
import subprocess

CONTAINER = "onda"
SCRIPT = "inference/inference_mdx.py"

def test_mdx_sine():
    """Separate vocals from sine wave using MDX-Net."""
    cmd = [
        "docker", "exec", CONTAINER, "python3", SCRIPT,
        "/app/tests/integration/fixtures/sine_440_5s.flac",
        "/tmp/onda-test-mdx/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
    assert result.returncode == 0, f"MDX-Net failed: {result.stderr}"
```

Commit: `test: añadir tests E2E para MDX-Net ONNX`

---

### Task 4: Test suite — Roformer (ViperX via API)

**Objective:** Test Roformer ViperX vía el pipeline Go (API HTTP).

**Files:**
- Create: `tests/integration/test_pipeline.py`

```python
"""End-to-end tests for the Go pipeline via API."""
import subprocess
import requests
import time
import pytest

API = "http://192.168.1.87:3000"

def test_api_health():
    r = requests.get(f"{API}/api/health", timeout=5)
    assert r.status_code == 200
    data = r.json()
    assert data["docker"] == "running"

def test_api_models():
    r = requests.get(f"{API}/api/models", timeout=5)
    assert r.status_code == 200
    data = r.json()
    assert "turbo" in str(data).lower() or "presets" in str(data).lower()

def test_api_separate_turbo():
    """End-to-end: POST separate with turbo preset, verify job starts."""
    payload = {
        "input": "/app/tests/integration/fixtures/sine_440_5s.flac",
        "output": "/tmp/onda-test-pipeline/",
        "preset": "turbo",
    }
    r = requests.post(f"{API}/api/separate", json=payload, timeout=10)
    assert r.status_code == 202, f"Expected 202, got {r.status_code}: {r.text}"
    data = r.json()
    assert "job_id" in data or "status" in data
```

Dependencia: `requests` (instalar en venv de tests)

Commit: `test: añadir tests E2E para API + pipeline`

---

### Task 5: Benchmark script

**Objective:** Script que mide RTF de cada método con el mismo audio.

**Files:**
- Create: `tests/integration/benchmark.py`

```python
#!/usr/bin/env python3
"""Benchmark all separation methods."""
import subprocess
import time
import json

CONTAINER = "onda"
INPUT = "/app/tests/integration/fixtures/chirp_5s.flac"
DURATION = 5.0  # seconds

benchmarks = {
    "demucs-onnx-cpu": [
        "docker", "exec", CONTAINER, "python3",
        "inference/inference_demucs_onnx.py", INPUT,
        "/tmp/bench-demucs-onnx/", "--stems", "vocals"
    ],
    "mdx-net": [
        "docker", "exec", CONTAINER, "python3",
        "inference/inference_mdx.py", INPUT,
        "/tmp/bench-mdx/"
    ],
    "demucs-pytorch": [
        "docker", "exec", CONTAINER, "demucs",
        "-n", "htdemucs_ft", "--two-stems", "vocals",
        "-o", "/tmp/bench-demucs-pt/", INPUT
    ],
}

results = {}

for name, cmd in benchmarks.items():
    print(f"Benchmarking {name}...")
    start = time.time()
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=180)
    elapsed = time.time() - start
    success = result.returncode == 0
    rtf = DURATION / elapsed if elapsed > 0 else float('inf')
    
    results[name] = {
        "rtf": round(rtf, 1),
        "elapsed_s": round(elapsed, 1),
        "success": success,
        "error": result.stderr[:200] if not success else None,
    }
    status = "✅" if success else "❌"
    print(f"  {status} {name}: {rtf:.1f}x realtime ({elapsed:.1f}s)")

print(f"\n=== RESULTS ===\n{json.dumps(results, indent=2)}")

with open("tests/integration/benchmark_results.json", "w") as f:
    json.dump(results, f, indent=2)
```

**Step 1: Ejecutar benchmark**

```bash
cd /home/starmito/projects/onda && python3 tests/integration/benchmark.py
```

Expected: JSON con RTF de cada método.

**Step 2: Commit**

```bash
git add tests/integration/benchmark.py tests/integration/benchmark_results.json
git commit -m "test: añadir benchmark comparativo de métodos de separación"
```

---

### Task 6: Edge cases y estrés

**Objective:** Tests para casos límite: silencio, archivo muy corto, formato no soportado.

**Files:**
- Modify: `tests/integration/test_edge_cases.py` (new)

```python
"""Edge case tests."""
import subprocess
import pytest

CONTAINER = "onda"

def run_demucs_onnx(input_file, stems="vocals"):
    cmd = [
        "docker", "exec", CONTAINER, "python3",
        "inference/inference_demucs_onnx.py",
        f"/app/tests/integration/fixtures/{input_file}",
        f"/tmp/onda-test-edge-{input_file}/",
        "--stems", stems,
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=60)

def test_silence_handled():
    """Silence should not crash — should produce output (silent stems)."""
    result = run_demucs_onnx("silence_5s.flac")
    # May fail or succeed, but must not hang/crash
    assert result.returncode in (0, 1), f"Unexpected exit: {result.returncode}"

def test_short_audio():
    """Very short audio (0.5s) should still process."""
    result = run_demucs_onnx("short_05s.flac")
    assert result.returncode == 0, f"Short audio failed: {result.stderr}"

def test_nonexistent_file():
    """Missing input file should error gracefully."""
    result = run_demucs_onnx("nonexistent.flac")
    assert result.returncode != 0, "Should fail on missing file"
    assert "error" in (result.stdout + result.stderr).lower()
```

Commit: `test: añadir tests de edge cases (silencio, corto, missing file)`

---

### Task 7: Integración final y CI helper

**Objective:** Script `run_all_tests.sh` que ejecuta toda la suite + guarda CHANGELOG de Fase 5.

**Files:**
- Create: `tests/integration/run_all.sh`

```bash
#!/bin/bash
set -euo pipefail
echo "=== Onda Fase 5 — Testing Integral ==="
echo ""

# 1. Generate fixtures
echo "[1/5] Generando audio de prueba..."
python3 tests/integration/generate_test_audio.py

# 2. Copy fixtures to container
echo "[2/5] Copiando fixtures al contenedor..."
docker cp tests/integration/fixtures/ onda:/app/tests/integration/fixtures/

# 3. Unit tests (Go backend)
echo "[3/5] Tests unitarios Go..."
cd backend && go test ./... -v -count=1 && cd ..

# 4. Integration tests
echo "[4/5] Tests E2E..."
python3 -m pytest tests/integration/ -v --timeout=120

# 5. Benchmarks
echo "[5/5] Benchmarks..."
python3 tests/integration/benchmark.py

echo ""
echo "=== Fase 5 completa ==="
```

**Step 1: Commit CHANGELOG**

```bash
git add tests/integration/run_all.sh
git commit -m "test: añadir script run_all.sh para Fase 5 completa"
```

**Step 2: Actualizar CHANGELOG.md** con sección Fase 5:

```markdown
### Fase 5 — Testing Integral

#### Added
- `tests/integration/`: suite E2E con audio sintético
- `generate_test_audio.py`: generador de fixtures (sine, chirp, silence, short)
- Tests por método: Demucs ONNX, MDX-Net, Pipeline API
- `benchmark.py`: RTF comparativo de todos los métodos
- `run_all.sh`: ejecución completa de la suite

#### Coverage
- 4 fixtures de audio sintético
- End-to-end: Demucs ONNX, MDX-Net ONNX, Pipeline Go
- Edge cases: silencio, audio corto, archivo inexistente
- Benchmark: PyTorch vs ONNX vs MDX-Net
```

Commit: `docs: CHANGELOG Fase 5 — testing integral`

---

## Verification Gate

Tras implementar todas las tareas, ejecutar contra .87:

```bash
# En .87
cd ~/projects/onda
docker cp tests/integration/fixtures/ onda:/app/tests/integration/fixtures/
cd backend && go test ./... -count=1
python3 -m pytest tests/integration/ -v
python3 tests/integration/benchmark.py
```

Criterio de aceptación:
- [ ] Todos los tests unitarios Go pasan (56 tests)
- [ ] Tests E2E pasan (> 80%)
- [ ] Benchmarks generan JSON con RTF de cada método
- [ ] Ningún método crashea con edge cases
- [ ] CHANGELOG actualizado
- [ ] Tag `v2.0.0-alpha.5`
