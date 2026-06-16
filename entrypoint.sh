#!/bin/bash
set -euo pipefail

GPU=$(detect_gpu.sh)
echo "🎯 GPU detected: $GPU"

# Para CPU: torch ya está en la imagen, no hacer nada extra
if [ "$GPU" != "cpu" ]; then
    CACHE_DIR="/opt/pytorch-backends/$GPU"
    export PYTHONPATH="${PYTHONPATH:-}:$CACHE_DIR"

    if [ ! -f "$CACHE_DIR/torch/__init__.py" ]; then
        echo "📦 Installing $GPU backend..."
        mkdir -p "$CACHE_DIR"
        case $GPU in
            cuda)
                pip install --target "$CACHE_DIR" torch==2.12.0 torchaudio==2.11.0 torchvision==0.27.0 onnxruntime-gpu==1.26.0
                ;;
            rocm)
                pip install --target "$CACHE_DIR" torch==2.11.0+rocm7.2 torchaudio==2.11.0+rocm7.2 torchvision==0.27.0+rocm7.2 onnxruntime --index-url https://download.pytorch.org/whl/rocm7.2
                ;;
        esac
        echo "✅ $GPU backend installed"
    fi
fi

# Crear directorios
mkdir -p /input /output /input_rubberband /config /tmp/numba_cache /tmp/torch_cache /tmp/xdg_cache /tmp/hf_cache

export TORCH_HOME=/tmp/torch_cache
export NUMBA_CACHE_DIR=/tmp/numba_cache
export XDG_CACHE_HOME=/tmp/xdg_cache
export HF_HOME=/tmp/hf_cache

echo "🚀 Starting Onda v3.1.0 ($GPU mode)..."
onda-backend serve --addr 0.0.0.0:3001 &

exec nginx -g "daemon off;"
