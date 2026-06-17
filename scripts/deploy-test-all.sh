#!/bin/bash
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_DIR"

echo "=== 1. Clean ==="
docker compose -f docker-compose.yml -f docker-compose.cuda.yml down -v 2>/dev/null
docker rm -f onda 2>/dev/null
docker volume rm onda_pytorch-cache 2>/dev/null
docker rmi onda:3.1.1 2>/dev/null
rm -rf output/* input/* 2>/dev/null
mkdir -p output input

echo "=== 2. Pull ==="
git pull

echo "=== 3. Build ==="
docker compose -f docker-compose.yml -f docker-compose.cuda.yml build --no-cache 2>&1

echo "=== 4. Deploy ==="
docker compose -f docker-compose.yml -f docker-compose.cuda.yml up -d 2>&1
sleep 5

echo "=== 5. Verify ==="
docker ps | grep onda
docker exec onda id
docker logs onda --tail 10

echo "=== 6. Tests ==="
echo "--- TEST API ---"
bash tests/api/test_api.sh 2>&1 || echo "TEST API FAILED"

echo "--- TEST INFERENCE ---"
bash tests/inference/test_inference.sh 2>&1 || echo "TEST INFERENCE FAILED"

echo "--- TEST E2E ---"
bash tests/e2e/test_frontend_backend.sh 2>&1 || echo "TEST E2E FAILED"

echo "=== 7. Summary ==="
docker ps | grep onda
ls -la output/ 2>/dev/null | head -20
stat -c "%U:%G" output/*/ 2>/dev/null | head -5
echo "=== DONE ==="
