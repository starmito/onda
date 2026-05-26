# Demucs ONNX (StemSplitio) — Plan de Implementación

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Migrar Demucs de PyTorch a ONNX usando el paquete `demucs-onnx` de StemSplitio. Misma calidad (SDR 9.19 dB vocals), 1.31× más rápido en CPU, 40× más ligero como dependencia. Sin PyTorch en inferencia.

**Architecture:** Paquete `demucs-onnx` v0.3.4 como dependencia Python. Wrapper ligero `inference/inference_demucs_onnx.py` con CLI + función `separate()` importable. Modelos ONNX (~1.26 GB total) montados como volumen. El pipeline Go invoca el wrapper vía `docker exec`.

**Tech Stack:** demucs-onnx 0.3.4, onnxruntime-gpu 1.26.0, numpy, soundfile

**Estado actual:**
- `onnxruntime-gpu==1.26.0` YA está en requirements-docker-v2.txt
- Contenedor `onda` corriendo con CUDA y bind mounts
- MDX-Net ONNX ya integrado (patrón de referencia)
- `inference/inference_mdx.py` como referencia de estructura

**Lo que NO incluye esta fase:**
- Exportar modelos nosotros (ya lo hizo StemSplitio, verificado SDR idéntico)
- Cambiar el pipeline Go (siguiente fase)

---

### Task 1: Añadir demucs-onnx a requirements

**Objective:** Añadir `demucs-onnx` como dependencia en el contenedor de inferencia.

**Files:**
- Modify: `requirements-docker-v2.txt` — añadir `demucs-onnx==0.3.4`

**Steps:**

```bash
echo "demucs-onnx==0.3.4" >> requirements-docker-v2.txt
```

**Verificación:**
```bash
grep "demucs-onnx" requirements-docker-v2.txt
# Expected: demucs-onnx==0.3.4
```

---

### Task 2: Descargar modelos ONNX

**Objective:** Descargar los 4 modelos especialistas ONNX de StemSplitio (~316 MB c/u, ~1.26 GB total).

**Files:**
- Create: `models/Demucs_ONNX/` (directorio con 4 archivos .onnx)

**Modelos:**
| Archivo | Stem | Tamaño |
|---|---|---|
| `htdemucs_ft_vocals.onnx` | Vocals | 316 MB |
| `htdemucs_ft_drums.onnx` | Drums | 316 MB |
| `htdemucs_ft_bass.onnx` | Bass | 316 MB |
| `htdemucs_ft_other.onnx` | Other | 316 MB |

**Steps:**
```bash
mkdir -p models/Demucs_ONNX
cd models/Demucs_ONNX

# Usar huggingface_hub CLI
pip install huggingface_hub
huggingface-cli download StemSplitio/htdemucs-ft-onnx \
  htdemucs_ft_vocals.onnx \
  htdemucs_ft_drums.onnx \
  htdemucs_ft_bass.onnx \
  htdemucs_ft_other.onnx \
  --local-dir . \
  --local-dir-use-symlinks False
```

**NOTA:** En localhost no tenemos huggingface-cli. Usar `demucs_onnx.separate()` que descarga automáticamente los modelos en el primer uso, o descargarlos directamente en .87.

**Alternativa (directa en .87):**
```bash
ssh .87 "mkdir -p /home/starmito/projects/onda/models/Demucs_ONNX && cd /home/starmito/projects/onda/models/Demucs_ONNX && for f in htdemucs_ft_vocals.onnx htdemucs_ft_drums.onnx htdemucs_ft_bass.onnx htdemucs_ft_other.onnx; do wget -q --show-progress https://huggingface.co/StemSplitio/htdemucs-ft-onnx/resolve/main/\$f; done"
```

**Verificación:**
```bash
ls -lh models/Demucs_ONNX/*.onnx | wc -l  # debe ser 4
du -sh models/Demucs_ONNX/  # ~1.3 GB
```

---

### Task 3: Añadir modelos a .gitignore

**Objective:** Excluir los modelos ONNX del control de versiones.

**Files:**
- Modify: `.gitignore`

**Steps:**
```bash
echo "models/Demucs_ONNX/" >> .gitignore
```

**Verificación:**
```bash
git status --porcelain models/  # no debe mostrar archivos .onnx
```

---

### Task 4: Crear inference_demucs_onnx.py

**Objective:** Wrapper standalone para Demucs ONNX. CLI para el pipeline Go + función `separate()` importable.

**Files:**
- Create: `inference/inference_demucs_onnx.py`

**Especificación del script:**

```python
#!/usr/bin/env python3
"""
Demucs ONNX inference via StemSplitio's demucs-onnx package.
Headless — no GUI dependencies. GPU auto-detected via onnxruntime.

Usage:
  python3 inference_demucs_onnx.py <input_audio> [output_dir] [--stems vocals,drums,bass,other]
  python3 inference_demucs_onnx.py --list-models
  python3 -c "from inference_demucs_onnx import separate; separate(...)"
"""

import sys
import os
import argparse
import time
import numpy as np
import demucs_onnx
import soundfile as sf

MODELS_DIR = os.path.join(os.path.dirname(__file__), '..', 'models', 'Demucs_ONNX')
DEFAULT_STEMS = ['vocals', 'drums', 'bass', 'other']


def separate(
    input_path,
    output_dir="output",
    stems=None,
    model="htdemucs_ft",
    precision="fp32",
    cache_dir=None,
):
    """
    Run Demucs ONNX separation.

    Args:
        input_path: Path to input audio file
        output_dir: Directory for output stems
        stems: List of stems to extract (default: all 4)
        model: Model name (htdemucs_ft, htdemucs, htdemucs_6s)
        precision: fp32 or fp16
        cache_dir: Model cache directory (default: MODELS_DIR)

    Returns:
        dict with stem names -> output file paths
    """
    if stems is None:
        stems = DEFAULT_STEMS
    if cache_dir is None:
        cache_dir = MODELS_DIR

    os.makedirs(output_dir, exist_ok=True)

    start = time.time()
    result = demucs_onnx.separate(
        input=input_path,
        output_dir=output_dir,
        model=model,
        stems=stems,
        providers="auto",  # auto-detect CUDA/CPU
        precision=precision,
        cache_dir=cache_dir,
        verbose=False,
        progress=False,
    )
    elapsed = time.time() - start

    # Escribir archivos de salida
    output_files = {}
    for stem, audio in result.items():
        out_path = os.path.join(output_dir, f"{stem}.wav")
        sf.write(out_path, audio.T, 44100)
        output_files[stem] = out_path

    # Log
    duration = len(list(result.values())[0]) / 44100 if result else 0
    print(f"Demucs ONNX: {duration:.1f}s audio procesado en {elapsed:.1f}s "
          f"({duration/elapsed:.1f}x realtime)")
    print(f"Stems generados: {', '.join(output_files.keys())}")

    return output_files


def list_models():
    """List available Demucs ONNX models."""
    models = demucs_onnx.list_models()
    print("Modelos Demucs ONNX disponibles:")
    for m in models:
        info = ""
        if m == "htdemucs_ft":
            info = " — 4 stems (vocals, drums, bass, other)"
        elif m == "htdemucs":
            info = " — 4 stems (single-file)"
        elif m == "htdemucs_6s":
            info = " — 6 stems (+ guitar, piano)"
        elif "_" in m:
            info = f" — especialista ({m.split('_')[-1]})"
        print(f"  {m}{info}")
    return models


def main():
    parser = argparse.ArgumentParser(
        description="Demucs ONNX — separación de stems de audio"
    )
    parser.add_argument("input", nargs="?", help="Archivo de audio de entrada")
    parser.add_argument("output_dir", nargs="?", default="output",
                        help="Directorio de salida")
    parser.add_argument("--stems", default="vocals,drums,bass,other",
                        help="Stems a extraer (default: vocals,drums,bass,other)")
    parser.add_argument("--model", default="htdemucs_ft",
                        help="Modelo ONNX (default: htdemucs_ft)")
    parser.add_argument("--precision", default="fp32", choices=["fp32", "fp16"],
                        help="Precisión (default: fp32)")
    parser.add_argument("--list-models", action="store_true",
                        help="Listar modelos disponibles")
    parser.add_argument("--runtime", action="store_true",
                        help="Mostrar info del runtime")

    args = parser.parse_args()

    if args.list_models:
        list_models()
        return

    if args.runtime:
        info = demucs_onnx.describe_runtime()
        print(f"onnxruntime: {info['onnxruntime']}")
        print(f"Providers: {info['available_providers']}")
        return

    if not args.input:
        parser.error("Se requiere INPUT (archivo de audio)")

    stems = [s.strip() for s in args.stems.split(",")]
    separate(
        input_path=args.input,
        output_dir=args.output_dir,
        stems=stems,
        model=args.model,
        precision=args.precision,
    )


if __name__ == "__main__":
    main()
```

**Requisitos funcionales:**
1. CLI con argparse: `--input`, `--output`, `--stems`, `--model`, `--precision`
2. `--list-models` para listar modelos disponibles
3. `--runtime` para información de onnxruntime
4. Función `separate()` importable desde otros scripts
5. Auto-detección de GPU via `providers="auto"`
6. Logging de tiempo de procesamiento y ratio realtime

**Verificación (en localhost, CPU):**
```bash
python3 inference/inference_demucs_onnx.py --list-models
# Expected: lista de 7 modelos

python3 inference/inference_demucs_onnx.py --runtime
# Expected: onnxruntime 1.26.0, providers ['CPUExecutionProvider']
```

**Verificación (en .87, GPU):**
```bash
ssh .87 "docker exec onda python /app/inference_demucs_onnx.py --runtime"
# Expected: providers incluye CUDAExecutionProvider
```

---

### Task 5: Integrar en contenedor Docker

**Objective:** Rebuild del contenedor con `demucs-onnx`, bind mount de modelos, y verificar.

**Files:**
- Modify: `docker-compose.yml` — añadir bind mount `models/Demucs_ONNX`
- Already OK: `Dockerfile.v2` — `COPY inference/ /app/` cubre el nuevo script

**Steps:**

1. Sincronizar archivos a .87:
```bash
rsync -av inference/inference_demucs_onnx.py starmito@192.168.1.87:/home/starmito/projects/onda/inference/
rsync -av requirements-docker-v2.txt starmito@192.168.1.87:/home/starmito/projects/onda/
```

2. Reconstruir imagen (en .87):
```bash
ssh .87 "cd /home/starmito/projects/onda && docker build -f Dockerfile.v2 -t onda-v2 ."
```

3. Verificar instalación:
```bash
ssh .87 "docker run --rm --gpus all \
  -v /home/starmito/projects/onda/models:/app/models \
  onda-v2 python -c 'import demucs_onnx; print(demucs_onnx.__version__)'"
# Expected: 0.3.4
```

4. Añadir bind mount al contenedor `onda` existente (o recrearlo):
```bash
# Si el contenedor ya tiene el bind mount de models/, verificar:
ssh .87 "docker inspect onda --format '{{json .Mounts}}' | python3 -m json.tool | grep models"
```

---

### Task 6: Benchmark Demucs ONNX vs PyTorch

**Objective:** Comparar velocidad y calidad entre Demucs PyTorch (actual) y Demucs ONNX (nuevo).

**Métrica principal:** Tiempo de procesamiento para mismo audio.
**Métrica secundaria:** Calidad (comparar checksums de salida).

**Steps:**

1. Ejecutar Demucs PyTorch (referencia):
```bash
time ssh .87 "docker exec onda python -m demucs --two-stems=vocals -n htdemucs_ft \
  -o /output/bench-pytorch /input/bench_60s.flac"
```

2. Ejecutar Demucs ONNX:
```bash
time ssh .87 "docker exec onda python /app/inference_demucs_onnx.py \
  /input/bench_60s.flac \
  /output/bench-onnx/ \
  --stems vocals"
```

3. Comparar tiempos:
```bash
# Medir ambos con time (wall clock)
```

4. Comparar calidad (md5):
```bash
ssh .87 "md5sum /home/starmito/projects/onda/output/bench-pytorch/*/*/vocals.wav"
ssh .87 "md5sum /home/starmito/projects/onda/output/bench-onnx/vocals.wav"
```

**Plantilla de resultados:**
| Método | Tiempo | Ratio realtime | VRAM |
|---|---|---|---|
| Demucs PyTorch | ?s | ?x | ? GB |
| Demucs ONNX | ?s | ?x | ? GB |

**Expected:** ONNX debe ser comparable o más rápido, con menos VRAM.

---

### Task 7: Documentar en CHANGELOG

**Objective:** Registrar la integración en el changelog del proyecto.

**Files:**
- Modify: `CHANGELOG.md`

**Contenido a añadir (bajo `## v2.0.0-alpha`):**
```markdown
### Demucs ONNX — Migración PyTorch → ONNX

#### Added
- `inference/inference_demucs_onnx.py` — wrapper CLI para Demucs ONNX vía `demucs-onnx`
- Dependencia `demucs-onnx==0.3.4` (0.1 MB, sin PyTorch)
- Modelos ONNX: 4 especialistas (vocals, drums, bass, other) — ~1.26 GB total
- Fuente: StemSplitio/htdemucs-ft-onnx (calidad idéntica a PyTorch, SDR 9.19 dB)

#### Changed
- Inferencia Demucs ahora usa ONNX en lugar de PyTorch (instalación 40× más ligera)
- Bind mount `models/Demucs_ONNX/` en contenedor
- `.gitignore`: excluye `models/Demucs_ONNX/`
```

---

### Task 8: Commit

**Objective:** Commit de todos los cambios en rama `v2.0.0-alpha`.

**Steps:**
```bash
cd ~/projects/onda
git add requirements-docker-v2.txt inference/inference_demucs_onnx.py .gitignore CHANGELOG.md docs/plans/2026-05-26-demucs-onnx.md
git commit -m "feat(inference): integrar Demucs ONNX vía demucs-onnx (StemSplitio)"
```

**GIT SAFETY CHECK:**
```bash
git status --porcelain  # solo archivos esperados
git log --oneline -3    # ≥ 3 commits ✓
git branch --show-current  # v2.0.0-alpha ✓
```

---

## Riesgos

- **Modelos grandes (1.26 GB):** La descarga puede fallar. Usar `wget -c` para reanudar.
- **onnxruntime-gpu + CUDA 12.8:** Puede no detectar GPU si falta `libcublasLt.so.12`. Solución: instalar `nvidia-cublas-cu12` o usar CPU fallback.
- **Tiempo de rebuild:** Build puede tardar >600s. Usar `background=true` con `notify_on_complete=true`.
- **El contenedor `onda` existente no tiene `demucs-onnx`:** Necesita rebuild o recrear con nueva imagen.
