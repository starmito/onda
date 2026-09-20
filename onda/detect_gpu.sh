#!/usr/bin/env bash
set -euo pipefail

# Detect GPU availability and echo the appropriate backend.
#   - nvidia-smi       → 'cuda'
#   - otherwise        → 'cpu'
#
# Nota: la ruta AMD/ROCm fue retirada en v3.5.0 porque no se valida
# a medio plazo. Recuperable desde la historia de git si algún día
# vuelve a ser prioridad.

if command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null; then
    echo 'cuda'
else
    echo 'cpu'
fi
