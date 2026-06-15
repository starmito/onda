#!/usr/bin/env bash
set -euo pipefail

# Detect GPU availability and echo the appropriate backend.
#   - nvidia-smi → 'cuda'
#   - rocm-smi  → 'rocm'
#   - otherwise  → 'cpu'

if command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null; then
    echo 'cuda'
elif command -v rocm-smi &>/dev/null && rocm-smi &>/dev/null; then
    echo 'rocm'
else
    echo 'cpu'
fi
