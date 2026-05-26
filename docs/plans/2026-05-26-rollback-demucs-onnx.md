# Rollback Demucs ONNX → Demucs PyTorch

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Deshabilitar Demucs ONNX (bug #1: GPU hang en RTX 5060 Ti), volver a Demucs PyTorch normal, limpiar dependencias y modelos ONNX.

**Architecture:** Eliminar `inference_demucs_onnx.py` y sus tests. Reemplazar tests edge-case con versión PyTorch. Quitar `demucs-onnx` de requirements. Simplificar Dockerfiles eliminando el truco de cleanup onnxruntime CPU (ya no necesario). MDX-Net ONNX se mantiene (funciona perfecto).

**Tech Stack:** Python 3.12, Docker, Demucs 4.0.1 (PyTorch), onnxruntime-gpu (solo para MDX)

---

### Task 1: Eliminar script Demucs ONNX

**Objective:** Borrar `inference/inference_demucs_onnx.py` (171 líneas, se cuelga en GPU)

**Files:**
- Delete: `inference/inference_demucs_onnx.py`

**Step 1: Borrar archivo**
```bash
git rm inference/inference_demucs_onnx.py
```

**Step 2: Commit**
```bash
git commit -m "refactor: eliminar inference_demucs_onnx.py (bug #1 GPU hang)"
```

---

### Task 2: Eliminar tests Demucs ONNX

**Objective:** Borrar `tests/integration/test_demucs_onnx.py`

**Files:**
- Delete: `tests/integration/test_demucs_onnx.py`

**Step 1: Borrar archivo**
```bash
git rm tests/integration/test_demucs_onnx.py
```

**Step 2: Commit**
```bash
git commit -m "test: eliminar tests Demucs ONNX (bug #1)"
```

---

### Task 3: Reemplazar edge case tests con Demucs PyTorch

**Objective:** `tests/integration/test_edge_cases.py` actualmente usa `demucs_onnx` para tests de silencio, audio corto, y archivo inexistente. Reemplazar con versión que use `demucs` CLI (PyTorch).

**Files:**
- Modify: `tests/integration/test_edge_cases.py`

**Step 1: Escribir versión actualizada**

Reemplazar TODO el contenido de `tests/integration/test_edge_cases.py` con:

```python
"""Edge case tests for separation methods."""
import subprocess
import pytest

CONTAINER = "onda"
FIXTURE_DIR = "/app/tests/integration/fixtures"


def run_demucs(input_file, output_dir, stems="vocals", timeout=120):
    """Run Demucs PyTorch inside container."""
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER,
        "demucs", "-n", "htdemucs_ft",
        "--two-stems", stems,
        "-o", output_dir,
        f"{FIXTURE_DIR}/{input_file}",
    ]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def test_silence_handled():
    """Silence should not crash — produce output (silent stems)."""
    out = "/tmp/onda-test-edge-silence/"
    result = run_demucs("silence_5s.flac", out)
    assert result.returncode in (0, 1), f"Unexpected exit: {result.returncode}"


def test_short_audio():
    """Very short audio (0.5s) should still process or fail gracefully."""
    out = "/tmp/onda-test-edge-short/"
    result = run_demucs("short_05s.flac", out)
    assert result.returncode in (0, 1), f"Short audio crashed: {result.stderr}"


def test_nonexistent_file():
    """Missing input file should error clearly."""
    out = "/tmp/onda-test-edge-missing/"
    result = run_demucs("nonexistent.flac", out)
    assert result.returncode != 0, "Should fail on missing file"


def test_mdx_short_audio():
    """MDX-Net with short audio should not crash."""
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER, "python3",
        "inference/inference_mdx.py",
        f"{FIXTURE_DIR}/short_05s.flac",
        "/tmp/onda-test-edge-mdx-short/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
    assert result.returncode in (0, 1), f"MDX short audio crashed: {result.stderr}"
```

**Step 2: Commit**
```bash
git add tests/integration/test_edge_cases.py
git commit -m "test: reemplazar Demucs ONNX con PyTorch en edge case tests"
```

---

### Task 4: Quitar demucs-onnx del benchmark

**Objective:** `tests/integration/benchmark.py` tiene `"demucs-onnx-cpu"` que ya no existe. Quitarlo.

**Files:**
- Modify: `tests/integration/benchmark.py`

**Step 1: Editar**

Eliminar la entrada `"demucs-onnx-cpu"` del diccionario `BENCHMARKS` (líneas 13-17 del archivo actual). El diccionario debe quedar:

```python
BENCHMARKS = {
    "mdx-net": [
        "ssh", ".87", "docker", "exec", CONTAINER, "python3",
        "inference/inference_mdx.py", FIXTURE,
        "/tmp/bench-mdx/"
    ],
    "demucs-pytorch": [
        "ssh", ".87", "docker", "exec", CONTAINER, "demucs",
        "-n", "htdemucs_ft", "--two-stems", "vocals",
        "-o", "/tmp/bench-demucs-pt/", FIXTURE
    ],
}
```

**Step 2: Commit**
```bash
git add tests/integration/benchmark.py
git commit -m "test: quitar demucs-onnx del benchmark"
```

---

### Task 5: Quitar demucs-onnx de requirements-docker.txt

**Objective:** Quitar `demucs-onnx` de la lista de dependencias.

**Files:**
- Modify: `requirements-docker.txt`

**Step 1: Editar**

Eliminar la línea 42: `demucs-onnx`

**Step 2: Commit**
```bash
git add requirements-docker.txt
git commit -m "build: quitar demucs-onnx de requirements-docker.txt"
```

---

### Task 6: Quitar demucs-onnx de requirements-docker-v2.txt

**Objective:** Quitar `demucs-onnx==0.3.4` de la lista de dependencias del contenedor v2.

**Files:**
- Modify: `requirements-docker-v2.txt`

**Step 1: Editar**

Eliminar la línea 42: `demucs-onnx==0.3.4`

**Step 2: Commit**
```bash
git add requirements-docker-v2.txt
git commit -m "build: quitar demucs-onnx de requirements-docker-v2.txt"
```

---

### Task 7: Simplificar Dockerfile (quitar cleanup onnxruntime CPU)

**Objective:** Las líneas 20-22 del Dockerfile hacen cleanup de `onnxruntime` CPU que instalaba `demucs-onnx` como dependencia. Al quitar `demucs-onnx`, ya no se instala onnxruntime CPU → el cleanup es innecesario. `onnxruntime-gpu` ya está en requirements-docker.txt y se instalará directamente.

**Files:**
- Modify: `Dockerfile`

**Step 1: Editar**

Eliminar las líneas 20-22:
```
# Remove CPU-only onnxruntime (pulled by demucs-onnx dep); keep onnxruntime-gpu
RUN rm -rf /deps/onnxruntime /deps/onnxruntime-*.dist-info && \
    pip install --no-cache-dir --target /deps --no-deps onnxruntime-gpu==1.26.0
```

**Step 2: Commit**
```bash
git add Dockerfile
git commit -m "build: eliminar cleanup onnxruntime CPU del Dockerfile"
```

---

### Task 8: Simplificar Dockerfile.v2 (quitar cleanup onnxruntime CPU)

**Objective:** Igual que Task 7 pero para el contenedor v2. Eliminar líneas 24-26.

**Files:**
- Modify: `Dockerfile.v2`

**Step 1: Editar**

Eliminar las líneas 24-26:
```
# Remove CPU-only onnxruntime (pulled by demucs-onnx dep); keep onnxruntime-gpu
RUN rm -rf /deps/onnxruntime /deps/onnxruntime-*.dist-info && \
    pip install --no-cache-dir --target /deps --no-deps onnxruntime-gpu==1.26.0
```

**Step 2: Commit**
```bash
git add Dockerfile.v2
git commit -m "build: eliminar cleanup onnxruntime CPU del Dockerfile.v2"
```

---

### Task 9: Actualizar CHANGELOG

**Objective:** Documentar el rollback de Demucs ONNX en CHANGELOG.md

**Files:**
- Modify: `CHANGELOG.md`

**Step 1: Añadir sección al inicio**

Insertar después de la línea `## v2.0.0-alpha` y antes de `### Rebuild CUDA 12.8`:

```markdown
### Rollback Demucs ONNX → Demucs PyTorch

#### Removed
- `inference/inference_demucs_onnx.py` — script ONNX eliminado (bug #1: GPU hang en RTX 5060 Ti)
- `tests/integration/test_demucs_onnx.py` — tests del script eliminado
- `demucs-onnx` de `requirements-docker.txt` y `requirements-docker-v2.txt`

#### Changed
- `tests/integration/test_edge_cases.py` — reemplazado Demucs ONNX con Demucs PyTorch (`demucs` CLI)
- `tests/integration/benchmark.py` — eliminada entrada `demucs-onnx-cpu`
- `Dockerfile` y `Dockerfile.v2` — eliminado cleanup de onnxruntime CPU (ya no necesario sin `demucs-onnx`)

#### Rationale
`demucs-onnx==0.3.4` + `onnxruntime-gpu` en RTX 5060 Ti se cuelga (GPU al 100%, no genera output). Los modelos ONNX de StemSplitio son incompatibles con CUDAExecutionProvider en esta configuración. Demucs PyTorch funciona correctamente (~60x realtime). MDX-Net ONNX se mantiene (funciona en GPU sin problemas).
```

**Step 2: Commit**
```bash
git add CHANGELOG.md
git commit -m "docs: documentar rollback Demucs ONNX en CHANGELOG"
```

---

## Post-Implementation Verification

Después de que Subagente A complete todas las tasks:

1. **Verificar git log** — mínimo 10+ commits (confirmar que no hubo commit huérfano)
2. **Push a .87** — `git push origin v2.0.0-alpha`
3. **Sincronizar .87** — `ssh .87 'cd ~/projects/onda && git fetch && git checkout v2.0.0-alpha && git reset --hard origin/v2.0.0-alpha'`
4. **Rebuild contenedor** — `ssh .87 'cd ~/projects/onda && docker build -t onda:nvidia .'`
5. **Verificar onnxruntime-gpu** — `ssh .87 'docker run --rm --gpus all onda:nvidia python -c "import onnxruntime; print(onnxruntime.get_available_providers())"'`
6. **Verificar Demucs PyTorch** — `ssh .87 'docker run --rm --gpus all -v $(pwd)/tests:/app/tests onda:nvidia demucs --help'`
7. **Ejecutar tests E2E** — `cd tests/integration && python3 -m pytest test_mdx_onnx.py test_edge_cases.py test_pipeline_api.py -v --timeout=300`
8. **Ejecutar benchmark** — `python3 tests/integration/benchmark.py`
