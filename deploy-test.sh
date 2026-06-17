#!/bin/bash
# deploy-test.sh — Script único para validar despliegues de Onda.
# Auto-detecta GPU, repositorio y perfil de docker compose.
#
# Uso:
#   bash deploy-test.sh         # detectar GPU automáticamente
#   bash deploy-test.sh --cpu   # forzar modo CPU

set -u

CPU_MODE=false
BACKEND=""
COMPOSE_ARGS=""
EXTRA_DOCKER_ENV=""

# a) Flag --cpu
for arg in "$@"; do
    if [ "$arg" = "--cpu" ]; then
        CPU_MODE=true
    fi
done

# b) Auto-detectar GPU
if [ "$CPU_MODE" = true ]; then
    BACKEND="cpu"
elif command -v nvidia-smi >/dev/null 2>&1; then
    BACKEND="cuda"
elif [ -e /dev/kfd ]; then
    BACKEND="rocm"
else
    echo "No GPU detected. Use --cpu for CPU-only test."
    exit 1
fi

# c) Auto-detectar REPO_DIR
REPO_DIR=""
for candidate in /home/starmito/projects/onda /home/starmito/onda "$(pwd)" "$HOME/projects/onda"; do
    if [ -f "$candidate/docker-compose.yml" ]; then
        REPO_DIR="$candidate"
        break
    fi
done

if [ -z "$REPO_DIR" ]; then
    echo "❌ No se encontró el repositorio (falta docker-compose.yml)."
    exit 1
fi

# d) Auto-elegir compose según GPU
if [ "$BACKEND" = "cuda" ]; then
    COMPOSE_ARGS="-f docker-compose.yml -f docker-compose.cuda.yml"
elif [ "$BACKEND" = "rocm" ]; then
    COMPOSE_ARGS="-f docker-compose.yml -f docker-compose.rocm.yml"
else
    COMPOSE_ARGS="-f docker-compose.yml"
fi

echo "════════════════════════════════════════════════════"
echo "  Onda deploy-test"
echo "  BACKEND:  $BACKEND"
echo "  REPO_DIR: $REPO_DIR"
echo "  COMPOSE:  docker compose $COMPOSE_ARGS"
echo "════════════════════════════════════════════════════"

cd "$REPO_DIR"

# e) git pull
run_step() {
    echo ""
    echo "==> $1"
    if "${@:2}"; then
        echo "   ✅ $1"
    else
        echo "   ❌ $1"
        return 1
    fi
}

run_step "git pull" git pull

# f) build --no-cache + deploy
run_step "docker compose build --no-cache" \
    docker compose $COMPOSE_ARGS build --no-cache

run_step "docker compose up -d --force-recreate" \
    docker compose $COMPOSE_ARGS up -d --force-recreate

# g) sleep 3
sleep 3

# h) Verificar GPU en logs (si no es cpu)
if [ "$BACKEND" != "cpu" ]; then
    if docker logs onda 2>&1 | grep -q "GPU detected: ${BACKEND}"; then
        echo ""
        echo "   ✅ GPU detectada en logs: $BACKEND"
    else
        echo ""
        echo "   ❌ No se encontró 'GPU detected: $BACKEND' en los logs"
        exit 1
    fi
fi

# i) Generar audio
python3 scripts/gen-test-audio.py

# j) Copiar test_sound.wav a input/
cp test_sound.wav input/

# k) Exportar EXTRA_DOCKER_ENV si rocm
if [ "$BACKEND" = "rocm" ]; then
    export EXTRA_DOCKER_ENV="-e HSA_OVERRIDE_GFX_VERSION=11.0.0"
    export VIPERX_OVERLAP=2
    export DEMUCS_EXTRA="-n htdemucs --segment 1 --jobs 1"
fi

# l) Sourcear deploy-test-common.sh y llamar run_all_steps
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy-test-common.sh
source "${SCRIPT_DIR}/deploy-test-common.sh"

run_all_steps
