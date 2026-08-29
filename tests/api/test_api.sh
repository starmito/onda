#!/usr/bin/env bash
# Onda API route/method smoke test
# Verifies HTTP status codes for common endpoints on localhost:3000.
#
# Requirements: bash, curl
set -uo pipefail

# Generate test audio in ./input/ mounted as /input/ in container
TEST_AUDIO="$(dirname "$0")/../input/test_api_valid.wav"
if [ ! -f "$TEST_AUDIO" ]; then
    ffmpeg -y -f lavfi -i "sine=f=440:r=44100:d=10" -ac 2 "$TEST_AUDIO" 2>/dev/null
fi

BASE_URL="${BASE_URL:-http://localhost:3000}"

PASS=0
FAIL=0

# Expect a specific HTTP status code for a request.
# Usage: expect_status <description> <method> <path> <expected_code> [curl_extra_args...]
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

echo "Onda API route tests against ${BASE_URL}"
echo ""

expect_status "GET /api/health" GET /api/health 200
expect_status "POST /api/health" POST /api/health 405
expect_status "GET /api/models" GET /api/models 200
expect_status "GET /api/gpu" GET /api/gpu 200
expect_status "GET /api/queue/status" GET /api/queue/status 200
expect_status "GET /api/nonexistent" GET /api/nonexistent 404
expect_status "POST /api/separate with valid JSON" POST /api/separate 202 \
    -H "Content-Type: application/json" \
    -d '{"input":"/app/input/test_api_valid.wav","viperx":true}'
expect_status "GET /api/separate" GET /api/separate 405

echo ""
if [ "$FAIL" -gt 0 ]; then
    echo "❌ API tests FAILED ($FAIL failures)"
    exit 1
fi
echo "✅ API tests PASSED ($PASS checks)"
exit 0
