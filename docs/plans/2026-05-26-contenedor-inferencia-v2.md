# Contenedor de Inferencia v2 — Plan de Implementación

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Crear contenedor `onda-v2` en .87 con CUDA 13.2, PyTorch 2.12.0, Demucs 4.0.1 y dependencias fijadas.

**Architecture:** Multi-stage Docker (python:3.12-slim) con wheel unificado de PyTorch. Mismos bind mounts que `onda`.

**Tech Stack:** CUDA 13.2 (driver 595.71.05) + PyTorch 2.12.0 + Demucs 4.0.1 + Python 3.12

---

### Task 1: Crear requirements-docker-v2.txt con versiones fijadas

**Objective:** Crear archivo de dependencias Python con versiones exactas.

**Files:**
- Create: `/home/starmito/projects/onda/requirements-docker-v2.txt`

**Contenido exacto:**
```
# Onda v2 — Dependencias fijadas para inferencia (CUDA 13.2 + PyTorch 2.12.0)

# -- Core científico --
numpy==2.4.6
scipy==1.17.1
soundfile==0.13.1
librosa==0.11.0

# -- PyTorch ecosystem --
einops==0.8.2
PyYAML==6.0.3
psutil==7.2.2
pydub==0.25.1
resampy==0.4.3

# -- Demucs (instalado aparte con --no-deps) --
# demucs==4.0.1 (ver Task 2)
torchcodec==0.13.0
diffq==0.2.4
omegaconf==2.3.0
pytorch_lightning==2.6.4
ml_collections==1.1.0

# -- ONNX --
onnx==1.21.0
onnxruntime-gpu==1.26.0
onnx2pytorch==0.5.3

# -- Modelos específicos --
beartype==0.22.9
rotary_embedding_torch==0.8.9
segmentation_models_pytorch==0.5.0
transformers==5.9.0
spafe==0.3.3
audiomentations==0.43.1
torch_audiomentations==0.12.0
asteroid==0.7.0
julius==0.2.7
samplerate==0.2.4
dora-search
```

**Verificación:** `cat requirements-docker-v2.txt | grep == | wc -l` debe mostrar > 25 dependencias fijadas.

---

### Task 2: Crear Dockerfile.v2

**Objective:** Crear Dockerfile multi-stage optimizado con CUDA 13.2, PyTorch 2.12.0 y Demucs 4.0.1.

**Files:**
- Create: `/home/starmito/projects/onda/Dockerfile.v2`

**Contenido exacto (adaptado del Dockerfile actual):**
```dockerfile
# Onda v2 — Inferencia (CUDA 13.2, PyTorch 2.12.0)
# Multi-stage: builder compila deps, runtime slim

FROM python:3.12-slim AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# PyTorch 2.12.0 — wheel unificado (incluye CUDA 13.0 runtime, sin sufijo cuXXX)
RUN pip install --no-cache-dir --target /deps \
    torch==2.12.0 \
    torchaudio==2.11.0

# Demucs 4.0.1 — sin dependencias (torchaudio se instala aparte)
RUN pip install --no-cache-dir --target /deps \
    demucs==4.0.1 --no-deps

# Dependencias restantes fijadas
COPY requirements-docker-v2.txt /tmp/
RUN SKLEARN_ALLOW_DEPRECATED_SKLEARN_PACKAGE_INSTALL=True \
    pip install --no-cache-dir --target /deps -r /tmp/requirements-docker-v2.txt

# ── Runtime stage ────────────────────────────────────
FROM python:3.12-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    libsndfile1 \
    rubberband-cli \
    ffmpeg \
    libtk8.6 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /deps /usr/local/lib/python3.12/site-packages/

# Demucs CLI entry point (pip --target omite console scripts)
RUN printf '#!/bin/bash\ncd /tmp\nexec env PYTHONPATH=/usr/local/lib/python3.12/site-packages python -m demucs "$@"\n' > /usr/local/bin/demucs && \
    chmod +x /usr/local/bin/demucs

WORKDIR /app
ENTRYPOINT ["python"]
```

**Verificación:** `docker build -f Dockerfile.v2 -t onda-v2 .` debe completar sin errores.

---

### Task 3: Build del contenedor en .87

**Objective:** Construir la imagen `onda-v2` en el servidor .87.

**Steps:**
1. Sincronizar archivos a .87:
   ```bash
   rsync -av Dockerfile.v2 requirements-docker-v2.txt starmito@192.168.1.87:/home/starmito/projects/onda/
   ```
2. Construir en .87:
   ```bash
   ssh starmito@192.168.1.87 "cd /home/starmito/projects/onda && docker build -f Dockerfile.v2 -t onda-v2 ."
   ```
   Expected: BUILD SUCCESS

---

### Task 4: Crear y arrancar contenedor onda-v2

**Objective:** Crear contenedor `onda-v2` con bind mounts y GPU.

**Steps:**
1. Crear contenedor (sin arrancar):
   ```bash
   ssh starmito@192.168.1.87 "docker create --name onda-v2 --gpus all \
     -v /home/starmito/projects/onda/input:/input \
     -v /home/starmito/projects/onda/output:/output \
     onda-v2"
   ```
2. Arrancar:
   ```bash
   ssh starmito@192.168.1.87 "docker start onda-v2"
   ```
3. Verificar GPU:
   ```bash
   ssh starmito@192.168.1.87 "docker exec onda-v2 nvidia-smi"
   ```
4. Verificar PyTorch + CUDA:
   ```bash
   ssh starmito@192.168.1.87 "docker exec onda-v2 python -c 'import torch; print(f\"PyTorch {torch.__version__}, CUDA {torch.version.cuda}, GPU {torch.cuda.get_device_name(0)}\")'"
   ```
   Expected: PyTorch 2.12.0, CUDA 13.0, NVIDIA RTX 5060 Ti

---

### Task 5: Prueba de separación con audio real

**Objective:** Probar que Demucs 4.0.1 separa audio correctamente.

**Steps:**
1. Probar con audio de 30s (rápido):
   ```bash
   ssh starmito@192.168.1.87 "docker exec onda-v2 python -m demucs --two-stems=vocals -o /output/test-v2 /input/test_30s.flac"
   ```
   Expected: proceso completa sin errores, archivos en /output/test-v2/htdemucs/test_30s/

2. Verificar archivos generados:
   ```bash
   ssh starmito@192.168.1.87 "ls -la /home/starmito/projects/onda/output/test-v2/htdemucs/test_30s/"
   ```
   Expected: vocals.wav + no_vocals.wav con tamaño > 0

---

### Task 6: Limpiar tras verificación

**Objective:** Parar y eliminar contenedor de prueba tras verificación exitosa.

**Steps:**
1. Parar:
   ```bash
   ssh starmito@192.168.1.87 "docker stop onda-v2"
   ```
2. Eliminar:
   ```bash
   ssh starmito@192.168.1.87 "docker rm onda-v2"
   ```
   (La imagen `onda-v2` se conserva para uso futuro)

### PLAN DE CONTINGENCIA

Si Task 5 falla:
- Revisar logs: `docker logs onda-v2`
- Si es error de dependencia → ajustar versiones en requirements-docker-v2.txt, rebuild
- Si es error de CUDA → verificar que el driver soporta CUDA 13.0 (ya confirmado)
- Si es error de Demucs → probar con flag `--two-stems=vocals` o sin flags
