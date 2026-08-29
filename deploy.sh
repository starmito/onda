#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

echo "🔍 Detectando hardware..."
GPU=$(bash onda/detect_gpu.sh)
echo "🎯 Hardware detectado: $GPU"

# Resolver versiones desde los tags de git (misma lógica que build.sh).
# Es necesario exportarlas porque docker-compose.yml las inyecta como ARG
# en build time y .dockerignore excluye .git, por lo que el contenedor no
# puede calcularlas por sí solo.
source ./build.sh --version
export ONDAP_VERSION GUI_VERSION

# Directorios montados como bind volumes (deben pertenecer al usuario host)
BIND_DIRS="output input input_rubberband daw-data config"

mkdir -p $BIND_DIRS

# Ensure dirs are not root-owned from previous runs
NEED_RESET=0
for dir in $BIND_DIRS; do
    if [ -d "$dir" ] && [ "$(stat -c '%u' "$dir" 2>/dev/null || echo 0)" != "$(id -u)" ]; then
        NEED_RESET=1
        break
    fi
done

if [ "$NEED_RESET" -eq 1 ]; then
    sudo rm -rf $BIND_DIRS 2>/dev/null || true
    mkdir -p $BIND_DIRS
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

echo "✅ Onda desplegado en http://localhost:${ONDA_PORT:-3000}"
