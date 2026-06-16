#!/bin/bash
set -euo pipefail

GPU=$(detect_gpu.sh)
echo "🎯 GPU detected: $GPU"
CACHE_DIR="/opt/pytorch-backends/$GPU"

# Añadir cache al PYTHONPATH
export PYTHONPATH="$CACHE_DIR:$PYTHONPATH"

# Si no está cacheado, instalar backend completo
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
        *)
            pip install --target "$CACHE_DIR" torch==2.12.0+cpu torchaudio==2.11.0+cpu onnxruntime --index-url https://download.pytorch.org/whl/cpu
            ;;
    esac

    # Instalar paquetes que dependen de torch
    pip install --target "$CACHE_DIR" \
        diffq pytorch_lightning ml_collections onnx2pytorch \
        rotary_embedding_torch segmentation_models_pytorch \
        transformers timm torchmetrics spafe \
        torch_audiomentations asteroid openunmix dora-search

    echo "✅ $GPU backend + torch deps installed"
fi

# Crear directorios necesarios
mkdir -p /input /output /input_rubberband /config /tmp/numba_cache /tmp/torch_cache /tmp/xdg_cache /tmp/hf_cache

# Arrancar Go backend
export TORCH_HOME=/tmp/torch_cache
export NUMBA_CACHE_DIR=/tmp/numba_cache
export XDG_CACHE_HOME=/tmp/xdg_cache
export HF_HOME=/tmp/hf_cache

echo "🚀 Starting Onda v3.1.0 ($GPU mode)..."
onda-backend serve --addr 0.0.0.0:3001 &

# Arrancar nginx en foreground
exec nginx -g "daemon off;"
