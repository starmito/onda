#!/usr/bin/env bash
# Onda CLI — wrapper que detecta GPU y ejecuta en Docker
#
# Uso:
#   onda pipeline [flags] <input>     → pipeline completo
#   onda shell                         → bash dentro del contenedor
#   onda --help                        → esto
#
# Auto-detecta NVIDIA vs AMD y elige la imagen correcta.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Auto-detect GPU ──────────────────────────────────
detect_gpu() {
    if lspci 2>/dev/null | grep -qi nvidia; then
        echo "nvidia"
    elif lspci 2>/dev/null | grep -qiE 'amd.*(vga|3d|display)'; then
        echo "amd"
    elif [ -d /dev/dri ] && ls /dev/dri/render* 2>/dev/null | grep -q .; then
        echo "amd"  # fallback: GPU presente pero no identificada
    else
        echo "nvidia"  # último fallback
    fi
}

GPU_TYPE="${GPU_TYPE:-$(detect_gpu)}"

case "$GPU_TYPE" in
    nvidia)
        GPU_DOCKERFILE="Dockerfile"
        IMAGE="onda:nvidia"
        ;;
    amd)
        GPU_DOCKERFILE="Dockerfile.amd"
        IMAGE="onda:amd"
        ;;
    *)
        echo "❌ Unknown GPU_TYPE: ${GPU_TYPE} (use nvidia|amd)"
        exit 1
        ;;
esac

# ── Ensure container is running ──────────────────────
if ! docker ps --format '{{.Names}}' | grep -qx onda 2>/dev/null; then
    echo "🚀 Starting Onda (${GPU_TYPE})..."
    GPU_TYPE="${GPU_TYPE}" GPU_DOCKERFILE="${GPU_DOCKERFILE}" \
        docker compose -f "${SCRIPT_DIR}/docker-compose.yml" up -d onda 2>&1 | tail -3
fi

# ── Route command ────────────────────────────────────
case "${1:-}" in
    pipeline)
        shift
        docker exec -i onda bash /app/pipeline.sh "$@"
        ;;
    shell)
        docker exec -it onda bash
        ;;
    --help|-h|"")
        echo "🎵 Onda — Audio stem separation"
        echo ""
        echo "  onda pipeline [flags] <input>   Run separation pipeline"
        echo "    --viperx                      Extract vocals/instrumental"
        echo "    --viperx-keep WHAT            instrumental | vocals | both"
        echo "    --demucs                      Separate 4 stems"
        echo "    --demucs-keep LIST            drums,bass,other,vocals | all"
        echo "    --rubberband                  Pitch shift"
        echo "    --pitch N                     Semitones (±12)"
        echo "  onda shell                      Open bash in container"
        echo "  onda --help                     This message"
        echo ""
        echo "  GPU detected: ${GPU_TYPE}"
        echo "  Image: ${IMAGE}"
        echo "  Config: ${SCRIPT_DIR}/docker-compose.yml"
        ;;
    *)
        docker exec -i onda "$@"
        ;;
esac
