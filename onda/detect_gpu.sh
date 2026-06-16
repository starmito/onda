#!/usr/bin/env bash
set -euo pipefail

# Detect GPU availability and echo the appropriate backend.
#   - nvidia-smi → 'cuda'
#   - rocm-smi  → 'rocm'
#   - /dev/kfd + /dev/dri/renderD128 → 'rocm' (LXC containers without rocm-smi)
#   - otherwise  → 'cpu'

if command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null; then
    echo 'cuda'
elif command -v rocm-smi &>/dev/null && rocm-smi &>/dev/null; then
    echo 'rocm'
elif [ -e /dev/kfd ] && [ -e /dev/dri/renderD128 ]; then
    echo 'rocm'
else
    echo 'cpu'
fi
