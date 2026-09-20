#!/bin/bash
set -euo pipefail

GPU=$(detect_gpu.sh)
export GPU
echo "🎯 GPU detected: $GPU"

# PYTHONPATH siempre incluye /app/lib_v5/ (necesario para inference_universal.py)
export PYTHONPATH="${PYTHONPATH:-}:/app/lib_v5"

# Para CPU: torch ya está en la imagen, no hacer nada extra
if [ "$GPU" != "cpu" ]; then
    CACHE_DIR="/opt/pytorch-backends/$GPU"
    export PYTHONPATH="${PYTHONPATH:-}:$CACHE_DIR"

    if [ ! -f "$CACHE_DIR/torch/__init__.py" ]; then
        echo "📦 Installing $GPU backend..."
        mkdir -p "$CACHE_DIR"
        case $GPU in
            cuda)
                pip install --target "$CACHE_DIR" torch==2.11.0 torchaudio==2.11.0 torchvision==0.26.0 onnxruntime-gpu==1.26.0
                ;;
        esac
        echo "✅ $GPU backend installed"
    fi

    # Robustness: verify onnxruntime imports from the cache, reinstall if missing/corrupt.
    if ! PYTHONPATH="$CACHE_DIR" python -c "import onnxruntime" >/dev/null 2>&1; then
        echo "⚠️  onnxruntime not importable in cache, reinstalling..."
        mkdir -p "$CACHE_DIR"
        case $GPU in
            cuda)
                pip install --target "$CACHE_DIR" onnxruntime-gpu==1.26.0
                ;;
        esac
        echo "✅ onnxruntime reinstalled"
    fi

    # onnxruntime-gpu needs CUDA libraries that are bundled inside torch's lib dir.
    if [ -d "$CACHE_DIR/torch/lib" ]; then
        export LD_LIBRARY_PATH="$CACHE_DIR/torch/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    fi
fi

ONDA_DATA_DIR="${ONDA_DATA_DIR:-/app/data}"

# Crear directorios de datos bajo la raiz configurada.
mkdir -p "${ONDA_DATA_DIR}/input" \
         "${ONDA_DATA_DIR}/output" \
         "${ONDA_DATA_DIR}/daw-data" \
         "${ONDA_DATA_DIR}/input_rubberband" \
         "${ONDA_DATA_DIR}/config" \
         "${ONDA_DATA_DIR}/logs" \
         "${ONDA_DATA_DIR}/models"

# Limpieza de subcarpetas temporales huérfanas de jobs abortados por reinicio duro.
for job_dir in "${ONDA_DATA_DIR}/output"/*; do
    if [ -d "$job_dir" ]; then
        for orphan in _vocal _demucs; do
            if [ -d "$job_dir/$orphan" ]; then
                rm -rf "$job_dir/$orphan"
                echo "🧹 Cleaned orphan temp dir: $job_dir/$orphan"
            fi
        done
    fi
done

# Use persistent caches under /app so torch/hf/numba state survives restarts.
export TORCH_HOME=/app/.cache/torch
export NUMBA_CACHE_DIR=/app/.cache/numba
export XDG_CACHE_HOME=/app/.cache/xdg
export HF_HOME=/app/.cache/hf

# Crear directorios de caché persistentes como appuser
mkdir -p /app/.cache/numba /app/.cache/torch /app/.cache/xdg /app/.cache/hf

echo "🚀 Starting Onda ${ONDAP_VERSION:-unknown} ($GPU mode)..."
exec /usr/local/bin/onda-backend serve --addr 0.0.0.0:3000
