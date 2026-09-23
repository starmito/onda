# Demucs ONNX — Guía de Setup y Troubleshooting

> **Última actualización:** 2026-09-23 — Versiones sincronizadas con Onda v3.5.6
>
> ⚠️ Esta guía cubre el setup de Demucs ONNX. Onda v3.5.6 usa `onnxruntime-gpu` 1.30.0.

## Dependencias

```txt
# requirements-docker.txt (o instalar vía pip)
demucs-onnx==0.3.4        # Wrapper StemSplitio (0.1 MB, sin PyTorch)
onnxruntime-gpu==1.30.0   # NVIDIA CUDA (recomendado)
# OR
onnxruntime==1.30.0       # CPU-only (fallback universal)
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
    onnxruntime-gpu==1.30.0 \
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

### AMD (ROCm) — retirado

> **La ruta AMD/ROCm fue retirada del proyecto en v3.5.0.** A medio plazo no se va a
> trabajar en ella y sus dependencias ya no se mantienen. Si en el futuro se
> recupera, el punto de partida está en la historia de git (anterior a
> `feat/v3.5.0`).

### CPU (fallback universal)

Siempre disponible. Sin dependencias GPU. ~1.3-1.6x realtime.

```bash
pip install onnxruntime  # sin sufijo -gpu
```

## Bind Mounts

El contenedor usa una única raíz de datos (`ONDA_DATA_DIR`, por defecto `/app/data`):

```yaml
# docker-compose.yml
volumes:
  - ./data:/app/data        # input/, output/, config/, models/, .cache/...
  - ./models:/app/models    # Modelos ONNX (opcional, lectura)
```

Dentro del contenedor los datos están en `${ONDA_DATA_DIR}/input/`, `${ONDA_DATA_DIR}/output/`, etc. Los mounts bare `./input:/input` y `./output:/output` son obsoletos.

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

## Benchmarks (RTX 5060 Ti, 16 GB)

| Método | Tiempo (30s audio) | Ratio | VRAM |
|---|---|---|---|
| Demucs ONNX (CPU) | 19.3s | 1.6x | ~270 MB |
| Demucs ONNX (GPU) | *pendiente — viable con CUDA 12.8 + onnxruntime-gpu 1.30* | | |
| Demucs PyTorch (GPU) | ~4s | ~7.5x | ~3 GB |

## Roadmap

- [x] CPU inferencia funcional (1.6x realtime)
- [x] Bind mounts estables
- [x] Documentación de setup y troubleshooting
- [x] Previsión AMD documentada (onnxruntime-rocm) — **retirada en v3.5.0**
- [ ] GPU inferencia (NVIDIA) — viable con CUDA 12.8, pendiente de rebuild del contenedor
- [ ] Benchmark GPU vs CPU
- [ ] Integración en pipeline Go (`onda pipeline --preset master --onnx`)
