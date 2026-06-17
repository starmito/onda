#!/usr/bin/env bash
set -euo pipefail

# End-to-end test for the Onda API on localhost:3000.
# Requires: bash, curl, ffmpeg, ffprobe, stat.
# No Python, no Node, no external test dependencies.

API_URL="${ONDA_API_URL:-http://localhost:3000}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
INPUT_DIR="${PROJECT_ROOT}/input"
OUTPUT_DIR="${PROJECT_ROOT}/output"
TEST_WAV="/tmp/onda_e2e_test_input.wav"

PASSED=0
FAILED=0

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

fail() {
    log "FAIL: $*"
    FAILED=$((FAILED + 1))
}

pass() {
    log "PASS: $*"
    PASSED=$((PASSED + 1))
}

json_value() {
    # Extract a JSON string value for a given key from the first argument.
    # Uses grep PCRE; works on compact JSON produced by the Onda API.
    local key="$1" json="$2"
    printf '%s' "$json" | grep -oP "\"${key}\"\s*:\s*\"\K[^\"]*" | head -1
}

extract_job_status() {
    # Given queue JSON and a song name, return the job's status.
    local queue="$1" song="$2"
    # Match the job object for this song and pull its status value.
    printf '%s' "$queue" | grep -oP "\"song\":\"${song}\".*?\"status\":\"\K[^\"]*" | head -1
}

extract_job_error() {
    # Given queue JSON and a song name, return the job's error message if any.
    local queue="$1" song="$2"
    printf '%s' "$queue" | grep -oP "\"song\":\"${song}\".*?\"error\":\"\K[^\"]*" | head -1
}

generate_audio() {
    log "Generating 10s stereo 44100Hz sine-wave test audio..."
    if ! ffmpeg -y -f lavfi -i "sine=f=200:r=44100:samples=441000" \
               -f lavfi -i "sine=f=440:r=44100:samples=441000" \
               -filter_complex "[0:a][1:a]amix=inputs=2:duration=first,volume=0.5[a]" \
               -map "[a]" -ac 2 "${TEST_WAV}" >/dev/null 2>&1; then
        fail "ffmpeg could not generate test audio"
        return 1
    fi
    pass "test audio generated (${TEST_WAV})"
    return 0
}

copy_input() {
    local name="$1"
    local dest="${INPUT_DIR}/${name}.wav"
    cp "${TEST_WAV}" "${dest}"
    log "Copied test audio to ${dest}"
}

post_separate() {
    local payload="$1"
    local resp
    resp=$(curl -s -X POST "${API_URL}/api/separate" \
                  -H "Content-Type: application/json" \
                  -d "$payload")
    printf '%s' "$resp"
}

wait_for_job() {
    local song="$1"
    local max_wait="${2:-600}"
    local interval="${3:-5}"
    local waited=0

    log "Polling /api/queue/status for song=${song} (max ${max_wait}s)..."
    while [ "$waited" -lt "$max_wait" ]; do
        local queue status
        queue=$(curl -s "${API_URL}/api/queue/status")
        status=$(extract_job_status "$queue" "$song")

        if [ "$status" = "done" ]; then
            pass "job ${song} completed"
            return 0
        fi
        if [ "$status" = "error" ]; then
            local err
            err=$(extract_job_error "$queue" "$song")
            fail "job ${song} failed: ${err:-unknown error}"
            return 1
        fi
        if [ -z "$status" ]; then
            fail "job ${song} not found in queue"
            return 1
        fi

        log "  status=${status}, elapsed=${waited}s"
        sleep "$interval"
        waited=$((waited + interval))
    done

    fail "timeout waiting for job ${song}"
    return 1
}

verify_output_files() {
    local song="$1"
    local song_dir="${OUTPUT_DIR}/${song}"

    if [ ! -d "$song_dir" ]; then
        fail "output directory does not exist: ${song_dir}"
        return 1
    fi

    local files=()
    while IFS= read -r -d '' f; do
        files+=("$f")
    done < <(find "$song_dir" -maxdepth 1 -type f -name '*.wav' -print0)

    if [ "${#files[@]}" -eq 0 ]; then
        fail "no .wav outputs found in ${song_dir}"
        return 1
    fi

    pass "found ${#files[@]} output file(s) for ${song}"

    local ok=1
    for f in "${files[@]}"; do
        local size bytes duration uid
        bytes=$(stat -c %s "$f")
        if [ "$bytes" -lt 102400 ]; then
            fail "${f} is too small (${bytes} bytes, expected >100KB)"
            ok=0
            continue
        fi

        duration=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$f" 2>/dev/null || true)
        if [ -z "$duration" ]; then
            fail "could not determine duration for ${f}"
            ok=0
            continue
        fi

        # ffprobe prints e.g. 10.000000; ensure duration >= 10 seconds.
        local too_short=1
        if command -v bc >/dev/null 2>&1; then
            if [ "$(echo "$duration >= 10" | bc)" -eq 1 ]; then
                too_short=0
            fi
        else
            local dur_sec
            dur_sec=${duration%.*}
            if [ "$dur_sec" -ge 10 ]; then
                too_short=0
            fi
        fi
        if [ "$too_short" -eq 1 ]; then
            fail "${f} duration too short (${duration}s, expected >=10s)"
            ok=0
            continue
        fi

        uid=$(stat -c %u "$f")
        if [ "$uid" -ne 1000 ]; then
            fail "${f} owner uid=${uid}, expected 1000 (appuser)"
            ok=0
            continue
        fi

        pass "${f}: size=${bytes} bytes, duration=${duration}s, uid=${uid}"
    done

    if [ "$ok" -eq 1 ]; then
        pass "all output files for ${song} are valid"
        return 0
    fi
    return 1
}

run_test() {
    local name="$1"
    local payload="$2"
    local input_name="$3"

    log "=== ${name} ==="

    copy_input "$input_name"

    log "POST /api/separate"
    local resp song
    resp=$(post_separate "$payload")
    log "  response: ${resp}"

    local status
    status=$(json_value "status" "$resp")
    if [ "$status" != "queued" ]; then
        fail "${name}: job not queued (status=${status:-?}, response=${resp})"
        return 1
    fi

    song=$(json_value "song" "$resp")
    if [ -z "$song" ]; then
        fail "${name}: no song returned"
        return 1
    fi
    pass "${name}: job queued, song=${song}"

    if wait_for_job "$song"; then
        verify_output_files "$song"
    fi
}

main() {
    log "Onda E2E API test against ${API_URL}"

    # Sanity check: API reachable.
    if ! curl -s "${API_URL}/api/health" >/dev/null 2>&1; then
        log "WARN: API /api/health is not reachable (curl failed)"
    fi

    mkdir -p "$INPUT_DIR" "$OUTPUT_DIR"

    if ! generate_audio; then
        log "Aborting: could not generate test audio"
        exit 1
    fi

    local ts
    ts=$(date +%s)

    # Test 1: BS-Roformer (ViperX) via API.
    local viperx_name="test_viperx_${ts}"
    local viperx_payload
    viperx_payload=$(printf '{"input":"/input/%s.wav","viperx":true,"viperx_keep":"both"}' "$viperx_name")
    run_test "Test 1: BS-Roformer (viperx)" "$viperx_payload" "$viperx_name" || true

    # Test 2: Demucs via API.
    local demucs_name="test_demucs_${ts}"
    local demucs_payload
    demucs_payload=$(printf '{"input":"/input/%s.wav","demucs":true,"stem_model":"htdemucs","demucs_segment":5,"jobs":1}' "$demucs_name")
    run_test "Test 2: Demucs" "$demucs_payload" "$demucs_name" || true

    log "=== Summary ==="
    log "Passed: ${PASSED}"
    log "Failed: ${FAILED}"

    if [ "$FAILED" -gt 0 ]; then
        exit 1
    fi
    exit 0
}

main "$@"
