#!/usr/bin/env bash
set -euo pipefail

# Detect GPU availability and echo the appropriate backend.
#   - nvidia-smi       → 'cuda'
#   - rocm-smi         → 'rocm'
#   - /dev/kfd         → 'rocm' (kernel ROCm cargado, aunque falte rocm-smi)
#   - otherwise        → 'cpu'
#
# /dev/kfd es un character device creado EXCLUSIVAMENTE por el kernel
# amdgpu cuando tiene soporte ROCm. No existe en sistemas sin ROCm.
# Es la señal más fiable — no necesita lspci ni paquetes extra.

if command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null; then
    echo 'cuda'
elif command -v rocm-smi &>/dev/null && rocm-smi &>/dev/null; then
    echo 'rocm'
elif [ -c /dev/kfd ]; then
    echo 'rocm'
else
    echo 'cpu'
fi
