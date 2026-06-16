#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

echo "🔍 Detectando hardware..."
GPU=$(bash onda/detect_gpu.sh)
echo "🎯 Hardware detectado: $GPU"

case $GPU in
  cuda)
    echo "🚀 Desplegando con aceleración NVIDIA CUDA..."
    docker compose -f docker-compose.yml -f docker-compose.cuda.yml up -d --build
    ;;
  rocm)
    echo "🚀 Desplegando con aceleración AMD ROCm..."
    docker compose -f docker-compose.yml -f docker-compose.rocm.yml up -d --build
    ;;
  *)
    echo "🚀 Desplegando en modo CPU..."
    docker compose up -d --build
    ;;
esac

echo "✅ Onda v3.1.0 desplegado en http://localhost:${ONDA_PORT:-3000}"
