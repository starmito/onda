#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

echo "🔍 Detectando hardware..."
GPU=$(bash onda/detect_gpu.sh)
echo "🎯 Hardware detectado: $GPU"

mkdir -p output input
# Ensure dirs are not root-owned from previous runs
if [ -d "output" ] && [ "$(stat -c '%u' output 2>/dev/null || echo 0)" != "$(id -u)" ]; then
    sudo rm -rf output input 2>/dev/null || true
    mkdir -p output input
fi

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

echo "✅ Onda v3.1.1 desplegado en http://localhost:${ONDA_PORT:-3000}"
