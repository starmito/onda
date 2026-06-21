#!/usr/bin/env bash
# Onda frontend → backend end-to-end smoke test
# Verifies that nginx serves the frontend, proxies API calls, and serves
# generated audio files from /output/ after a successful separation job.
#
# Requirements: bash, curl, ffmpeg, ffprobe
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:3000}"
INPUT_DIR="${INPUT_DIR:-./input}"
OUTPUT_DIR="${OUTPUT_DIR:-./output}"
MAX_WAIT="${MAX_WAIT:-600}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

PASS=0
FAIL=0

check() {
    local desc="$1"
    shift
    if "$@"; then
        echo "  ✅ $desc"
        PASS=$((PASS + 1))
    else
        echo "  ❌ $desc"
        FAIL=$((FAIL + 1))
    fi
}

expect_status() {
    local desc="$1"
    local method="$2"
    local path="$3"
    local expected="$4"
    shift 4
    local code
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" "$@" "${BASE_URL}${path}")
    if [ "$code" = "$expected" ]; then
        echo "  ✅ $desc → $code"
        PASS=$((PASS + 1))
    else
        echo "  ❌ $desc → $code (expected $expected)"
        FAIL=$((FAIL + 1))
    fi
}

poll_job() {
    local song="$1"
    local elapsed=0
    while [ "$elapsed" -lt "$MAX_WAIT" ]; do
        sleep 5
        elapsed=$((elapsed + 5))
        local json status
        json=$(curl -s "${BASE_URL}/api/queue/status")
        status=$(echo "$json" | sed -n "s/.*\"song\":\"${song}\"[^}]*\"status\":\"\([^\"]*\)\".*/\1/p")
        echo "  [poll] song=${song} elapsed=${elapsed}s status=${status:-unknown}"
        case "${status:-}" in
            done) return 0 ;;
            error) return 1 ;;
        esac
    done
    echo "  ❌ timeout waiting for ${song}"
    return 1
}

echo "Onda frontend-to-backend chain test against ${BASE_URL}"
echo ""

# ── Frontend ───────────────────────────────────────────────────
echo "Checking frontend..."
expect_status "GET / returns 200" GET / 200
html=$(curl -s "${BASE_URL}/")
if echo "$html" | grep -qiE "Onda|html"; then
    echo "  ✅ Frontend body contains 'Onda' or 'html'"
    PASS=$((PASS + 1))
else
    echo "  ❌ Frontend body missing expected content"
    FAIL=$((FAIL + 1))
fi

# ── Static output directory ────────────────────────────────────
echo ""
echo "Checking static output serving..."
expect_status "GET /output/ returns 200" GET /output/ 200

# ── API proxied through nginx ──────────────────────────────────
echo ""
echo "Checking API proxy..."
expect_status "GET /api/health returns 200" GET /api/health 200

# ── Full separation chain ──────────────────────────────────────
echo ""
echo "Running full separation chain..."
TS=$(date +%s)
SONG="test_e2e_${TS}"
INPUT_NAME="${SONG}.wav"

mkdir -p "$INPUT_DIR" "$OUTPUT_DIR"
ffmpeg -y -hide_banner -loglevel error \
    -f lavfi -i "sine=frequency=200:duration=10" \
    -f lavfi -i "sine=frequency=440:duration=10" \
    -filter_complex "[0:a][1:a]amerge=inputs=2" \
    -ar 44100 -sample_fmt s16 -ac 2 "${INPUT_DIR}/${INPUT_NAME}"

resp=$(curl -s -w "\n%{http_code}" -X POST "${BASE_URL}/api/separate" \
    -H "Content-Type: application/json" \
    -d "{\"input\":\"/app/input/${INPUT_NAME}\",\"viperx\":true,\"viperx_keep\":\"both\"}")
code=$(echo "$resp" | tail -1)
body=$(echo "$resp" | sed '$d')
echo "  POST /api/separate → $code"
if [ "$code" != "202" ]; then
    echo "  Body: $body"
    check "Separation accepted (202)" false
    echo ""
    echo "❌ E2E tests FAILED ($FAIL failures)"
    exit 1
fi

song=$(echo "$body" | sed -n 's/.*"song":"\([^"]*\)".*/\1/p')
song="${song:-$SONG}"
if ! poll_job "$song"; then
    check "Separation job completed" false
    echo ""
    echo "❌ E2E tests FAILED ($FAIL failures)"
    exit 1
fi

# Find a generated WAV in output/<song>/
outdir="${OUTPUT_DIR}/${song}"
file=$(find "$outdir" -maxdepth 1 -type f -name "*.wav" | head -1)
if [ -z "$file" ]; then
    check "Output WAV exists" false
    echo ""
    echo "❌ E2E tests FAILED ($FAIL failures)"
    exit 1
fi
filename=$(basename "$file")
echo "  Output file: ${song}/${filename}"

# Verify it is served by nginx
url="/output/${song}/${filename}"
expect_status "GET ${url} returns 200" GET "$url" 200

# Verify duration via HTTP ffprobe
dur=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "${BASE_URL}${url}" 2>/dev/null | cut -d. -f1)
if [ -n "$dur" ] && [ "$dur" -ge 10 ]; then
    echo "  ✅ HTTP-served WAV duration=${dur}s (>=10s)"
    PASS=$((PASS + 1))
else
    echo "  ❌ HTTP-served WAV duration < 10s: ${dur:-unknown}"
    FAIL=$((FAIL + 1))
fi

# ── Summary ────────────────────────────────────────────────────
echo ""
if [ "$FAIL" -gt 0 ]; then
    echo "❌ E2E tests FAILED ($FAIL failures)"
    exit 1
fi
echo "✅ E2E tests PASSED ($PASS checks)"
exit 0
