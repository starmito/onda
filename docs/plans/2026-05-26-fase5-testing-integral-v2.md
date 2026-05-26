# Fase 5 — Testing Integral (Post-Rollback)

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Ejecutar suite completa de tests E2E contra .87 con Demucs PyTorch + MDX-Net ONNX.

**Architecture:** MDX models están en `/mnt/almacen/onda/models/MDX_Net_Models/`. Hay que symlinkearlos a `~/projects/onda/models/` para que el contenedor los vea vía bind mount `./models:/app/models:ro`.

**Tech Stack:** Python 3.12, pytest, Demucs PyTorch CLI, onnxruntime-gpu

---

### Task 1: Symlink MDX models al proyecto

**Objective:** Los modelos MDX ONNX viven en `/mnt/almacen/onda/models/MDX_Net_Models/` pero el contenedor espera `/app/models/MDX_Net_ONNX/`. Crear symlinks.

**Files:**
- Create symlinks in: `~/projects/onda/models/` (en .87)

**Step 1: Crear directorio y symlinks**
```bash
ssh .87 'mkdir -p ~/projects/onda/models/MDX_Net_ONNX && \
  ln -sf /mnt/almacen/onda/models/MDX_Net_Models/Kim_Vocal_2.onnx ~/projects/onda/models/MDX_Net_ONNX/Kim_Vocal_2.onnx && \
  ln -sf /mnt/almacen/onda/models/MDX_Net_Models/Kim_Vocal_1.onnx ~/projects/onda/models/MDX_Net_ONNX/Kim_Vocal_1.onnx && \
  ln -sf /mnt/almacen/onda/models/MDX_Net_Models/UVR_MDXNET_Main.onnx ~/projects/onda/models/MDX_Net_ONNX/UVR_MDXNET_Main.onnx && \
  ls -la ~/projects/onda/models/MDX_Net_ONNX/'
```

**Step 2: Verificar desde el contenedor**
```bash
ssh .87 'docker exec onda ls -la /app/models/MDX_Net_ONNX/'
```

**Step 3: Commit**
```bash
git add -A && git commit -m "infra: symlink MDX ONNX models desde /mnt/almacen"
```

---

### Task 2: Fix test_mdx_onnx.py — añadir modelo ONNX

**Objective:** `run_mdx()` no pasa el path del modelo ONNX. inference_mdx.py espera: `<model.onnx> <input.wav> <output_dir>`.

**Files:**
- Modify: `tests/integration/test_mdx_onnx.py`

**Step 1: Añadir MODEL_PATH y pasarlo en run_mdx()**

Cambiar:
```python
OUTPUT = "/tmp/onda-test-mdx"
SCRIPT = "inference/inference_mdx.py"
FIXTURE_DIR = "/app/tests/integration/fixtures"

def run_mdx(input_file, output_dir=None):
    out = output_dir or f"{OUTPUT}"
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER,
        "python3", SCRIPT,
        f"{FIXTURE_DIR}/{input_file}",
        out,
    ]
```

Por:
```python
OUTPUT = "/tmp/onda-test-mdx"
SCRIPT = "inference/inference_mdx.py"
FIXTURE_DIR = "/app/tests/integration/fixtures"
MODEL_PATH = "/app/models/MDX_Net_ONNX/Kim_Vocal_2.onnx"

def run_mdx(input_file, output_dir=None):
    out = output_dir or f"{OUTPUT}"
    cmd = [
        "ssh", ".87", "docker", "exec", CONTAINER,
        "python3", SCRIPT,
        MODEL_PATH,
        f"{FIXTURE_DIR}/{input_file}",
        out,
    ]
```

También ajustar `test_mdx_sine` y `test_mdx_chirp` que usan output_dir fijo (debe ser único para evitar colisiones).

**Step 2: Commit**
```bash
git add tests/integration/test_mdx_onnx.py
git commit -m "test: fix MDX test — añadir MODEL_PATH a run_mdx()"
```

---

### Task 3: Nuevo test_demucs_pytorch.py

**Objective:** Test de separación normal con Demucs PyTorch (no solo edge cases).

**Files:**
- Create: `tests/integration/test_demucs_pytorch.py`

**Step 1: Crear archivo**

```python
"""End-to-end tests for Demucs PyTorch separation."""
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


def test_demucs_sine_vocals():
    """Separate vocals from 440 Hz sine wave."""
    out = "/tmp/onda-test-demucs-sine/"
    result = run_demucs("sine_440_5s.flac", out)
    assert result.returncode == 0, f"Demucs failed: {result.stderr}"
    # Verify output file exists
    check = subprocess.run(
        ["ssh", ".87", "docker", "exec", CONTAINER,
         "find", out, "-name", "vocals.wav"],
        capture_output=True, text=True, timeout=10
    )
    assert "vocals.wav" in check.stdout, f"Missing vocals.wav in output"


def test_demucs_chirp_vocals():
    """Separate vocals from chirp signal."""
    out = "/tmp/onda-test-demucs-chirp/"
    result = run_demucs("chirp_5s.flac", out)
    assert result.returncode == 0, f"Demucs chirp failed: {result.stderr}"


def test_demucs_output_structure():
    """Verify output has expected directory structure."""
    out = "/tmp/onda-test-demucs-struct/"
    result = run_demucs("sine_440_5s.flac", out)
    assert result.returncode == 0
    # Demucs output: <out>/htdemucs_ft/<track_name>/vocals.wav, no_vocals.wav
    check = subprocess.run(
        ["ssh", ".87", "docker", "exec", CONTAINER,
         "find", out, "-name", "*.wav", "-type", "f"],
        capture_output=True, text=True, timeout=10
    )
    wavs = [w for w in check.stdout.strip().split("\n") if w]
    assert len(wavs) >= 2, f"Expected >=2 wav files, got {len(wavs)}: {wavs}"
```

**Step 2: Commit**
```bash
git add tests/integration/test_demucs_pytorch.py
git commit -m "test: añadir tests E2E para Demucs PyTorch"
```

---

### Task 4: Push + sync .87

```bash
git push origin v2.0.0-alpha
ssh .87 'cd ~/projects/onda && git fetch origin && git checkout v2.0.0-alpha && git reset --hard origin/v2.0.0-alpha'
```

---

## Post-Implementation: Verification Gate

Subagente B ejecuta contra .87:

```bash
# 1. Verificar symlinks MDX
ssh .87 'docker exec onda ls /app/models/MDX_Net_ONNX/'

# 2. Generar fixtures
ssh .87 'docker exec onda python3 tests/integration/generate_test_audio.py'

# 3. Ejecutar suite completa
cd ~/projects/onda
python3 -m pytest tests/integration/test_mdx_onnx.py tests/integration/test_demucs_pytorch.py tests/integration/test_edge_cases.py tests/integration/test_pipeline_api.py -v --timeout=300 2>&1

# 4. Benchmark
python3 tests/integration/benchmark.py
```
