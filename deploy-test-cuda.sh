#!/bin/bash
# deploy-test-cuda.sh — Valida un despliegue de Onda con NVIDIA CUDA.
# Uso: bash deploy-test-cuda.sh

set -u

REPO_DIR="/home/starmito/projects/onda"
BACKEND="cuda"
COMPOSE_FILE="docker-compose.cuda.yml"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy-test-common.sh
source "${SCRIPT_DIR}/deploy-test-common.sh"

run_all_steps
