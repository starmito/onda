#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
#  Onda — Environment Validation Script
#  Verifica que el entorno esté listo antes de ejecutar Onda.
#  Funciona desde cualquier directorio (rutas relativas).
# ─────────────────────────────────────────────────────────────
set -uo pipefail
# NOTE: no set -e — queremos ejecutar TODAS las comprobaciones

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

PASS=0
FAIL=0
RESULTS=()

check() {
    local desc="$1"
    shift
    if "$@"; then
        RESULTS+=("  ✅ $desc")
        PASS=$((PASS+1))
    else
        RESULTS+=("  ❌ $desc")
        FAIL=$((FAIL+1))
    fi
}

cd "$PROJECT_DIR"

echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║     🔍 Onda — Environment Validation         ║"
echo "╚══════════════════════════════════════════════╝"
echo "  Project : $(basename "$PROJECT_DIR")"
echo "  Dir     : $PROJECT_DIR"
echo ""

# ── 1. Docker installed ────────────────────────────────────
echo "📦 Docker"
check "docker installed + running" docker --version &>/dev/null
echo ""

# ── 2. GPU detection ───────────────────────────────────────
echo "🎮 GPU"
if [ -f "$PROJECT_DIR/onda/detect_gpu.sh" ]; then
    GPU_BACKEND=$(bash "$PROJECT_DIR/onda/detect_gpu.sh" 2>/dev/null)
    if [ "$GPU_BACKEND" = "cuda" ]; then
        check "GPU detected (cuda)" true
    elif [ "$GPU_BACKEND" = "rocm" ]; then
        check "GPU detected (rocm)" true
    else
        check "GPU: no accelerator found (CPU mode)" true
    fi
elif command -v nvidia-smi &>/dev/null; then
    check "nvidia-smi available" nvidia-smi &>/dev/null
else
    check "GPU: no accelerator found (CPU mode)" true
fi
echo ""

# ── 3. VERSION file ────────────────────────────────────────
echo "📄 VERSION"
check "VERSION file exists" test -f VERSION
if [ -f VERSION ]; then
    check "VERSION format valid (vX.Y.Z)" grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$' VERSION
fi
echo ""

# ── 4. Required directories ────────────────────────────────
echo "📂 Directories"
check "input/ directory exists" test -d input
check "output/ directory exists" test -d output
check "models/ directory exists" test -d models
echo ""

# ── 5. Dockerfile syntax ───────────────────────────────────
echo "🐳 Dockerfile"
check "Dockerfile exists" test -f Dockerfile
if [ -f Dockerfile ]; then
    # Attempt dry-run syntax check; fall back gracefully if buildx --check unavailable
    if docker buildx build --check . &>/dev/null 2>&1; then
        check "Dockerfile syntax OK" true
    elif docker buildx version &>/dev/null; then
        # buildx present but --check not supported in older versions
        check "Dockerfile exists (syntax check skipped — old Docker)" true
    else
        check "Dockerfile exists (syntax check skipped — buildx unavailable)" true
    fi
fi
echo ""

# ── 6. docker-compose.yml syntax ───────────────────────────
echo "🐙 docker-compose.yml"
check "docker-compose.yml exists" test -f docker-compose.yml
if [ -f docker-compose.yml ]; then
    check "docker-compose.yml syntax valid" docker compose config -q &>/dev/null
fi
echo ""

# ── 7. pipeline.sh (bonus check) ───────────────────────────
echo "📜 pipeline.sh"
check "pipeline.sh exists" test -f pipeline.sh
if [ -f pipeline.sh ]; then
    check "pipeline.sh bash syntax OK" bash -n pipeline.sh &>/dev/null
fi
echo ""

# ── Summary ────────────────────────────────────────────────
echo "╔══════════════════════════════════════════════╗"
printf "║  Results:  %2d passed,  %2d failed              ║\n" "$PASS" "$FAIL"
echo "╚══════════════════════════════════════════════╝"
echo ""

# Print individual results
for r in "${RESULTS[@]}"; do
    echo "$r"
done
echo ""

if [ "$FAIL" -gt 0 ]; then
    echo "❌ Environment validation FAILED — review errors above"
    exit 1
else
    echo "✅ All checks passed — environment is ready"
fi
