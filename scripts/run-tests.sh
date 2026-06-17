#!/bin/bash
set -e

echo "=== TEST API ==="
bash tests/api/test_api.sh 2>&1

echo ""
echo "=== TEST INFERENCE (BS-Roformer + Demucs) ==="
bash tests/inference/test_inference.sh 2>&1

echo ""
echo "=== TEST E2E (frontend → backend → outputs) ==="
bash tests/e2e/test_frontend_backend.sh 2>&1

echo ""
echo "=== RESUMEN ==="
docker ps --filter name=onda --format '{{.Names}} {{.Status}}'
echo "--- output/ ---"
ls -la output/ 2>/dev/null | head -20
echo "--- owners ---"
stat -c "%U:%G %n" output/*/ 2>/dev/null | head -5
