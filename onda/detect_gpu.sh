#!/usr/bin/env bash
set -euo pipefail

# Detect GPU availability and echo the appropriate backend.
#   - nvidia-smi → 'cuda'
#   - rocm-smi  → 'rocm' (solo si ROCm stack está instalado)
#   - otherwise  → 'cpu'
#
# NOTA: No detectamos por /dev/kfd + /dev/dri porque en LXC pueden
# existir sin que ROCm esté realmente disponible (ej: Cezanne gfx90c).

if command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null; then
    echo 'cuda'
elif command -v rocm-smi &>/dev/null && rocm-smi &>/dev/null; then
    echo 'rocm'
else
    echo 'cpu'
fi
