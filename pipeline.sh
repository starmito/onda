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
#   --vocal-model PATH    Vocal model path (default: /app/models/VR_Models/BS_Roformer_Viperx)
#   --vocal-type TYPE     Vocal model type: mdx | mdxnet | roformer | auto (default: auto)
#   --vocal-keep WHAT     What to save: instrumental | vocals | both (default) (alias: --viperx-keep)
#   --viperx-model PATH   Same as --vocal-model (deprecated)
#   --viperx-keep WHAT    Same as --vocal-keep (deprecated)
#   --demucs-keep LIST    Stems to keep: drums,bass,other,vocals or all (default)
#   --stem-model NAME     Demucs stem model name (default: htdemucs_ft)
#   --pitch N             Semitones for rubberband (default: 0)
#   --output DIR          Output directory (default: /app/output/<song_name>)
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

# PYTHONPATH para docker exec (entrypoint no se ejecuta)
export PYTHONPATH="${PYTHONPATH:-}:/app/lib_v5"

# Detectar GPU y añadir backend si existe
DETECT_GPU=$(command -v detect_gpu.sh || echo '')
if [ -n "$DETECT_GPU" ]; then
    GPU_BACKEND=$($DETECT_GPU 2>/dev/null || echo 'cpu')
elif [ -f /app/detect_gpu.sh ]; then
    GPU_BACKEND=$(/app/detect_gpu.sh 2>/dev/null || echo 'cpu')
elif [ -f ./onda/detect_gpu.sh ]; then
    GPU_BACKEND=$(./onda/detect_gpu.sh 2>/dev/null || echo 'cpu')
else
    GPU_BACKEND='cpu'
fi

if [ "$GPU_BACKEND" != 'cpu' ] && [ -d "/opt/pytorch-backends/$GPU_BACKEND" ]; then
    export PYTHONPATH="$PYTHONPATH:/opt/pytorch-backends/$GPU_BACKEND"
fi

# ── Docker container ────────────────────────────
ONDA_CONTAINER="onda"

# ── Path conversion for Docker ──────────────────
# pipeline.sh runs on the HOST and receives host paths (e.g. /home/.../onda/input/file.wav).
# Docker exec commands run INSIDE the container and need container paths
# because the bind mounts are: ./input -> /app/input, ./output -> /app/output.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
to_container() {
    local p="$1"
    # Normalize relative paths to absolute so prefix matching works
    [[ "$p" != /* ]] && p="${SCRIPT_DIR}/${p}"
    # Strip the host input dir prefix
    if [[ "$p" == "${SCRIPT_DIR}/input/"* ]]; then
        echo "/app/input/${p#${SCRIPT_DIR}/input/}"
    elif [[ "$p" == "${SCRIPT_DIR}/output/"* ]]; then
        echo "/app/output/${p#${SCRIPT_DIR}/output/}"
    else
        echo "$p"
    fi
}

# ── Progress reporting ──────────────────────────
START_TIME=$(date +%s)
LAST_ETA=""  # cap ETA so it never increases between steps
STATUS_FILE="${PIPELINE_STATUS_FILE:-/app/output/pipeline_status.json}"
rm -f "$STATUS_FILE"
CURRENT_STEP=""

VOCAL_MODEL_DISPLAY=""   # friendly name like "BS_Roformer_Viperx"
VIPERX_MODEL_DISPLAY=""  # alias for backward compatibility
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
{"status":"$status","step":"$step","progress":$progress_float,"song":"${SONG:-}","elapsed":$elapsed,"eta":$eta,"vocal_model":"${VOCAL_MODEL_DISPLAY:-${VIPERX_MODEL_DISPLAY:-}}","stem_model":"${DEMUCS_MODEL_DISPLAY:-}","segment_size":${VIPERX_DIM_T:-0},"overlap":${VIPERX_NUM_OVERLAP:-0},"chunk_size":${ONDA_CHUNK_SIZE:-0},"batch_size":${VIPERX_BATCH_SIZE:-0},"device":"${DEVICE:-cpu}","gpu_type":"${GPU_TYPE:-unknown}","shifts":${SHIFTS:-1},"demucs_segment":${DEMUCS_SEGMENT:-0},"jobs":${JOBS:-0}}
JSONEOF
}
trap 'report_progress "error" "${CURRENT_STEP:-unknown}" 0' ERR
# Normalize rocm -> cuda immediately so DEVICE is always "cuda" in status reports
case "${DEVICE:-}" in
    rocm) DEVICE="cuda" ;;
esac

# Clear stale pipeline status from previous run and signal that a new pipeline has started
report_progress "running" "starting" 0

# ── Background elapsed/eta updater ─────────────
# Runs in a subshell loop, updating elapsed and eta every second
# while a long-running docker exec is in progress.
update_elapsed_loop() {
    local LOOP_LAST_ETA=""
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

# Helper: terminate a background PID and wait for it, with a forced kill fallback
# to avoid hanging if the process ignores SIGTERM.
kill_wait() {
    local pid="${1:-}"
    [ -n "$pid" ] || return 0
    if ! kill -0 "$pid" 2>/dev/null; then
        wait "$pid" 2>/dev/null || true
        return 0
    fi
    kill "$pid" 2>/dev/null || true
    local i
    for i in $(seq 1 30); do
        if ! kill -0 "$pid" 2>/dev/null; then
            wait "$pid" 2>/dev/null || true
            return 0
        fi
        sleep 0.1
    done
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
}

# Helper: run a command with elapsed/eta updates in background
# Usage: run_with_elapsed <command...>
run_with_elapsed() {
    update_elapsed_loop &
    local elapsed_pid=$!
    # Ensure the background loop is always cleaned up, even on failure or exit.
    # Use ${elapsed_pid:-} so set -u never aborts the trap before cleanup.
    trap 'kill_wait "${elapsed_pid:-}"' EXIT
    "$@"
    local cmd_rc=$?
    kill_wait "${elapsed_pid:-}"
    trap - EXIT
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

# Detect whether a vocal model directory contains an MDX-C (MDX-Net) model.
# Heuristic: explicit --vocal-type mdx, OR a YAML with MDX-C fields
# (num_scales / num_subbands), OR the checkpoint filename contains MDX23C.
#
# SCNet models are detected separately by is_scnet_model_dir().
is_mdx_model_dir() {
    local model_path="$1"
    local model_dir="$model_path"
    if [ -f "$model_path" ]; then
        model_dir="$(dirname "$model_path")"
    fi

    case "$VOCAL_TYPE" in
        mdx) return 0 ;;
        roformer) return 1 ;;
    esac

    # Explicit MDX checkpoint name.
    local ckpt_name
    ckpt_name=$(ls "${model_dir}"/*.ckpt 2>/dev/null | head -1 || true)
    if [ -n "$ckpt_name" ]; then
        ckpt_name="$(basename "$ckpt_name")"
        if [[ "${ckpt_name}" =~ [Mm][Dd][Xx]23[Cc] ]]; then
            return 0
        fi
    fi

    # YAML with MDX-C topology fields.
    local yaml_file
    yaml_file=$(ls "${model_dir}"/*.yaml 2>/dev/null | head -1 || true)
    if [ -n "$yaml_file" ]; then
        local has_mdx
        has_mdx=$(python3 - <<PY
import yaml, sys
try:
    cfg = yaml.load(open('${yaml_file}'), Loader=yaml.FullLoader)
    m = cfg.get('model', {})
    if any(k in m for k in ('num_scales', 'num_subbands', 'num_blocks_per_scale')):
        sys.exit(0)
except Exception:
    pass
sys.exit(1)
PY
        ) && return 0
    fi

    return 1
}

# Detect whether a vocal model directory contains an SCNet model.
# Heuristic: explicit --vocal-type scnet, OR a YAML with SCNet fields
# (band_SR / band_stride / band_kernel), OR the checkpoint filename contains scnet.
is_scnet_model_dir() {
    local model_path="$1"
    local model_dir="$model_path"
    if [ -f "$model_path" ]; then
        model_dir="$(dirname "$model_path")"
    fi

    case "$VOCAL_TYPE" in
        scnet) return 0 ;;
    esac

    # Explicit SCNet checkpoint name.
    local ckpt_name
    ckpt_name=$(ls "${model_dir}"/*.ckpt 2>/dev/null | head -1 || true)
    if [ -n "$ckpt_name" ]; then
        ckpt_name="$(basename "$ckpt_name")"
        if [[ "${ckpt_name}" =~ [Ss][Cc][Nn][Ee][Tt] ]]; then
            return 0
        fi
    fi

    # YAML with SCNet topology fields.
    local yaml_file
    yaml_file=$(ls "${model_dir}"/*.yaml 2>/dev/null | head -1 || true)
    if [ -n "$yaml_file" ]; then
        local has_scnet
        has_scnet=$(python3 - <<PY
import yaml, sys
try:
    cfg = yaml.load(open('${yaml_file}'), Loader=yaml.FullLoader)
    m = cfg.get('model', {})
    if any(k in m for k in ('band_SR', 'band_stride', 'band_kernel')):
        sys.exit(0)
    sources = [s.lower() for s in m.get('sources', [])]
    if 'drums' in sources and 'bass' in sources and 'vocals' in sources:
        sys.exit(0)
except Exception:
    pass
sys.exit(1)
PY
        ) && return 0
    fi

    return 1
}

# Detect whether a vocal model directory contains an MDXNet ONNX model.
# Heuristic: explicit --vocal-type mdxnet, OR the directory contains a .onnx file.
is_onnx_model_dir() {
    local model_path="$1"
    local model_dir="$model_path"
    if [ -f "$model_path" ]; then
        model_dir="$(dirname "$model_path")"
    fi

    case "$VOCAL_TYPE" in
        mdxnet) return 0 ;;
    esac

    if ls "${model_dir}"/*.onnx >/dev/null 2>&1; then
        return 0
    fi

    return 1
}

# Run a Vocal model step in chaining mode
# Args: model_path (file or dir), input_file, output_dir
run_vocal_step() {
    local model_path="$1"
    local input_file="$2"
    local output_dir="$3"

    # Find model directory: if model_path is a file, use its parent dir
    local model_dir="$model_path"
    if [ -f "$model_path" ]; then
        model_dir="$(dirname "$model_path")"
    fi

    if [ ! -d "$model_dir" ]; then
        echo "❌ Model not found: ${model_path}" >&2
        exit 2
    fi

    if is_mdx_model_dir "$model_dir"; then
        if [ ! -f /app/inference_mdx.py ]; then
            echo "❌ inference_mdx.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Detected MDX-C vocal model"
        local mdx_overlap="8"
        local mdx_batch_size="1"
        local mdx_yaml
        mdx_yaml=$(ls "${model_dir}"/*.yaml 2>/dev/null | head -1)
        if [ -n "$mdx_yaml" ]; then
            mdx_overlap=$(python3 -c "import yaml; print(yaml.load(open('$mdx_yaml'), Loader=yaml.FullLoader).get('inference',{}).get('num_overlap',8))" 2>/dev/null || echo "8")
            mdx_batch_size=$(python3 -c "import yaml; print(yaml.load(open('$mdx_yaml'), Loader=yaml.FullLoader).get('inference',{}).get('batch_size',1))" 2>/dev/null || echo "1")
        fi
        run_with_elapsed python3 /app/inference_mdx.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            --batch-size "${mdx_batch_size}" \
            "${model_dir}" "${input_file}" "${output_dir}" "${mdx_overlap}"
    elif is_scnet_model_dir "$model_dir"; then
        if [ ! -f /app/inference_scnet.py ]; then
            echo "❌ inference_scnet.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Detected SCNet vocal model"
        run_with_elapsed python3 /app/inference_scnet.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            "${model_dir}" "${input_file}" "${output_dir}"
    elif is_onnx_model_dir "$model_dir"; then
        if [ ! -f /app/inference_onnx.py ]; then
            echo "❌ inference_onnx.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Detected MDXNet ONNX vocal model"
        local onnx_overlap="4"
        local onnx_json
        onnx_json=$(ls "${model_dir}"/*.json 2>/dev/null | head -1)
        if [ -n "$onnx_json" ]; then
            onnx_overlap=$(python3 -c "import json; print(json.load(open('$onnx_json')).get('overlap',4))" 2>/dev/null || echo "4")
        fi
        run_with_elapsed python3 /app/inference_onnx.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            "${model_dir}" "${input_file}" "${output_dir}" "${onnx_overlap}"
    else
        if [ ! -f /app/inference_universal.py ]; then
            echo "❌ inference_universal.py not found" >&2
            exit 2
        fi
        # Read YAML params
        local yaml_num_overlap="4"
        local yaml_chunk_size="0"
        local vocal_yaml
        vocal_yaml=$(ls "${model_dir}"/*.yaml 2>/dev/null | head -1)
        if [ -n "$vocal_yaml" ]; then
            yaml_num_overlap=$(python3 -c "import yaml; print(yaml.load(open('$vocal_yaml'), Loader=yaml.FullLoader)['inference']['num_overlap'])" 2>/dev/null || echo "4")
            yaml_chunk_size=$(python3 -c "import yaml; print(yaml.load(open('$vocal_yaml'), Loader=yaml.FullLoader).get('inference',{}).get('chunk_size',0))" 2>/dev/null || echo "0")
        fi

        # Pass chunk size to inference via environment (0 = whole song)
        ONDA_CHUNK_SIZE="${yaml_chunk_size}" run_with_elapsed python3 /app/inference_universal.py \
            --pipeline-status "$STATUS_FILE" \
            "${model_dir}" "${input_file}" "${output_dir}" "${yaml_num_overlap}"
    fi
}

# Alias for backward compatibility
run_viperx_step() {
    run_vocal_step "$@"
}

# Apply fallback Demucs parameters from config/model_configs/<model>.yaml
# when the caller did not explicitly pass --shifts / --demucs-segment / --jobs.
# This keeps the pipeline aligned with values saved via the UI/API.
apply_demucs_fallback_config() {
    local model_name="${1:-htdemucs_ft}"

    if $SHIFTS_SET_EXPLICITLY && $DEMUCS_SEGMENT_SET_EXPLICITLY && $JOBS_SET_EXPLICITLY; then
        return 0
    fi

    local config_dir="${SCRIPT_DIR}/config/model_configs"
    if [ ! -d "$config_dir" ]; then
        config_dir="/app/config/model_configs"
    fi
    local yaml_file="${config_dir}/${model_name}.yaml"
    if [ ! -f "$yaml_file" ]; then
        return 0
    fi

    if ! $SHIFTS_SET_EXPLICITLY; then
        SHIFTS=$(python3 -c "import yaml; print(yaml.load(open('$yaml_file'), Loader=yaml.FullLoader).get('demucs',{}).get('shifts',1))" 2>/dev/null || echo "1")
    fi
    if ! $DEMUCS_SEGMENT_SET_EXPLICITLY; then
        DEMUCS_SEGMENT=$(python3 -c "import yaml; print(yaml.load(open('$yaml_file'), Loader=yaml.FullLoader).get('demucs',{}).get('segment',0))" 2>/dev/null || echo "0")
    fi
    if ! $JOBS_SET_EXPLICITLY; then
        JOBS=$(python3 -c "import yaml; print(yaml.load(open('$yaml_file'), Loader=yaml.FullLoader).get('demucs',{}).get('jobs',0))" 2>/dev/null || echo "0")
    fi
}

# Run a Demucs step (chaining or legacy mode).
# Args: model_name, input_file, output_dir, [expected_stems_count], [step_index]
# If step_index is empty the legacy report_progress path is used; otherwise
# multi_step_progress is updated with real progress parsed from demucs stderr.
run_demucs_step() {
    local model_name="$1"
    local input_file="$2"
    local output_dir="$3"
    local expected_stems="${4:-4}"
    local step_idx="${5:-}"
    local demucs_pid=""
    local elapsed_pid=""

    local demucs_args=(-n "${model_name}" --device "${DEVICE}" -o "${output_dir}")
    [ "${SHIFTS:-1}" -gt 0 ] && demucs_args+=(--shifts "${SHIFTS:-1}")
    if awk "BEGIN {exit !(${DEMUCS_SEGMENT:-0} > 0)}"; then
        demucs_args+=(--segment "${DEMUCS_SEGMENT:-0}")
    fi
    [ "${JOBS:-0}" -gt 0 ] && demucs_args+=(-j "${JOBS:-0}")

    mkdir -p "${output_dir}"
    local progress_log="${output_dir}/.demucs_progress.log"
    rm -f "${progress_log}"

    update_elapsed_loop &
    elapsed_pid=$!

    # demucs writes its tqdm progress bars to stderr.  Line-buffer stderr so
    # updates are available immediately in the log file instead of being fully
    # buffered until the process ends.  Fall back to plain demucs if stdbuf is
    # not available (progress will be less granular but still functional).
    if command -v stdbuf >/dev/null 2>&1; then
        stdbuf -eL demucs "${demucs_args[@]}" "${input_file}" 2> "${progress_log}" &
    else
        demucs "${demucs_args[@]}" "${input_file}" 2> "${progress_log}" &
    fi
    demucs_pid=$!

    # Always clean up both background processes when the function exits, even on
    # error, so the pipeline never hangs on a stray background loop.
    # Use ${var:-} so set -u never aborts the trap before cleanup.
    trap 'kill_wait "${demucs_pid:-}"; kill_wait "${elapsed_pid:-}"' EXIT

    # Poll the demucs stderr log for real progress percentages.
    while kill -0 "$demucs_pid" 2>/dev/null; do
        if [ -s "${progress_log}" ]; then
            local pct_line pct
            pct_line=$(tail -c 4096 "${progress_log}" | tr '\r' '\n' | grep -aE '^ *[0-9]+%' | tail -1)
            if [ -n "${pct_line}" ]; then
                pct=$(echo "${pct_line}" | LC_ALL=C sed -E 's/^ *([0-9]+)%.*/\1/')
                if [ -n "${pct}" ] && [ "${pct}" -gt 0 ] 2>/dev/null; then
                    if [ -n "${step_idx}" ]; then
                        multi_step_progress "processing" "${step_idx}" "${pct}"
                    else
                        local global_pct=$(( DEMUCS_START + (pct * (DEMUCS_END - DEMUCS_START) / 100) ))
                        [ "${global_pct}" -gt "${DEMUCS_END}" ] && global_pct=${DEMUCS_END}
                        [ "${global_pct}" -lt "${DEMUCS_START}" ] && global_pct=${DEMUCS_START}
                        report_progress "running" "demucs" "${global_pct}"
                    fi
                fi
            fi
        fi
        sleep 2
    done

    wait "$demucs_pid"
    local demucs_rc=$?

    # Clean up the elapsed updater explicitly before dropping the trap, so the
    # function can return the real demucs exit code without blocking.
    kill_wait "${elapsed_pid:-}"
    trap - EXIT
    rm -f "${progress_log}"

    return $demucs_rc
}


# ── Parse flags ──────────────────────────────────
VOCAL=false             # auto-detected: true when vocal-specific flags are passed
VIPERX=false            # alias for backward compatibility
VOCAL_KEEP="both"
VIPERX_KEEP="both"      # alias for backward compatibility
VOCAL_MODEL="/app/models/VR_Models/BS_Roformer_Viperx"
VIPERX_MODEL="/app/models/VR_Models/BS_Roformer_Viperx"  # alias for backward compatibility
VOCAL_TYPE="auto"       # mdx | roformer | auto
DEMUCS=false           # auto-detected: true when demucs-specific flags are passed
DEMUCS_KEEP="all"
DEMUCS_MODEL="htdemucs_ft"
RUBBERBAND=false       # auto-detected: true when --pitch is passed
PITCH=0
OUTPUT=""
DEVICE="cuda"
DEVICE_SET_EXPLICITLY=false
SHIFTS=1
SHIFTS_SET_EXPLICITLY=false
DEMUCS_SEGMENT=0
DEMUCS_SEGMENT_SET_EXPLICITLY=false
JOBS=0
JOBS_SET_EXPLICITLY=false
NO_CLEAN=false        # v2.8.0: don't clean output dir between chained steps
INPUT_FROM_STEP=""    # v2.8.0: use this existing file as input instead of original
STEPS_JSON=""         # v2.8.0: JSON array of steps for single-invocation chaining

INPUT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --steps)        STEPS_JSON="$2"; shift 2 ;;
        --vocal-model)  VOCAL_MODEL="$2"; VIPERX_MODEL="$2"; VOCAL=true; shift 2 ;;
        --vocal-type)   VOCAL_TYPE="$2"; VOCAL=true; shift 2 ;;
        --vocal-keep)   VOCAL_KEEP="$2"; VIPERX_KEEP="$2"; VOCAL=true; shift 2 ;;
        --viperx-model) VOCAL_MODEL="$2"; VIPERX_MODEL="$2"; VOCAL=true; shift 2 ;;
        --viperx-keep)  VOCAL_KEEP="$2"; VIPERX_KEEP="$2"; VOCAL=true; shift 2 ;;
        --demucs-keep)  DEMUCS_KEEP="$2"; DEMUCS=true; shift 2 ;;
        --stem-model)   DEMUCS_MODEL="$2"; DEMUCS=true; shift 2 ;;
        --pitch)        PITCH="$2"; RUBBERBAND=true; shift 2 ;;
        --output)       OUTPUT="$2"; shift 2 ;;
        --device)       DEVICE="$2"; DEVICE_SET_EXPLICITLY=true; shift 2 ;;
        --shifts)       SHIFTS="$2"; SHIFTS_SET_EXPLICITLY=true; shift 2 ;;
        --demucs-segment) DEMUCS_SEGMENT="$2"; DEMUCS_SEGMENT_SET_EXPLICITLY=true; shift 2 ;;
        --jobs)         JOBS="$2"; JOBS_SET_EXPLICITLY=true; shift 2 ;;
        --no-clean)     NO_CLEAN=true; shift ;;
        --input-from-step) INPUT_FROM_STEP="$2"; shift 2 ;;
        -*)             echo "Unknown flag: $1"; exit 1 ;;
        *)              INPUT="$1"; shift ;;
    esac
done

# ── Auto-detect device if not explicitly set ──
if ! $DEVICE_SET_EXPLICITLY; then
    DETECTED_DEVICE=$(detect_gpu.sh 2>/dev/null || echo "cpu")
    echo "   ℹ️  Auto-detected device: ${DETECTED_DEVICE}"
    DEVICE="${DETECTED_DEVICE}"
fi

# Capture real GPU type for status reporting (not normalized away)
GPU_TYPE=$(detect_gpu.sh 2>/dev/null || echo "unknown")

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
OUTPUT="${OUTPUT:-/app/output/${SONG}}"

# ── Auto-detect steps: if no step was explicitly requested and not in --steps mode,
#    enable all steps for backward compatibility (full pipeline).
if ! $VOCAL && ! $VIPERX && ! $DEMUCS && ! $RUBBERBAND && [ -z "$STEPS_JSON" ]; then
    VOCAL=true
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
            viperx|vocal)
                run_vocal_step "${STEP_MODEL:-/app/models/VR_Models/BS_Roformer_Viperx}" "${CURRENT_INPUT}" "${STEP_TMP}"
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

                apply_demucs_fallback_config "${STEP_MODEL:-htdemucs_ft}"
                run_demucs_step "${STEP_MODEL:-htdemucs_ft}" "${CURRENT_INPUT}" "${STEP_TMP}" "$STEM_COUNT" "$STEP_IDX"
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
VOCAL_START=0; VOCAL_END=0
DEMUCS_START=0; DEMUCS_END=0
if { $VOCAL || $VIPERX; } && $DEMUCS; then
    VOCAL_START=0; VOCAL_END=65
    VIPERX_START=0; VIPERX_END=65
    DEMUCS_START=65; DEMUCS_END=100
elif $VOCAL || $VIPERX; then
    VOCAL_START=0; VOCAL_END=100
    VIPERX_START=0; VIPERX_END=100
elif $DEMUCS; then
    DEMUCS_START=0; DEMUCS_END=100
fi

# ── Model display names for status reporting ─────
VOCAL_MODEL_DISPLAY="${VOCAL_MODEL##*/}"    # strip path, keep filename
VOCAL_MODEL_DISPLAY="${VOCAL_MODEL_DISPLAY%.*}"  # strip extension
VIPERX_MODEL_DISPLAY="${VIPERX_MODEL##*/}"    # alias for backward compat
VIPERX_MODEL_DISPLAY="${VIPERX_MODEL_DISPLAY%.*}"
DEMUCS_MODEL_DISPLAY="$DEMUCS_MODEL"

# ── Validate ─────────────────────────────────────
if [ ! -f "$INPUT" ]; then
    echo "❌ File not found: $INPUT"
    exit 1
fi

# ── Read model YAML for default inference parameters ──
VOCAL_DIM_T=""
VOCAL_NUM_OVERLAP=""
VOCAL_BATCH_SIZE=""
VOCAL_CHUNK_SIZE="0"
VIPERX_DIM_T=""    # alias for backward compat
VIPERX_NUM_OVERLAP=""
VIPERX_BATCH_SIZE=""
VIPERX_CHUNK_SIZE="0"
if $VOCAL || $VIPERX; then
    MODEL_DIR="${VOCAL_MODEL}"
    [ -z "$MODEL_DIR" ] && MODEL_DIR="${VIPERX_MODEL}"
    if [ -d "$MODEL_DIR" ]; then
        VOCAL_YAML=$(ls "${MODEL_DIR}"/*.yaml 2>/dev/null | head -1)
        if [ -n "$VOCAL_YAML" ]; then
            VOCAL_DIM_T=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader)['inference']['dim_t'])" 2>/dev/null || echo "")
            VOCAL_NUM_OVERLAP=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader)['inference']['num_overlap'])" 2>/dev/null || echo "")
            VOCAL_BATCH_SIZE=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader)['inference']['batch_size'])" 2>/dev/null || echo "")
            VOCAL_CHUNK_SIZE=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader).get('inference',{}).get('chunk_size',0))" 2>/dev/null || echo "0")
            VIPERX_DIM_T="${VOCAL_DIM_T}"
            VIPERX_NUM_OVERLAP="${VOCAL_NUM_OVERLAP}"
            VIPERX_BATCH_SIZE="${VOCAL_BATCH_SIZE}"
            VIPERX_CHUNK_SIZE="${VOCAL_CHUNK_SIZE}"
            echo "   ℹ️  Model YAML: dim_t=${VOCAL_DIM_T}, overlap=${VOCAL_NUM_OVERLAP}, batch=${VOCAL_BATCH_SIZE}, chunk=${VOCAL_CHUNK_SIZE}"
        fi
    fi
fi

# Export chunk size for RoFormer inference (0 = whole song)
export ONDA_CHUNK_SIZE="${VIPERX_CHUNK_SIZE:-${VOCAL_CHUNK_SIZE:-0}}"

# ── Smart defaults: Vocal model ya separa vocals, Demucs no necesita repetir ──
if { $VOCAL || $VIPERX; } && $DEMUCS && [ "${DEMUCS_KEEP}" = "all" ]; then
    DEMUCS_KEEP="drums,bass,other"
    echo "   ℹ️  Vocal model activo → Demucs vocals excluido (ya existe vocals)"
fi

echo "═══════════════════════════════════════"
echo "🎵 Onda Pipeline"
echo "   Input:    ${INPUT}"
echo "   Vocal:   ${VOCAL:-$VIPERX} (keep: ${VOCAL_KEEP:-$VIPERX_KEEP})"
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
# STEP 1: Vocal model → vocal + instrumental
# ══════════════════════════════════════════════════════
if $VOCAL || $VIPERX; then
    echo ""
    echo "🔪 Vocal model → vocal + instrumental..."
    TMP_VOCAL="${OUTPUT}/_vocal"
    TMP_VIP="${TMP_VOCAL}"  # alias for compat
    mkdir -p "${TMP_VOCAL}"  # must exist before progress file write
    CURRENT_STEP="vocal"
    report_progress "running" "vocal" 0
    # Pre-flight: verify model path exists (file or directory)
    vocal_model_dir="${VOCAL_MODEL:-${VIPERX_MODEL}}"
    if [ -f "${vocal_model_dir}" ]; then
        vocal_model_dir="$(dirname "${vocal_model_dir}")"
    fi
    if [ ! -d "${vocal_model_dir}" ]; then
        echo "❌ Vocal model not found: ${VOCAL_MODEL:-${VIPERX_MODEL}}" >&2
        exit 2
    fi
    # Launch inference — Python writes pipeline_status.json directly on each chunk.
    # Pass num_overlap as positional arg for backward compatibility.
    VOCAL_OVERLAP_INT="${VOCAL_NUM_OVERLAP:-${VIPERX_NUM_OVERLAP:-4}}"

    if is_mdx_model_dir "${vocal_model_dir}"; then
        if [ ! -f /app/inference_mdx.py ]; then
            echo "❌ inference_mdx.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Using MDX-C inference"
        VOCAL_OVERLAP_INT="${VOCAL_NUM_OVERLAP:-${VIPERX_NUM_OVERLAP:-8}}"
        VOCAL_BATCH_SIZE_INT="${VOCAL_BATCH_SIZE:-${VIPERX_BATCH_SIZE:-1}}"
        run_with_elapsed python3 /app/inference_mdx.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            --batch-size "${VOCAL_BATCH_SIZE_INT}" \
            "${vocal_model_dir}" "${INPUT}" "${TMP_VOCAL}" ${VOCAL_OVERLAP_INT}
    elif is_scnet_model_dir "${vocal_model_dir}"; then
        if [ ! -f /app/inference_scnet.py ]; then
            echo "❌ inference_scnet.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Using SCNet inference"
        run_with_elapsed python3 /app/inference_scnet.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            "${vocal_model_dir}" "${INPUT}" "${TMP_VOCAL}"
    elif is_onnx_model_dir "${vocal_model_dir}"; then
        if [ ! -f /app/inference_onnx.py ]; then
            echo "❌ inference_onnx.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Using MDXNet ONNX inference"
        onnx_overlap="4"
        onnx_json=$(ls "${vocal_model_dir}"/*.json 2>/dev/null | head -1)
        if [ -n "$onnx_json" ]; then
            onnx_overlap=$(python3 -c "import json; print(json.load(open('$onnx_json')).get('overlap',4))" 2>/dev/null || echo "4")
        fi
        run_with_elapsed python3 /app/inference_onnx.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            "${vocal_model_dir}" "${INPUT}" "${TMP_VOCAL}" "${onnx_overlap}"
    else
        if [ ! -f /app/inference_universal.py ]; then
            echo "❌ inference_universal.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Using RoFormer inference"
        run_with_elapsed python3 /app/inference_universal.py \
            --pipeline-status "$STATUS_FILE" \
            "${vocal_model_dir}" "${INPUT}" "${TMP_VOCAL}" ${VOCAL_OVERLAP_INT}
    fi
    echo "   ✅ Vocal model done"

    # Find instrumental (for demucs)
    INSTRUMENTAL=$(find "${TMP_VOCAL}" -maxdepth 1 -type f \( -iname "*instrumental*" -o -iname "*no_vocals*" \) | head -1)

    # Copy based on --vocal-keep flag
    VOCAL_VOCAL=$(find "${TMP_VOCAL}" -maxdepth 1 -type f -iname "*vocal*" ! -iname "*instrumental*" | head -1)
    KEEP_VOCALS=false; KEEP_INST=false
    case "${VOCAL_KEEP:-${VIPERX_KEEP}}" in
        both)           KEEP_VOCALS=true; KEEP_INST=true ;;
        vocals)         KEEP_VOCALS=true ;;
        instrumental)   KEEP_INST=true ;;
        *)              echo "   ⚠️  Invalid --vocal-keep value: ${VOCAL_KEEP:-${VIPERX_KEEP}} (use: instrumental|vocals|both)"; KEEP_VOCALS=true; KEEP_INST=true ;;
    esac

    if $KEEP_VOCALS && [ -n "${VOCAL_VOCAL}" ]; then
        cp "${VOCAL_VOCAL}" "${OUTPUT}/vocals.wav"
        echo "   ✅ vocals → ${OUTPUT}/vocals.wav"
    elif [ -n "${VOCAL_VOCAL}" ]; then
        echo "   🗑️  vocals discarded (--vocal-keep ${VOCAL_KEEP:-${VIPERX_KEEP}})"
    fi
    if $KEEP_INST && [ -n "${INSTRUMENTAL}" ]; then
        cp "${INSTRUMENTAL}" "${OUTPUT}/instrumental.wav"
        echo "   ✅ instrumental → ${OUTPUT}/instrumental.wav"
    elif [ -n "${INSTRUMENTAL}" ]; then
        echo "   🗑️  instrumental discarded (--vocal-keep ${VOCAL_KEEP:-${VIPERX_KEEP}})"
    fi

    # If demucs is off but rubberband is on, stems come from vocal dir
    if ! $DEMUCS && $RUBBERBAND; then
        STEM_DIR="${TMP_VOCAL}"
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
    apply_demucs_fallback_config "${DEMUCS_MODEL}"
    # Build demucs args with optional shift/segment/jobs flags
    DEMUCS_ARGS=(-n "${DEMUCS_MODEL}" --device "${DEVICE}" -o "${TMP_DEM}")
    [ "${SHIFTS}" -gt 0 ] && DEMUCS_ARGS+=(--shifts "${SHIFTS}")
    if awk "BEGIN {exit !(${DEMUCS_SEGMENT:-0} > 0)}"; then
        DEMUCS_ARGS+=(--segment "${DEMUCS_SEGMENT}")
    fi
    [ "${JOBS}" -gt 0 ] && DEMUCS_ARGS+=(-j "${JOBS}")

    # Calculate expected number of stems for progress tracking
    if [ "${DEMUCS_KEEP}" = "all" ]; then
        DEMUCS_EXPECTED=4
    else
        DEMUCS_EXPECTED=$(echo "${DEMUCS_KEEP}" | tr ',' '\n' | wc -l)
    fi

    report_progress "running" "demucs" $DEMUCS_START

    # Run Demucs and report real progress parsed from its stderr output
    # instead of counting output WAV files, which caused jumpy progress.
    run_demucs_step "$DEMUCS_MODEL" "${DEMUCS_INPUT}" "${TMP_DEM}" "$DEMUCS_EXPECTED"
    DEMUCS_RC=$?
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

# ── Clean up instrumental if it was only an intermediate step for Demucs ──
if { $VOCAL || $VIPERX; } && $DEMUCS; then
    rm -f "${OUTPUT}/instrumental.wav"
    echo "   🗑️  instrumental (intermedio, consumido por Demucs)"
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
rm -rf "${OUTPUT}/_vocal" "${OUTPUT}/_demucs" 2>/dev/null || true

echo ""
echo "═══════════════════════════════════════"
echo "✅ Pipeline complete!"
echo ""
ls -lh "${OUTPUT}"/*.wav 2>/dev/null | awk '{print "   " $NF " (" $5 ")"}' || true
echo "═══════════════════════════════════════"
