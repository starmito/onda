#!/bin/bash
# deploy-test-rocm.sh — Valida un despliegue de Onda con AMD ROCm.
# Uso: bash deploy-test-rocm.sh

set -u

REPO_DIR="/home/starmito/onda"
BACKEND="rocm"
COMPOSE_FILE="docker-compose.rocm.yml"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy-test-common.sh
source "${SCRIPT_DIR}/deploy-test-common.sh"

run_all_steps
