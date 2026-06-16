#!/usr/bin/env bash
set -euo pipefail

# Detect GPU availability and echo the appropriate backend.
#   - nvidia-smi       → 'cuda'
#   - rocm-smi         → 'rocm' (ROCm stack + rocm-smi package)
#   - /dev/kfd + AMD GPU lspci → 'rocm' (ROCm kernel loaded, sin rocm-smi)
#   - otherwise        → 'cpu'
#
# En Ubuntu 26.04: 'sudo apt install rocm' instala el stack ROCm pero
# NO incluye rocm-smi (paquete separado).  El fallback por /dev/kfd
# + lspci cubre ese caso sin falsos positivos en LXC sin passthrough.

if command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null; then
    echo 'cuda'
elif command -v rocm-smi &>/dev/null && rocm-smi &>/dev/null; then
    echo 'rocm'
elif [ -c /dev/kfd ] && command -v lspci &>/dev/null && lspci -nn 2>/dev/null | grep -qiE '(amd|ati).*(vga|display|3d|gpu)'; then
    echo 'rocm'
else
    echo 'cpu'
fi
