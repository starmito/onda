# Demucs ONNX — Guía de Setup y Troubleshooting

> **Última actualización:** 2026-05-26 — Bind mounts arreglados, GPU pendiente de rebuild

## Dependencias

```txt
# requirements-docker-v2.txt (o instalar vía pip)
demucs-onnx==0.3.4        # Wrapper StemSplitio (0.1 MB, sin PyTorch)
onnxruntime-gpu==1.26.0   # NVIDIA CUDA (recomendado)
# OR
onnxruntime-rocm==1.26.0  # AMD ROCM (alternativa)
# OR
onnxruntime==1.26.0       # CPU-only (fallback universal)
```

**Nota:** `demucs-onnx` NO requiere PyTorch. Solo numpy + onnxruntime + soundfile + soxr + tqdm + huggingface-hub.

## Modelos ONNX

| Modelo | Stem | Tamaño | Fuente |
|---|---|---|---|
| `htdemucs_ft_vocals.onnx` | Vocals | 302 MB | StemSplitio/htdemucs-ft-onnx |
| `htdemucs_ft_drums.onnx` | Drums | 302 MB | StemSplitio/htdemucs-ft-onnx |
| `htdemucs_ft_bass.onnx` | Bass | 302 MB | StemSplitio/htdemucs-ft-onnx |
| `htdemucs_ft_other.onnx` | Other | 302 MB | StemSplitio/htdemucs-ft-onnx |

**Total:** ~1.2 GB

### Descarga

```bash
cd models/Demucs_ONNX
for f in htdemucs_ft_vocals.onnx htdemucs_ft_drums.onnx htdemucs_ft_bass.onnx htdemucs_ft_other.onnx; do
    wget -q --show-progress "https://huggingface.co/StemSplitio/htdemucs-ft-onnx/resolve/main/$f"
done
```

Los modelos NO se comitean a git (`.gitignore`: `models/Demucs_ONNX/`). Se montan como volumen en el contenedor.

## GPU Setup

### NVIDIA (CUDA)

El contenedor necesita **3 cosas** para que ONNX detecte GPU:

1. `onnxruntime-gpu` instalado vía pip
2. Bibliotecas CUDA accesibles (`libcublas.so.12`, `libcudnn.so.9`)
3. Runtime `nvidia` de Docker (`--gpus all`)

**Problema conocido:** El contenedor `onda:nvidia` actual tiene PyTorch con CUDA (funciona porque PyTorch empaqueta sus propias libs CUDA), pero NO tiene las bibliotecas CUDA del sistema. `onnxruntime-gpu` necesita las bibliotecas del sistema.

**Solución (Dockerfile):**

```dockerfile
# Además de onnxruntime-gpu, instalar las bibliotecas CUDA vía pip wheels
RUN pip install --no-cache-dir \
    onnxruntime-gpu==1.26.0 \
    nvidia-cublas-cu12 \
    nvidia-cudnn-cu12
```

**Verificar GPU:**
```bash
docker exec onda python -c "
import onnxruntime as ort
print('CUDA available:', 'CUDAExecutionProvider' in ort.get_available_providers())
print('Providers:', ort.get_available_providers())
"
# Expected: CUDA available: True, Providers: ['CUDAExecutionProvider', 'CPUExecutionProvider']
```

### AMD (ROCM)

Para tarjetas AMD (RX 7000+, Instinct), usar `onnxruntime-rocm` en lugar de `onnxruntime-gpu`:

```dockerfile
# AMD ROCM
RUN pip install --no-cache-dir onnxruntime-rocm==1.26.0
```

El código del wrapper (`inference_demucs_onnx.py`) ya es portable — usa `providers="auto"` que detecta automáticamente CUDA, ROCM, CoreML, DirectML, o CPU.

**Verificar ROCM:**
```bash
docker exec onda python -c "
import onnxruntime as ort
print('ROCM available:', 'ROCMExecutionProvider' in ort.get_available_providers())
print('Providers:', ort.get_available_providers())
"
```

### CPU (fallback universal)

Siempre disponible. Sin dependencias GPU. ~1.3-1.6x realtime.

```bash
pip install onnxruntime  # sin sufijo -gpu ni -rocm
```

## Bind Mounts

El contenedor necesita acceso a 3 directorios del host:

```yaml
# docker-compose.yml
volumes:
  - ./models:/app/models    # Modelos ONNX (lectura)
  - ./input:/input          # Audio de entrada
  - ./output:/output        # Stems generados
```

**⚠️ Si los mounts no funcionan** (directorios vacíos en el contenedor):
- Verificar inodos: `stat -c '%i' /host/path` vs `docker exec onda stat -c '%i' /container/path` — deben coincidir
- Solución: `docker compose down && docker compose up -d` (recrea el contenedor)

## Uso

### CLI

```bash
# Separar todos los stems
python3 inference_demucs_onnx.py cancion.flac output/

# Solo vocales
python3 inference_demucs_onnx.py cancion.flac output/ --stems vocals

# Precisión fp16 (mitad VRAM, misma calidad)
python3 inference_demucs_onnx.py cancion.flac output/ --precision fp16

# Info del runtime
python3 inference_demucs_onnx.py --runtime

# Listar modelos disponibles
python3 inference_demucs_onnx.py --list-models
```

### Como librería

```python
from inference_demucs_onnx import separate

files = separate(
    input_path="cancion.flac",
    output_dir="output/",
    stems=["vocals", "drums"],
    model="htdemucs_ft",
    precision="fp32",
)
# files = {"vocals": "output/vocals.wav", "drums": "output/drums.wav"}
```

## Troubleshooting

| Síntoma | Causa probable | Solución |
|---|---|---|
| `FileNotFoundError: input` | Bind mount roto | `docker compose down && docker compose up -d` |
| Solo `CPUExecutionProvider` | Falta `onnxruntime-gpu` o CUDA libs | Instalar `nvidia-cublas-cu12 nvidia-cudnn-cu12` |
| `ModuleNotFoundError: demucs.augment` | Demucs PyTorch corrupto | Usar ONNX, no PyTorch |
| Modelos no encontrados | cache_dir incorrecto | Verificar `models/Demucs_ONNX/` existe y tiene 4 .onnx |
| CUDA out of memory | Modelo muy grande | Usar `--precision fp16` (mitad VRAM) |
| Descarga lenta de modelos | Sin HF_TOKEN | `export HF_TOKEN=...` o descargar con wget |

### Docker Build — Problemas conocidos (26-may-2026)

**Contexto:** Construyendo contenedor `onda-next` con `python:3.14-slim` (Debian 13 Trixie) + CUDA 13.2.

**Problema #1 — NVIDIA repo + Debian Trixie (SHA1)**
- La keyring de NVIDIA usa SHA1, que Debian Trixie rechaza desde 2026-02-01
- Error: `OpenPGP signature verification failed: SHA1 is not considered secure`
- **Solución probada:** `--allow-unauthenticated` → no funcionó (problemas de quotes en Dockerfile)
- **Solución final:** Usar el repo para Debian 13 (`debian13`) en lugar de Debian 12 (`debian12`)

**Problema #2 — `libcuda.so` ausente en python:slim**
- `onnxruntime-gpu` necesita `libcuda.so` (driver NVIDIA del host)
- `python:3.12-slim` y `python:3.14-slim` no la incluyen
- PyTorch sí funciona porque empaqueta sus propias bibliotecas CUDA
- **Solución:** Instalar `cuda-compat-13-2` desde el repo NVIDIA → proporciona `libcuda.so`

**Problema #3 — Bind mounts rotos tras recrear contenedor**
- Al recrear con `docker compose down && docker compose up -d`, los mounts quedan vacíos
- **Solución:** `docker stop && docker rm && docker compose up -d`

### Dockerfile — Versión actual (`Dockerfile.next`)

```dockerfile
FROM python:3.14-slim

# Sistema
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential libsndfile1 rubberband-cli ffmpeg wget gnupg \
    && rm -rf /var/lib/apt/lists/*

# NVIDIA CUDA repo (Debian 13 Trixie)
RUN wget -q https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb \
    && dpkg -i cuda-keyring_1.1-1_all.deb \
    && rm cuda-keyring_1.1-1_all.deb \
    && apt-get update \
    && apt-get install -y --no-install-recommends cuda-compat-13-2 \
    && rm -rf /var/lib/apt/lists/*

# PyTorch CUDA 13.2
RUN pip install --no-cache-dir torch torchvision --index-url https://download.pytorch.org/whl/cu132 \
    && pip install --no-cache-dir torchaudio

# Dependencias del proyecto
COPY requirements-docker.txt /tmp/
RUN SKLEARN_ALLOW_DEPRECATED_SKLEARN_PACKAGE_INSTALL=True \
    pip install --no-cache-dir -r /tmp/requirements-docker.txt

# ONNX GPU + Demucs ONNX
RUN pip install --no-cache-dir onnxruntime-gpu==1.26.0 demucs-onnx

RUN ldconfig
WORKDIR /app
COPY . .
ENTRYPOINT ["tail", "-f", "/dev/null"]
```

## Benchmarks (RTX 5060 Ti, 16 GB)

| Método | Tiempo (30s audio) | Ratio | VRAM |
|---|---|---|---|
| Demucs ONNX (CPU) | 19.3s | 1.6x | ~270 MB |
| Demucs ONNX (GPU) | *pendiente* | *pendiente* | *pendiente* |
| Demucs PyTorch (GPU) | ~4s | ~7.5x | ~3 GB |

## Roadmap

- [x] CPU inferencia funcional (1.6x realtime)
- [x] Bind mounts estables
- [x] Documentación de setup y troubleshooting
- [x] Dockerfile para python:3.14-slim + CUDA 13.2 (Dockerfile.next)
- [x] Previsión AMD documentada (onnxruntime-rocm)
- [ ] GPU inferencia (NVIDIA) — build en curso (`onda-next`, 4º intento)
- [ ] Benchmark GPU vs CPU
- [ ] Integración en pipeline Go (`onda pipeline --preset master --onnx`)
