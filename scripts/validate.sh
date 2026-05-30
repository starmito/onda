#!/usr/bin/env bash
# Onda pre-build validation — catches issues BEFORE docker build
# Run: bash scripts/validate.sh
set -uo pipefail
# NOTE: no set -e — we want to run ALL checks even if some fail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
pass=0; fail=0; warn=0

check() { if [ $? -eq 0 ]; then echo -e "  ${GREEN}✓${NC} $1"; pass=$((pass+1)); else echo -e "  ${RED}✗${NC} $1"; fail=$((fail+1)); fi }
warn_check() { if [ $? -eq 0 ]; then echo -e "  ${GREEN}✓${NC} $1"; pass=$((pass+1)); else echo -e "  ${YELLOW}⚠${NC} $1"; warn=$((warn+1)); fi }

echo "═══════════════════════════════════════"
echo "🔍 Onda Pre-Build Validation"
echo "═══════════════════════════════════════"
echo ""

# ── 1. Required files ──
echo "📁 Required files"
[ -f pipeline.sh ]        ; check "pipeline.sh"
[ -f inference_universal.py ]; check "inference_universal.py"
[ -f Dockerfile ] || [ -f Dockerfile.v2 ]       ; check "Dockerfile"
[ -f docker-compose.yml ]  ; check "docker-compose.yml"
[ -f .dockerignore ]       ; check ".dockerignore"
[ -f requirements-docker-v2.txt ]; check "requirements-docker-v2.txt"
echo ""

# ── 2. Bash syntax ──
echo "🔧 Bash syntax"
bash -n pipeline.sh        ; check "pipeline.sh syntax OK"

# Check for common anti-patterns
grep -q '[^a-zA-Z]jq ' pipeline.sh && { echo -e "  ${YELLOW}⚠${NC} pipeline.sh contains 'jq ' (should use python3)"; warn=$((warn+1)); } || echo -e "  ${GREEN}✓${NC} pipeline.sh jq-free"; pass=$((pass+1))
grep -q '^[^#]*docker exec' pipeline.sh && { echo -e "  ${YELLOW}⚠${NC} pipeline.sh has docker exec"; warn=$((warn+1)); } || { echo -e "  ${GREEN}✓${NC} pipeline.sh no docker exec"; pass=$((pass+1)); }
echo ""

# ── 3. Python syntax ──
echo "🐍 Python syntax"
python3 -c "import ast; ast.parse(open('inference_universal.py').read())" 2>/dev/null; check "inference_universal.py syntax OK"

# Check lib_v5/ if it exists
if [ -d lib_v5 ]; then
    py_ok=true
    for f in lib_v5/*.py; do
        python3 -c "import ast; ast.parse(open('$f').read())" 2>/dev/null || py_ok=false
    done
    $py_ok && check "lib_v5/*.py syntax OK" || { echo -e "  ${RED}✗${NC} lib_v5/*.py has syntax errors"; ((fail++)); }
fi
echo ""

# ── 4. Dockerfile sanity ──
echo "🐳 Dockerfile checks"
grep -q 'FROM.*runtime' Dockerfile ; check "Dockerfile has multi-stage (runtime)"
grep -q 'python3\|python' Dockerfile ; warn_check "Dockerfile references python"

# Check .dockerignore has critical excludes
for pattern in 'venv/' '__pycache__/' 'models/' '.git/' 'output'; do
    grep -q "$pattern" .dockerignore && check ".dockerignore: $pattern" || { echo -e "  ${YELLOW}⚠${NC} .dockerignore missing '$pattern'"; ((warn++)); }
done
echo ""

# ── 5. Model paths ──
echo "🤖 Model paths"
MODEL_DIR="${ONDA_MODEL_DIR:-/mnt/almacen/onda/models}"
if [ -d "$MODEL_DIR" ]; then
    check "Model dir exists: $MODEL_DIR"
    # Check ViperX model
    VIPERX_PATH="$MODEL_DIR/VR_Models/BS_Roformer_Viperx"
    [ -d "$VIPERX_PATH" ] && check "ViperX model: $VIPERX_PATH" || warn_check "ViperX model: $VIPERX_PATH (not found — OK for build, needed for runtime)"

    # Check Demucs model
    DEMUCS_PATH="$MODEL_DIR/Demucs_ONNX"
    [ -d "$DEMUCS_PATH" ] && check "ONNX stems: $DEMUCS_PATH" || warn_check "ONNX stems: $DEMUCS_PATH (not found — Demucs included in Docker image)"
else
    warn_check "Model dir: $MODEL_DIR (not accessible from this machine — OK for build)"
fi
echo ""

# ── 6. Docker build context size check ──
echo "📦 Build context"
if command -v du &>/dev/null; then
    SIZE=$(du -sh . --exclude=models --exclude=venv --exclude=input --exclude=output --exclude=output_* --exclude=.git --exclude=frontend/node_modules 2>/dev/null | cut -f1)
    echo -e "  ${GREEN}ℹ${NC}  Build context (excl. models/venv/input/output): ~$SIZE"
fi
echo ""

# ── 7. Git state ──
# ── 7. Git state ──
echo "🔀 Git state"
if git rev-parse --git-dir >/dev/null 2>&1; then
    git diff --quiet || { echo -e "  ⚠ Uncommitted changes exist"; warn=1; }
    git diff --cached --quiet || { echo -e "  ⚠ Staged changes not committed"; warn=1; }
    UNPUSHED=0
    if [ "" -eq 0 ]; then echo -e "  ✓ All commits pushed"; pass=1; else echo -e "  ⚠  unpushed commits"; warn=1; fi
else
    echo -e "  ⚠ Not a git repository — skipping git checks"; warn=1
fi
echo ""

# ── Summary ──
echo "═══════════════════════════════════════"
echo -e "  ${GREEN}Passed: $pass${NC}  ${RED}Failed: $fail${NC}  ${YELLOW}Warnings: $warn${NC}"
echo "═══════════════════════════════════════"

if [ "$fail" -gt 0 ]; then
    echo -e "${RED}❌ Validation FAILED — fix errors before building${NC}"
    exit 1
elif [ "$warn" -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Validation passed with warnings — review before building${NC}"
else
    echo -e "${GREEN}✅ All checks passed — ready to build${NC}"
fi
