#!/usr/bin/env bash
# Onda inference test: BS-Roformer + Demucs via API
# Generates a 10s stereo test tone and runs separation jobs through
# http://localhost:3000/api/separate, polling /api/queue/status.
#
# Requirements: bash, curl, ffmpeg, ffprobe, stat
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

# Generate 10s stereo WAV with 200Hz (L) + 440Hz (R), 44100Hz, 16-bit
generate_audio() {
    local dest="$1"
    ffmpeg -y -hide_banner -loglevel error \
        -f lavfi -i "sine=frequency=200:duration=10" \
        -f lavfi -i "sine=frequency=440:duration=10" \
        -filter_complex "[0:a][1:a]amerge=inputs=2" \
        -ar 44100 -sample_fmt s16 -ac 2 "$dest"
}

# Poll /api/queue/status until the given song is done/error or timeout
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

# Verify that output files exist, are >100KB, >=10s long, and owned by uid 1000
verify_outputs() {
    local song="$1"
    local outdir="${OUTPUT_DIR}/${song}"
    if [ ! -d "$outdir" ]; then
        echo "  ❌ output dir not found: $outdir"
        return 1
    fi
    local ok=true
    while IFS= read -r f; do
        [ -f "$f" ] || continue
        local size dur uid name
        size=$(stat -c%s "$f")
        dur=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$f" 2>/dev/null | cut -d. -f1)
        uid=$(stat -c%u "$f")
        name=$(basename "$f")
        if [ "${size:-0}" -lt 102400 ]; then
            echo "  ❌ $name too small: ${size:-0}B"
            ok=false
        else
            echo "  ✅ $name size=${size}B"
        fi
        if [ -z "$dur" ] || [ "$dur" -lt 10 ]; then
            echo "  ❌ $name duration < 10s: ${dur:-0}s"
            ok=false
        else
            echo "  ✅ $name duration=${dur}s"
        fi
        if [ "${uid:-0}" -ne 1000 ]; then
            echo "  ❌ $name owner uid != 1000: $uid"
            ok=false
        else
            echo "  ✅ $name owner uid=1000"
        fi
    done < <(find "$outdir" -maxdepth 1 -type f -name "*.wav")
    $ok
}

# Ensure input/output dirs exist
mkdir -p "$INPUT_DIR" "$OUTPUT_DIR"

# Prepare unique input files
echo "Generating test audio..."
TS=$(date +%s)
VIPERX_NAME="test_viperx_${TS}.wav"
DEMUCS_NAME="test_demucs_${TS}.wav"

if ! generate_audio "${TMP_DIR}/tone.wav"; then
    echo "❌ ffmpeg failed"
    exit 1
fi
cp "${TMP_DIR}/tone.wav" "${INPUT_DIR}/${VIPERX_NAME}" || { echo "❌ copy failed"; exit 1; }
cp "${TMP_DIR}/tone.wav" "${INPUT_DIR}/${DEMUCS_NAME}" || { echo "❌ copy failed"; exit 1; }

# ── Test 1: BS-Roformer ────────────────────────────────────────
echo ""
echo "Test 1: BS-Roformer (viperx)"
resp=$(curl -s -w "\n%{http_code}" -X POST "${BASE_URL}/api/separate" \
    -H "Content-Type: application/json" \
    -d "{\"input\":\"/input/${VIPERX_NAME}\",\"viperx\":true,\"viperx_keep\":\"both\"}")
code=$(echo "$resp" | tail -1)
body=$(echo "$resp" | sed '$d')
echo "  HTTP $code"
if [ "$code" = "202" ]; then
    song=$(echo "$body" | sed -n 's/.*"song":"\([^"]*\)".*/\1/p')
    if poll_job "${song:-test_viperx_${TS}}"; then
        check "BS-Roformer outputs valid" verify_outputs "${song:-test_viperx_${TS}}"
    else
        check "BS-Roformer job completed" false
    fi
else
    check "BS-Roformer accepted (202)" false
fi

# ── Test 2: Demucs ─────────────────────────────────────────────
echo ""
echo "Test 2: Demucs"
resp=$(curl -s -w "\n%{http_code}" -X POST "${BASE_URL}/api/separate" \
    -H "Content-Type: application/json" \
    -d "{\"input\":\"/input/${DEMUCS_NAME}\",\"demucs\":true,\"stem_model\":\"htdemucs\",\"demucs_segment\":5,\"jobs\":1}")
code=$(echo "$resp" | tail -1)
body=$(echo "$resp" | sed '$d')
echo "  HTTP $code"
if [ "$code" = "202" ]; then
    song=$(echo "$body" | sed -n 's/.*"song":"\([^"]*\)".*/\1/p')
    if poll_job "${song:-test_demucs_${TS}}"; then
        check "Demucs outputs valid" verify_outputs "${song:-test_demucs_${TS}}"
    else
        check "Demucs job completed" false
    fi
else
    check "Demucs accepted (202)" false
fi

# ── Summary ────────────────────────────────────────────────────
echo ""
if [ "$FAIL" -gt 0 ]; then
    echo "❌ Inference tests FAILED ($FAIL failures)"
    exit 1
fi
echo "✅ Inference tests PASSED ($PASS checks)"
exit 0
