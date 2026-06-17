#!/bin/bash
set -euo pipefail

# Si arrancamos como root hacemos el setup con privilegios y luego
# re-ejecutamos el entrypoint como appuser (gosu equivale a su-exec en Debian).
if [ "$(id -u)" = "0" ]; then
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
                rocm)
                    pip install --target "$CACHE_DIR" torch==2.11.0+rocm7.1 torchaudio==2.11.0+rocm7.1 torchvision==0.26.0+rocm7.1 onnxruntime --extra-index-url https://download.pytorch.org/whl/rocm7.1
                    ;;
            esac
            echo "✅ $GPU backend installed"
        fi
    fi

    # Crear directorios de montaje
    mkdir -p /input /output /input_rubberband /config

    export TORCH_HOME=/tmp/torch_cache
    export NUMBA_CACHE_DIR=/tmp/numba_cache
    export XDG_CACHE_HOME=/tmp/xdg_cache
    export HF_HOME=/tmp/hf_cache

    # Bajar privilegios y continuar como appuser
    exec gosu appuser /entrypoint.sh
fi

# --- A partir de aquí vamos como appuser ---
GPU="${GPU:-cpu}"

# Crear directorios temporales como appuser
mkdir -p /tmp/numba_cache /tmp/torch_cache /tmp/xdg_cache /tmp/hf_cache

echo "🚀 Starting Onda v3.1.2 ($GPU mode)..."
onda-backend serve --addr 0.0.0.0:3001 &

exec nginx -g "daemon off;"
