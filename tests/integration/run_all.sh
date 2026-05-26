#!/bin/bash
set -euo pipefail
echo "=== Onda Fase 5 — Testing Integral ==="
echo ""

# 1. Generate fixtures
echo "[1/5] Generando audio de prueba..."
python3 tests/integration/generate_test_audio.py

# 2. Copy fixtures to container
echo "[2/5] Copiando fixtures al contenedor..."
docker exec onda mkdir -p /app/tests/integration/fixtures/
docker cp tests/integration/fixtures/. onda:/app/tests/integration/fixtures/ 2>/dev/null || \
  echo "  ⚠️  No se pudieron copiar fixtures (contenedor local?)"

# 3. Unit tests (Go backend)
echo "[3/5] Tests unitarios Go..."
cd backend && go test ./... -v -count=1 && cd ..

# 4. Integration tests
echo "[4/5] Tests E2E..."
python3 -m pytest tests/integration/ -v --timeout=120 --ignore=tests/integration/test_pipeline_api.py 2>/dev/null || \
  python3 -m pytest tests/integration/ -v --timeout=120 -k "not api"

# 5. Benchmarks
echo "[5/5] Benchmarks..."
python3 tests/integration/benchmark.py

echo ""
echo "=== Fase 5 completa ==="
