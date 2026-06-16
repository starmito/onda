#!/usr/bin/env bash
# Onda Pipeline v2.8.0 — Modular step-based audio separation with chaining
#
# Usage:
#   pipeline.sh [flags] <input_audio>
#
# Chained mode (--steps JSON):
#   pipeline.sh --steps JSON <input_audio>
#   where JSON is an array of step objects, e.g.:
#   '[{"type":"viperx","model":"BS_Roformer_Viperx","stems":{"vocals":{"action":"route","target":"step:1"},"instrumental":{"action":"save"}}},{"type":"demucs","model":"htdemucs_ft","stems":{"drums":{"action":"save"},"bass":{"action":"save"},"other":{"action":"save"},"vocals":{"action":"save"}}}]'
#
# Flags:
#   --steps JSON          Chained mode: JSON array of step objects
#   --viperx-model PATH   ViperX model path (default: /models/VR_Models/BS_Roformer_Viperx)
#   --viperx-keep WHAT    What to save: instrumental | vocals | both (default)
#   --demucs-keep LIST    Stems to keep: drums,bass,other,vocals or all (default)
#   --stem-model NAME     Demucs stem model name (default: htdemucs_ft)
#   --pitch N             Semitones for rubberband (default: 0)
#   --output DIR          Output directory (default: /output/<song_name>)
#   --device NAME         Inference device: cpu | cuda (default: cuda)
#   --shifts N            Demucs shift-averaging passes (default: 1)
#   --demucs-segment N    Demucs segment duration in seconds (default: 0 = auto)
#   --jobs N              Demucs parallel workers (default: 0 = auto)
#   --no-clean            Don't clean output dir (for chained invocations)
#   --input-from-step     Use existing file as input instead of original
#
#
# Examples:
#   pipeline.sh cancion.mp3                                    # full pipeline (viperx + demucs + rubberband)
#   pipeline.sh --pitch 2 cancion.wav                          # only rubberband pitch shift
#   pipeline.sh --viperx-keep instrumental cancion.mp3         # only instrumentals
#   pipeline.sh --demucs-keep drums,bass cancion.mp3           # only drums + bass
#   pipeline.sh --steps '[...]' cancion.wav                    # chained steps

set -euo pipefail

# ── Docker container ────────────────────────────
ONDA_CONTAINER="onda"

# ── Path conversion for Docker ──────────────────
# pipeline.sh runs on the HOST and receives host paths (e.g. /home/.../onda/input/file.wav).
# Docker exec commands run INSIDE the container and need container paths
# because the bind mounts are: ./input -> /input, ./output -> /output.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
to_container() {
    local p="$1"
    # Normalize relative paths to absolute so prefix matching works
    [[ "$p" != /* ]] && p="${SCRIPT_DIR}/${p}"
    # Strip the host input dir prefix
    if [[ "$p" == "${SCRIPT_DIR}/input/"* ]]; then
        echo "/input/${p#${SCRIPT_DIR}/input/}"
    elif [[ "$p" == "${SCRIPT_DIR}/output/"* ]]; then
        echo "/output/${p#${SCRIPT_DIR}/output/}"
    else
        echo "$p"
    fi
}

# ── Progress reporting ──────────────────────────
START_TIME=$(date +%s)
LAST_ETA=""  # cap ETA so it never increases between steps
STATUS_FILE="${PIPELINE_STATUS_FILE:-/output/pipeline_status.json}"
rm -f "$STATUS_FILE"
CURRENT_STEP=""

VIPERX_MODEL_DISPLAY=""   # friendly name like "BS_Roformer_Viperx"
DEMUCS_MODEL_DISPLAY=""   # friendly name like "htdemucs_ft"

report_progress() {
    local status="$1"
    local step="$2"
    local progress="$3"
    local now elapsed eta progress_float
    now=$(date +%s)
    elapsed=$((now - START_TIME))
    eta=0
    if [ "$progress" -gt 0 ] && [ "$elapsed" -gt 0 ]; then
        new_eta=$(awk "BEGIN {printf \"%d\", int(($elapsed * (100 - $progress)) / $progress)}")
        # Don't let ETA increase — it should only decrease or stay stable
        if [ -z "$LAST_ETA" ] || [ "$new_eta" -lt "$LAST_ETA" ]; then
            eta=$new_eta
            LAST_ETA=$new_eta
        else
            eta=$LAST_ETA
        fi
    fi
    progress_float=$(awk "BEGIN {printf \"%.2f\", $progress/100}")
    cat > "$STATUS_FILE" << JSONEOF
{"status":"$status","step":"$step","progress":$progress_float,"song":"${SONG:-}","elapsed":$elapsed,"eta":$eta,"vocal_model":"${VIPERX_MODEL_DISPLAY:-}","stem_model":"${DEMUCS_MODEL_DISPLAY:-}","segment_size":${VIPERX_DIM_T:-0},"overlap":${VIPERX_NUM_OVERLAP:-0},"chunk_size":0,"batch_size":${VIPERX_BATCH_SIZE:-0},"device":"${DEVICE:-cpu}","shifts":${SHIFTS:-1},"demucs_segment":${DEMUCS_SEGMENT:-0},"jobs":${JOBS:-0}}
JSONEOF
}
trap 'report_progress "error" "${CURRENT_STEP:-unknown}" 0' ERR

# Clear stale pipeline status from previous run and signal that a new pipeline has started
report_progress "running" "starting" 0

# ── Background elapsed/eta updater ─────────────
# Runs in a subshell loop, updating elapsed and eta every second
# while a long-running docker exec is in progress.
update_elapsed_loop() {
    while true; do
        sleep 1
        if [ -f "$STATUS_FILE" ]; then
            now=$(date +%s)
            e=$((now - START_TIME))
            # Read current progress from status file
            prog=$(python3 -c "import json; print(json.load(open('$STATUS_FILE')).get('progress',0))" 2>/dev/null || echo 0)
            [ -z "$prog" ] && prog=0
            # Recalculate eta based on current progress
            new_eta=0
            if awk "BEGIN {exit !($prog > 0)}" && [ "$e" -gt 0 ]; then
                new_eta=$(awk "BEGIN {printf \"%d\", int(($e * (1 - $prog)) / $prog)}")
                # Don't let ETA increase — it should only decrease or stay stable
                if [ -z "$LOOP_LAST_ETA" ] || [ "$new_eta" -lt "$LOOP_LAST_ETA" ]; then
                    eta=$new_eta
                    LOOP_LAST_ETA=$new_eta
                else
                    eta=$LOOP_LAST_ETA
                fi
            fi
            # Update only elapsed and eta; preserve status, step, progress, song
            python3 -c "
import json
d=json.load(open('$STATUS_FILE'))
d['elapsed']=$e
d['eta']=${eta:-0}
json.dump(d, open('${STATUS_FILE}.tmp','w'))
" && mv "${STATUS_FILE}.tmp" "$STATUS_FILE"
        fi
    done
}

# Helper: run a command with elapsed/eta updates in background
# Usage: run_with_elapsed <command...>
run_with_elapsed() {
    update_elapsed_loop &
    local elapsed_pid=$!
    "$@"
    local cmd_rc=$?
    kill $elapsed_pid 2>/dev/null || true
    wait $elapsed_pid 2>/dev/null || true
    return $cmd_rc
}

# ═══════════════════════════════════════════════════════════
# Multi-step progress reporting (for --steps chaining mode)
# ═══════════════════════════════════════════════════════════

# Initialize multi-step progress tracking from the steps config file
# Reads from STEPS_CONFIG_FILE, writes to STEPS_STATE_FILE and pipeline_status.json
multi_step_init() {
    python3 << 'PYEOF'
import json, os, time

config_file = os.environ.get('STEPS_CONFIG_FILE', '')
state_file = os.environ.get('STEPS_STATE_FILE', '')
status_file = os.environ.get('STATUS_FILE', '')
song = os.environ.get('SONG', '')
start_time = int(os.environ.get('START_TIME', '0'))

with open(config_file) as f:
    steps = json.load(f)

state = {'steps': []}
for s in steps:
    state['steps'].append({
        'name': s.get('type', ''),
        'model': s.get('model', ''),
        'progress': 0,
        'status': 'waiting',
        'current_stems': list(s.get('stems', {}).keys())
    })

with open(state_file, 'w') as f:
    json.dump(state, f)

# Write initial pipeline_status.json
now = int(time.time())
elapsed = now - start_time
result = {
    'status': 'running',
    'song': song,
    'steps': state['steps'],
    'overall_progress': 0,
    'elapsed': elapsed,
    'eta': 0
}
with open(status_file, 'w') as f:
    json.dump(result, f)
PYEOF
}

# Update progress for a specific step and refresh pipeline_status.json
multi_step_progress() {
    local step_status="$1"
    local step_idx="$2"
    local progress_val="$3"

    export STEP_STATUS="$step_status" STEP_IDX="$step_idx" PROGRESS_VAL="$progress_val"

    python3 << 'PYEOF'
import json, os, time

state_file = os.environ.get('STEPS_STATE_FILE', '')
status_file = os.environ.get('STATUS_FILE', '')
song = os.environ.get('SONG', '')
start_time = int(os.environ.get('START_TIME', '0'))
last_eta_file = status_file + '.eta'

step_status = os.environ.get('STEP_STATUS', 'running')
step_idx = int(os.environ.get('STEP_IDX', '-1'))
progress_val = int(os.environ.get('PROGRESS_VAL', '0'))

with open(state_file) as f:
    state = json.load(f)

if 0 <= step_idx < len(state['steps']):
    state['steps'][step_idx]['progress'] = progress_val
    state['steps'][step_idx]['status'] = step_status

total = len(state['steps'])
overall = sum(s['progress'] for s in state['steps']) // max(total, 1)
state['overall_progress'] = overall

now = int(time.time())
elapsed = now - start_time

eta = 0
if overall > 0 and elapsed > 0:
    new_eta = int((elapsed * (100 - overall)) / overall)
    last_eta = 0
    try:
        with open(last_eta_file) as f:
            last_eta = int(f.read().strip())
    except Exception:
        pass
    if last_eta == 0 or new_eta < last_eta:
        eta = new_eta
        with open(last_eta_file, 'w') as f:
            f.write(str(eta))
    else:
        eta = last_eta

all_done = all(s['status'] in ('completed', 'done') for s in state['steps'])
has_error = any(s['status'] == 'error' for s in state['steps'])

if all_done:
    final_status = 'done'
elif has_error:
    final_status = 'error'
else:
    final_status = 'running'

result = {
    'status': final_status,
    'song': song,
    'steps': state['steps'],
    'overall_progress': overall,
    'elapsed': elapsed,
    'eta': eta
}

with open(status_file, 'w') as f:
    json.dump(result, f)
PYEOF
}

# Update elapsed/eta for multi-step mode (non-blocking background updater)
multi_step_elapsed_loop() {
    while true; do
        sleep 1
        if [ -f "$STATUS_FILE" ]; then
            python3 << 'PYEOF' 2>/dev/null || true
import json, os, time, shutil

status_file = os.environ.get('STATUS_FILE', '')
last_eta_file = status_file + '.eta'

with open(status_file) as f:
    d = json.load(f)

now = int(time.time())
start_time = int(os.environ.get('START_TIME', '0'))
elapsed = now - start_time
d['elapsed'] = elapsed

op = d.get('overall_progress', 0)
if op > 0 and elapsed > 0:
    new_eta = int((elapsed * (100 - op)) / op)
    last_eta = 0
    try:
        with open(last_eta_file) as f:
            last_eta = int(f.read().strip())
    except Exception:
        pass
    if last_eta == 0 or new_eta < last_eta:
        d['eta'] = new_eta
        with open(last_eta_file, 'w') as f:
            f.write(str(new_eta))
    else:
        d['eta'] = last_eta

with open(status_file + '.tmp', 'w') as f:
    json.dump(d, f)

shutil.move(status_file + '.tmp', status_file)
PYEOF
        fi
    done
}

# Run a ViperX step in chaining mode
# Args: model_path, input_file, output_dir
run_viperx_step() {
    local model_path="$1"
    local input_file="$2"
    local output_dir="$3"

    if [ ! -d "$model_path" ]; then
        echo "❌ ViperX model not found: ${model_path}" >&2
        exit 2
    fi
    if [ ! -f /app/inference_universal.py ]; then
        echo "❌ inference_universal.py not found" >&2
        exit 2
    fi

    # Read YAML params
    local yaml_num_overlap="4"
    local viperx_yaml
    viperx_yaml=$(ls "${model_path}"/*.yaml 2>/dev/null | head -1)
    if [ -n "$viperx_yaml" ]; then
        yaml_num_overlap=$(python3 -c "import yaml; print(yaml.load(open('$viperx_yaml'), Loader=yaml.FullLoader)['inference']['num_overlap'])" 2>/dev/null || echo "4")
    fi

    run_with_elapsed python3 /app/inference_universal.py \
        --pipeline-status "$STATUS_FILE" \
        "${model_path}" "${input_file}" "${output_dir}" "${yaml_num_overlap}"
}

# Run a Demucs step in chaining mode
# Args: model_name, input_file, output_dir, [expected_stems_count]
run_demucs_step() {
    local model_name="$1"
    local input_file="$2"
    local output_dir="$3"
    local expected_stems="${4:-4}"

    local demucs_args=(-n "${model_name}" --device "${DEVICE}" -o "${output_dir}")
    [ "${SHIFTS:-1}" -gt 0 ] && demucs_args+=(--shifts "${SHIFTS:-1}")
    [ "${DEMUCS_SEGMENT:-0}" -gt 0 ] && demucs_args+=(--segment "${DEMUCS_SEGMENT:-0}")
    [ "${JOBS:-0}" -gt 0 ] && demucs_args+=(-j "${JOBS:-0}")

    update_elapsed_loop &
    local elapsed_pid=$!
    demucs "${demucs_args[@]}" "${input_file}" &
    local demucs_pid=$!

    # Poll for stems appearing in output directory
    while kill -0 $demucs_pid 2>/dev/null; do
        if [ -d "${output_dir}" ]; then
            local found
            found=$(find "${output_dir}" -type f -name "*.wav" 2>/dev/null | wc -l)
            if [ "$found" -gt 0 ] && [ "$expected_stems" -gt 0 ]; then
                local step_pct=$(( found * 100 / expected_stems ))
                [ "$step_pct" -gt 100 ] && step_pct=100
                # Also update multi-step progress if in chained mode
                if [ -n "${STEPS_STATE_FILE:-}" ] && [ -f "$STEPS_STATE_FILE" ]; then
                    multi_step_progress "processing" "$CURRENT_STEP_INDEX" "$step_pct"
                fi
            fi
        fi
        sleep 2
    done
    wait $demucs_pid
    local demucs_rc=$?
    kill $elapsed_pid 2>/dev/null || true
    wait $elapsed_pid 2>/dev/null || true

    return $demucs_rc
}


# ── Parse flags ──────────────────────────────────
VIPERX=false           # auto-detected: true when viperx-specific flags are passed
VIPERX_KEEP="both"
VIPERX_MODEL="/models/VR_Models/BS_Roformer_Viperx"
DEMUCS=false           # auto-detected: true when demucs-specific flags are passed
DEMUCS_KEEP="all"
DEMUCS_MODEL="htdemucs_ft"
RUBBERBAND=false       # auto-detected: true when --pitch is passed
PITCH=0
OUTPUT=""
DEVICE="cuda"
DEVICE_SET_EXPLICITLY=false
SHIFTS=1
DEMUCS_SEGMENT=0
JOBS=0
NO_CLEAN=false        # v2.8.0: don't clean output dir between chained steps
INPUT_FROM_STEP=""    # v2.8.0: use this existing file as input instead of original
STEPS_JSON=""         # v2.8.0: JSON array of steps for single-invocation chaining

INPUT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --steps)        STEPS_JSON="$2"; shift 2 ;;
        --vocal-model)  VIPERX_MODEL="$2"; VIPERX=true; shift 2 ;;
        --viperx-keep)  VIPERX_KEEP="$2"; VIPERX=true; shift 2 ;;
        --viperx-model) VIPERX_MODEL="$2"; VIPERX=true; shift 2 ;;
        --demucs-keep)  DEMUCS_KEEP="$2"; DEMUCS=true; shift 2 ;;
        --stem-model)   DEMUCS_MODEL="$2"; DEMUCS=true; shift 2 ;;
        --pitch)        PITCH="$2"; RUBBERBAND=true; shift 2 ;;
        --output)       OUTPUT="$2"; shift 2 ;;
        --device)       DEVICE="$2"; DEVICE_SET_EXPLICITLY=true; shift 2 ;;
        --shifts)       SHIFTS="$2"; shift 2 ;;
        --demucs-segment) DEMUCS_SEGMENT="$2"; shift 2 ;;
        --jobs)         JOBS="$2"; shift 2 ;;
        --no-clean)     NO_CLEAN=true; shift ;;
        --input-from-step) INPUT_FROM_STEP="$2"; shift 2 ;;
        -*)             echo "Unknown flag: $1"; exit 1 ;;
        *)              INPUT="$1"; shift ;;
    esac
done

# ── Auto-detect device if not explicitly set ──
if ! $DEVICE_SET_EXPLICITLY; then
    DETECTED_DEVICE=$(/app/detect_gpu.sh 2>/dev/null || echo "cpu")
    echo "   ℹ️  Auto-detected device: ${DETECTED_DEVICE}"
    DEVICE="${DETECTED_DEVICE}"
fi

# Resolve input: --input-from-step overrides positional arg
if [ -n "$INPUT_FROM_STEP" ]; then
    INPUT="$INPUT_FROM_STEP"
fi

if [ -z "$INPUT" ]; then
    echo "Usage: pipeline.sh [--steps JSON] [--pitch N] <input>"
    exit 1
fi
if [ ! -f "$INPUT" ]; then
    echo "❌ File not found: $INPUT"
    exit 1
fi

SONG=$(basename "${INPUT%.*}")
OUTPUT="${OUTPUT:-/output/${SONG}}"

# ── Auto-detect steps: if no step was explicitly requested and not in --steps mode,
#    enable all steps for backward compatibility (full pipeline).
if ! $VIPERX && ! $DEMUCS && ! $RUBBERBAND && [ -z "$STEPS_JSON" ]; then
    VIPERX=true
    DEMUCS=true
    RUBBERBAND=true
fi

# ══════════════════════════════════════════════════════════
# CHAINED MODE (--steps JSON)
# Execute all steps in a single invocation with stem routing
# ══════════════════════════════════════════════════════════
if [ -n "$STEPS_JSON" ]; then

    # write steps config to file for safe Python access
    mkdir -p "${OUTPUT}"
    STEPS_CONFIG_FILE="${OUTPUT}/.steps_config.json"
    STEPS_STATE_FILE="${OUTPUT}/.steps_state.json"
    export STEPS_CONFIG_FILE STEPS_STATE_FILE STATUS_FILE SONG START_TIME OUTPUT
    export DEVICE SHIFTS DEMUCS_SEGMENT JOBS PITCH

    # Write steps JSON to config file
    cat > "$STEPS_CONFIG_FILE" <<< "$STEPS_JSON"

    # ── Validate steps JSON with Python ──
    python3 << 'PYEOF' > /dev/null 2>&1 && rc=0 || rc=$?
import json, sys, os
config_file = os.environ.get('STEPS_CONFIG_FILE', '')
try:
    with open(config_file) as f:
        steps = json.load(f)
    if not isinstance(steps, list) or len(steps) == 0:
        sys.exit(1)
    for s in steps:
        if 'type' not in s:
            sys.exit(1)
except Exception:
    sys.exit(1)
PYEOF

    if [ "$rc" -ne 0 ]; then
        echo "❌ Invalid --steps JSON: must be a non-empty array of step objects" >&2
        rm -f "$STEPS_CONFIG_FILE"
        exit 1
    fi

    echo "════════════════════════════════════════════════════"
    echo "🎵 Onda Pipeline v2.8.0 — Chained Steps Mode"
    echo "   Input:    ${INPUT}"
    echo "   Output:   ${OUTPUT}"
    echo "════════════════════════════════════════════════════"

    # Clean output dir (unless --no-clean)
    if ! $NO_CLEAN; then
        rm -rf "${OUTPUT}" 2>/dev/null || true
        mkdir -p "${OUTPUT}"
    fi

    # Re-create config/state files after possible cleanup
    cat > "$STEPS_CONFIG_FILE" <<< "$STEPS_JSON"

    # Routed files directory (intermediate stems passed between steps)
    export ROUTED_DIR="${OUTPUT}/_routed"
    mkdir -p "${ROUTED_DIR}"

    # ── Parse step count and initialize progress ──
    TOTAL_STEPS=$(python3 -c "
import json
with open('$STEPS_CONFIG_FILE') as f:
    steps = json.load(f)
print(len(steps))
" 2>/dev/null || echo 0)

    # Initialize multi-step progress tracking
    multi_step_init

    # ── Iterate through steps ──
    CURRENT_INPUT="$INPUT"
    CURRENT_STEP_INDEX=0

    for ((STEP_IDX=0; STEP_IDX<TOTAL_STEPS; STEP_IDX++)); do

        # Extract step config via Python (reading from config file)
        STEP_INFO=$(python3 -c "
import json
with open('$STEPS_CONFIG_FILE') as f:
    steps = json.load(f)
s = steps[$STEP_IDX]
print(s.get('type',''))
print(s.get('model',''))
stems = s.get('stems', {})
for k, v in stems.items():
    a = v.get('action', 'save')
    t = v.get('target', '')
    print('STEM|{}|{}|{}'.format(k, a, t))
print('ENDSTEMS')
" 2>/dev/null)

        STEP_TYPE=$(echo "$STEP_INFO" | sed -n '1p')
        STEP_MODEL=$(echo "$STEP_INFO" | sed -n '2p')

        if [ -z "$STEP_TYPE" ]; then
            echo "❌ Step ${STEP_IDX}: missing type" >&2
            exit 1
        fi

        CURRENT_STEP="${STEP_TYPE}"
        CURRENT_STEP_INDEX=$STEP_IDX

        echo ""
        echo "🔧 Step $((STEP_IDX+1))/${TOTAL_STEPS}: ${STEP_TYPE}${STEP_MODEL:+ (${STEP_MODEL})}"
        echo "   input: ${CURRENT_INPUT}"

        # Create step temp directory
        STEP_TMP="${OUTPUT}/_step_${STEP_IDX}"
        mkdir -p "${STEP_TMP}"

        # Mark step as processing
        multi_step_progress "processing" $STEP_IDX 0

        step_rc=0
        case "$STEP_TYPE" in
            viperx)
                run_viperx_step "${STEP_MODEL:-/models/VR_Models/BS_Roformer_Viperx}" "${CURRENT_INPUT}" "${STEP_TMP}"
                echo "   ✅ ${STEP_TYPE} done"
                ;;
            demucs)
                # Count expected stems from config (non-discard stems)
                STEM_COUNT=$(python3 -c "
import json
with open('$STEPS_CONFIG_FILE') as f:
    steps = json.load(f)
s = steps[$STEP_IDX]
stems = [k for k, v in s.get('stems', {}).items() if v.get('action') != 'discard']
print(len(stems))
" 2>/dev/null || echo 4)

                run_demucs_step "${STEP_MODEL:-htdemucs_ft}" "${CURRENT_INPUT}" "${STEP_TMP}" "$STEM_COUNT"
                step_rc=$?
                if [ $step_rc -ne 0 ]; then
                    echo "❌ Demucs failed with exit code $step_rc" >&2
                    exit $step_rc
                fi
                echo "   ✅ ${STEP_TYPE} done"
                ;;
            rubberband)
                # Find stems from parent step's temp dir
                PARENT_IDX=$((STEP_IDX-1))
                PARENT_TMP="${OUTPUT}/_step_${PARENT_IDX}"
                if [ ! -d "$PARENT_TMP" ]; then
                    PARENT_TMP="${OUTPUT}"
                fi

                # Parse stem names from config
                STEM_NAMES=$(python3 -c "
import json
with open('$STEPS_CONFIG_FILE') as f:
    steps = json.load(f)
s = steps[$STEP_IDX]
for k in s.get('stems', {}).keys():
    print(k)
" 2>/dev/null)

                while IFS= read -r stem_name; do
                    [ -z "$stem_name" ] && continue
                    # Skip drums — they get copied as-is (no pitch)
                    if [ "$stem_name" = "drums" ]; then
                        SRC=$(find "${PARENT_TMP}" -maxdepth 3 -iname "*drums*" -type f 2>/dev/null | head -1)
                        if [ -n "$SRC" ]; then
                            cp "$SRC" "${STEP_TMP}/drums.wav"
                            echo "   ✅ drums (no pitch) → ${STEP_TMP}/drums.wav"
                        fi
                        continue
                    fi
                    SRC=$(find "${PARENT_TMP}" -maxdepth 3 -iname "*${stem_name}*" -type f 2>/dev/null | head -1)
                    if [ -n "$SRC" ]; then
                        run_with_elapsed rubberband --pitch "${PITCH}" --quiet "${SRC}" "${STEP_TMP}/${stem_name}.wav"
                        echo "   ✅ ${stem_name} pitched → ${STEP_TMP}/${stem_name}.wav"
                    else
                        echo "   ⚠️  Stem '${stem_name}' not found for rubberband"
                    fi
                done <<< "$STEM_NAMES"
                echo "   ✅ ${STEP_TYPE} done"
                ;;
            *)
                echo "❌ Unknown step type: ${STEP_TYPE}" >&2
                exit 1
                ;;
        esac

        # ── Process stems (save / route / discard) ──
        # Parse stem routing from the step's config file
        STEM_ROUTING=$(python3 -c "
import json
with open('$STEPS_CONFIG_FILE') as f:
    steps = json.load(f)
s = steps[$STEP_IDX]
for k, v in s.get('stems', {}).items():
    a = v.get('action', 'save')
    t = v.get('target', '')
    print('{}|{}|{}'.format(k, a, t))
" 2>/dev/null)

        ROUTED_TO_NEXT=""
        while IFS= read -r stem_line; do
            [ -z "$stem_line" ] && continue
            IFS='|' read -r stem_name stem_action stem_target <<< "$stem_line"

            # Find the stem file in the step's temp dir
            STEM_FILE=$(find "${STEP_TMP}" -maxdepth 3 -iname "*${stem_name}*" -type f 2>/dev/null | head -1)

            if [ -z "$STEM_FILE" ]; then
                # Try finding in demucs output subdirectory (model-named dir)
                STEM_FILE=$(find "${STEP_TMP}" -maxdepth 4 -iname "*${stem_name}*.wav" -type f 2>/dev/null | head -1)
            fi

            if [ -z "$STEM_FILE" ] && [ "$stem_action" != "discard" ]; then
                echo "   ⚠️  Stem '${stem_name}' not found in step output"
                continue
            fi

            case "$stem_action" in
                route)
                    # Route to another step: copy to routed dir
                    routed_name="step_${STEP_IDX}_${stem_name}.wav"
                    cp "$STEM_FILE" "${ROUTED_DIR}/${routed_name}"
                    echo "   📍 ${stem_name} → route${stem_target:+ (→ ${stem_target})}"
                    # Check if routed to next step
                    NEXT_IDX=$((STEP_IDX+1))
                    if [ "$stem_target" = "step:${NEXT_IDX}" ] || [ "$stem_target" = "step:next" ] || { [ "$stem_target" = "step:demucs" ] && [ "$NEXT_IDX" -lt "$TOTAL_STEPS" ]; }; then
                        ROUTED_TO_NEXT="${ROUTED_DIR}/${routed_name}"
                    fi
                    ;;
                save)
                    cp "$STEM_FILE" "${OUTPUT}/${stem_name}.wav"
                    echo "   ✅ ${stem_name} → ${OUTPUT}/${stem_name}.wav"
                    ;;
                discard)
                    echo "   🗑️  ${stem_name} discarded"
                    ;;
                *)
                    # Default: save
                    cp "$STEM_FILE" "${OUTPUT}/${stem_name}.wav"
                    echo "   ✅ ${stem_name} → ${OUTPUT}/${stem_name}.wav"
                    ;;
            esac
        done <<< "$STEM_ROUTING"

        # ── Determine input for next step ──
        NEXT_IDX=$((STEP_IDX+1))
        if [ "$NEXT_IDX" -lt "$TOTAL_STEPS" ]; then
            if [ -n "$ROUTED_TO_NEXT" ] && [ -f "$ROUTED_TO_NEXT" ]; then
                CURRENT_INPUT="$ROUTED_TO_NEXT"
                echo "   🔗 Next step input ← routed stem: ${CURRENT_INPUT}"
            else
                echo "   ⚠️  No routed stem for step ${NEXT_IDX}, using original input"
                CURRENT_INPUT="$INPUT"
            fi
        fi

        # Mark step as completed
        multi_step_progress "completed" $STEP_IDX 100

        # ── Cleanup step temp ──
        rm -rf "$STEP_TMP" 2>/dev/null || true
    done

    # ── Final cleanup ──
    rm -rf "${ROUTED_DIR}" "${STEPS_STATE_FILE}" "${STEPS_CONFIG_FILE}" 2>/dev/null || true

    # Final progress report
    multi_step_progress "done" -1 100

    echo ""
    echo "════════════════════════════════════════════════════"
    echo "✅ Pipeline complete!"
    echo ""
    ls -lh "${OUTPUT}"/*.wav 2>/dev/null | awk '{print "   " $NF " (" $5 ")"}' || true
    echo "════════════════════════════════════════════════════"
    exit 0
fi

# ══════════════════════════════════════════════════════════
# LEGACY MODE (original behavior, no --steps)
# ══════════════════════════════════════════════════════════

# ── Progress ranges (dynamic based on active steps) ──
VIPERX_START=0; VIPERX_END=0
DEMUCS_START=0; DEMUCS_END=0
if $VIPERX && $DEMUCS; then
    VIPERX_START=0; VIPERX_END=65
    DEMUCS_START=65; DEMUCS_END=100
elif $VIPERX; then
    VIPERX_START=0; VIPERX_END=100
elif $DEMUCS; then
    DEMUCS_START=0; DEMUCS_END=100
fi

# ── Model display names for status reporting ─────
VIPERX_MODEL_DISPLAY="${VIPERX_MODEL##*/}"    # strip path, keep filename
VIPERX_MODEL_DISPLAY="${VIPERX_MODEL_DISPLAY%.*}"  # strip extension
DEMUCS_MODEL_DISPLAY="$DEMUCS_MODEL"

# ── Validate ─────────────────────────────────────
if [ ! -f "$INPUT" ]; then
    echo "❌ File not found: $INPUT"
    exit 1
fi

# ── Read model YAML for default inference parameters ──
VIPERX_DIM_T=""
VIPERX_NUM_OVERLAP=""
VIPERX_BATCH_SIZE=""
if $VIPERX && [ -d "${VIPERX_MODEL}" ]; then
    VIPERX_YAML=$(ls "${VIPERX_MODEL}"/*.yaml 2>/dev/null | head -1)
    if [ -n "$VIPERX_YAML" ]; then
        VIPERX_DIM_T=$(python3 -c "import yaml; print(yaml.load(open('$VIPERX_YAML'), Loader=yaml.FullLoader)['inference']['dim_t'])" 2>/dev/null || echo "")
        VIPERX_NUM_OVERLAP=$(python3 -c "import yaml; print(yaml.load(open('$VIPERX_YAML'), Loader=yaml.FullLoader)['inference']['num_overlap'])" 2>/dev/null || echo "")
        VIPERX_BATCH_SIZE=$(python3 -c "import yaml; print(yaml.load(open('$VIPERX_YAML'), Loader=yaml.FullLoader)['inference']['batch_size'])" 2>/dev/null || echo "")
        echo "   ℹ️  Model YAML: dim_t=${VIPERX_DIM_T}, overlap=${VIPERX_NUM_OVERLAP}, batch=${VIPERX_BATCH_SIZE}"
    fi
fi

# ── Smart defaults: ViperX ya separa vocals, Demucs no necesita repetir ──
if $VIPERX && $DEMUCS && [ "${DEMUCS_KEEP}" = "all" ]; then
    DEMUCS_KEEP="drums,bass,other"
    echo "   ℹ️  ViperX activo → Demucs vocals excluido (ya existe vocals_viperx)"
fi

echo "═══════════════════════════════════════"
echo "🎵 Onda Pipeline"
echo "   Input:    ${INPUT}"
echo "   Viperx:   ${VIPERX} (keep: ${VIPERX_KEEP})"
echo "   Demucs:   ${DEMUCS} (keep: ${DEMUCS_KEEP})"
echo "   Rubber:   ${RUBBERBAND} (pitch: ${PITCH})"
echo "   Output:   ${OUTPUT}"
echo "═══════════════════════════════════════"

# Clean previous run output (safe: pipeline runs as uid 1000, owns these dirs)
# Skip if --no-clean is set (v2.8.0 chaining mode)
if ! $NO_CLEAN; then
    rm -rf "${OUTPUT}" 2>/dev/null || true
    mkdir -p "${OUTPUT}"
fi

# Clean previous output to prevent accumulation of old stems
if ! $NO_CLEAN; then
    rm -f "${OUTPUT}"/*.wav 2>/dev/null || true
fi

# ── Track what's available for downstream steps ──
STEM_DIR=""        # dir with drums/bass/other/vocals for rubberband
INSTRUMENTAL=""    # .wav for demucs input

# ══════════════════════════════════════════════════════
# STEP 1: Viperx → vocal + instrumental
# ══════════════════════════════════════════════════════
if $VIPERX; then
    echo ""
    echo "🔪 Viperx → vocal + instrumental..."
    TMP_VIP="${OUTPUT}/_viperx"
    mkdir -p "${TMP_VIP}"  # must exist before progress file write
    CURRENT_STEP="viperx"
    report_progress "running" "viperx" 0
    # Pre-flight: verify model path exists
    if [ ! -d "${VIPERX_MODEL}" ]; then
        echo "❌ ViperX model not found: ${VIPERX_MODEL}" >&2
        exit 2
    fi
    if [ ! -f /app/inference_universal.py ]; then
        echo "❌ inference_universal.py not found" >&2
        exit 2
    fi
    # Launch inference — Python writes pipeline_status.json directly on each chunk.
    # inference_universal.py reads dim_t, num_overlap, batch_size from the model's YAML.
    # Pass num_overlap as positional arg for backward compatibility.
    VIPERX_OVERLAP_INT="${VIPERX_NUM_OVERLAP:-4}"
    # run_with_elapsed starts the background elapsed/eta updater loop.
    run_with_elapsed python3 /app/inference_universal.py \
        --pipeline-status "$STATUS_FILE" \
        "${VIPERX_MODEL}" "${INPUT}" "${TMP_VIP}" ${VIPERX_OVERLAP_INT}
    echo "   ✅ Viperx done"

    # Find instrumental (for demucs)
    INSTRUMENTAL=$(find "${TMP_VIP}" -maxdepth 1 \( -iname "*instrumental*" -o -iname "*no_vocals*" \) | head -1)

    # Copy based on --viperx-keep flag
    VIPERX_VOCAL=$(find "${TMP_VIP}" -maxdepth 1 -iname "*vocal*" ! -iname "*instrumental*" | head -1)
    KEEP_VOCALS=false; KEEP_INST=false
    case "${VIPERX_KEEP}" in
        both)           KEEP_VOCALS=true; KEEP_INST=true ;;
        vocals)         KEEP_VOCALS=true ;;
        instrumental)   KEEP_INST=true ;;
        *)              echo "   ⚠️  Invalid --viperx-keep value: ${VIPERX_KEEP} (use: instrumental|vocals|both)"; KEEP_VOCALS=true; KEEP_INST=true ;;
    esac

    if $KEEP_VOCALS && [ -n "${VIPERX_VOCAL}" ]; then
        cp "${VIPERX_VOCAL}" "${OUTPUT}/vocals_viperx.wav"
        echo "   ✅ vocals_viperx → ${OUTPUT}/vocals_viperx.wav"
    elif [ -n "${VIPERX_VOCAL}" ]; then
        echo "   🗑️  vocals discarded (--viperx-keep ${VIPERX_KEEP})"
    fi
    if $KEEP_INST && [ -n "${INSTRUMENTAL}" ]; then
        cp "${INSTRUMENTAL}" "${OUTPUT}/instrumental_viperx.wav"
        echo "   ✅ instrumental_viperx → ${OUTPUT}/instrumental_viperx.wav"
    elif [ -n "${INSTRUMENTAL}" ]; then
        echo "   🗑️  instrumental discarded (--viperx-keep ${VIPERX_KEEP})"
    fi

    # If demucs is off but rubberband is on, stems come from viperx dir
    if ! $DEMUCS && $RUBBERBAND; then
        STEM_DIR="${TMP_VIP}"
    fi
fi

# ══════════════════════════════════════════════════════
# STEP 2: HTDemucs_ft → drums, bass, other, vocals
# ══════════════════════════════════════════════════════
if $DEMUCS; then
    DEMUCS_INPUT="${INSTRUMENTAL:-${INPUT}}"
    echo ""
    echo "🥁 HTDemucs_ft → drums, bass, other, vocals..."
    echo "   input: ${DEMUCS_INPUT}"

    TMP_DEM="${OUTPUT}/_demucs"
    CURRENT_STEP="demucs"
    # Build demucs args with optional shift/segment/jobs flags
    DEMUCS_ARGS=(-n "${DEMUCS_MODEL}" --device "${DEVICE}" -o "${TMP_DEM}")
    [ "${SHIFTS}" -gt 0 ] && DEMUCS_ARGS+=(--shifts "${SHIFTS}")
    [ "${DEMUCS_SEGMENT}" -gt 0 ] && DEMUCS_ARGS+=(--segment "${DEMUCS_SEGMENT}")
    [ "${JOBS}" -gt 0 ] && DEMUCS_ARGS+=(-j "${JOBS}")

    # Calculate expected number of stems for progress tracking
    if [ "${DEMUCS_KEEP}" = "all" ]; then
        DEMUCS_EXPECTED=4
    else
        DEMUCS_EXPECTED=$(echo "${DEMUCS_KEEP}" | tr ',' '\n' | wc -l)
    fi

    report_progress "running" "demucs" $DEMUCS_START

    # Launch elapsed updater and demucs in background; track stem count as progress
    update_elapsed_loop &
    ELAPSED_PID=$!
    demucs "${DEMUCS_ARGS[@]}" "${DEMUCS_INPUT}" &
    DEMUCS_PID=$!

    # Poll for stems appearing in output directory
    while kill -0 $DEMUCS_PID 2>/dev/null; do
        if [ -d "${TMP_DEM}" ]; then
            found=$(find "${TMP_DEM}" -type f -name "*.wav" 2>/dev/null | wc -l)
            if [ "$found" -gt 0 ] && [ "$DEMUCS_EXPECTED" -gt 0 ]; then
                step_pct=$(( found * 100 / DEMUCS_EXPECTED ))
                [ "$step_pct" -gt 100 ] && step_pct=100
                global_pct=$(( DEMUCS_START + (step_pct * (DEMUCS_END - DEMUCS_START) / 100) ))
                report_progress "running" "demucs" $global_pct
            fi
        fi
        sleep 2
    done
    wait $DEMUCS_PID
    DEMUCS_RC=$?
    kill $ELAPSED_PID 2>/dev/null || true
    wait $ELAPSED_PID 2>/dev/null || true

    if [ $DEMUCS_RC -ne 0 ]; then
        echo "❌ Demucs failed with exit code $DEMUCS_RC" >&2
        exit $DEMUCS_RC
    fi

    report_progress "running" "demucs" $DEMUCS_END
    echo "   ✅ HTDemucs_ft done"

    # Find stem directory
    DEMUCS_OUT=$(find "${TMP_DEM}" -type d -name "${DEMUCS_MODEL}" | head -1)
    STEM_DIR=$(find "${DEMUCS_OUT}" -maxdepth 1 -type d ! -name "${DEMUCS_MODEL}" | head -1)
    STEM_DIR="${STEM_DIR:-${DEMUCS_OUT}}"

    # If rubberband is off, copy only selected stems to output
    if ! $RUBBERBAND; then
        for stem in drums bass other vocals; do
            if [[ "${DEMUCS_KEEP}" == "all" ]] || [[ ",${DEMUCS_KEEP}," == *",${stem},"* ]]; then
                SRC=$(find "${STEM_DIR}" -maxdepth 1 -iname "*${stem}*" | head -1)
                if [ -n "${SRC}" ]; then
                    cp "${SRC}" "${OUTPUT}/${stem}.wav"
                    echo "   ✅ ${stem} → ${OUTPUT}/${stem}.wav"
                fi
            else
                echo "   🗑️  ${stem} discarded (--demucs-keep ${DEMUCS_KEEP})"
            fi
        done
    fi
fi

# ── Limpiar instrumental_viperx si fue solo paso intermedio para Demucs ──
if $VIPERX && $DEMUCS; then
    rm -f "${OUTPUT}/instrumental_viperx.wav"
    echo "   🗑️  instrumental_viperx (intermedio, consumido por Demucs)"
fi

# ══════════════════════════════════════════════════════
# STEP 3: Rubberband → pitch shift (skip drums)
# ══════════════════════════════════════════════════════
if $RUBBERBAND; then
    echo ""
    echo "🎛️  Rubberband — pitch ${PITCH} semitones"

    if [ -n "${STEM_DIR}" ]; then
        CURRENT_STEP="rubberband"
        # Stems from demucs or viperx — apply rubberband to selected stems
        for stem in bass other vocals; do
            if [[ "${DEMUCS_KEEP}" == "all" ]] || [[ ",${DEMUCS_KEEP}," == *",${stem},"* ]]; then
                SRC=$(find "${STEM_DIR}" -maxdepth 1 -iname "*${stem}*" | head -1)
                if [ -n "${SRC}" ]; then
                    run_with_elapsed rubberband --pitch "${PITCH}" --quiet "${SRC}" "${OUTPUT}/${stem}.wav"
                    echo "   ✅ ${stem} → ${OUTPUT}/${stem}.wav"
                fi
            else
                echo "   🗑️  ${stem} discarded (--demucs-keep ${DEMUCS_KEEP})"
            fi
        done
        # Drums: copy as-is (no pitch) — only if selected
        if [[ "${DEMUCS_KEEP}" == "all" ]] || [[ ",${DEMUCS_KEEP}," == *",drums,"* ]]; then
            DRUMS=$(find "${STEM_DIR}" -maxdepth 1 -iname "*drums*" | head -1)
            if [ -n "${DRUMS}" ]; then
                cp "${DRUMS}" "${OUTPUT}/drums.wav"
                echo "   ✅ drums (no pitch) → ${OUTPUT}/drums.wav"
            fi
        else
            echo "   🗑️  drums discarded (--demucs-keep ${DEMUCS_KEEP})"
        fi
    else
        # No prior steps: apply rubberband directly to input
        # Only pitch if it's a mono/stereo track (not stems)
        OUT_FILE="${OUTPUT}/${SONG}_pitch${PITCH}.wav"
        CURRENT_STEP="rubberband"
        run_with_elapsed rubberband --pitch "${PITCH}" --quiet "${INPUT}" "${OUT_FILE}"
        echo "   ✅ pitch shift → ${OUT_FILE}"
    fi
fi

report_progress "done" "complete" 100

# ── Cleanup temps ────────────────────────────────
rm -rf "${OUTPUT}/_viperx" "${OUTPUT}/_demucs" 2>/dev/null || true

echo ""
echo "═══════════════════════════════════════"
echo "✅ Pipeline complete!"
echo ""
ls -lh "${OUTPUT}"/*.wav 2>/dev/null | awk '{print "   " $NF " (" $5 ")"}' || true
echo "═══════════════════════════════════════"
