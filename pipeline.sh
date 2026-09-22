#!/usr/bin/env bash
# Onda Pipeline v2.8.0 — Modular step-based audio separation with chaining
#
# Usage:
#   pipeline.sh [flags] <input_audio>
#
# Chained mode (--steps JSON):
#   pipeline.sh --steps JSON <input_audio>
#   where JSON is an array of step objects, e.g.:
#   '[{"type":"vocal","model":"BS_Roformer_Viperx","stems":{"vocals":{"action":"route","target":"step:1"},"instrumental":{"action":"save"}}},{"type":"demucs","model":"htdemucs_ft","stems":{"drums":{"action":"save"},"bass":{"action":"save"},"other":{"action":"save"},"vocals":{"action":"save"}}}]'
#
# Flags:
#   --steps JSON          Chained mode: JSON array of step objects
#   --vocal-model PATH    Vocal model path (default: $MODELS_DIR/VR_Models/BS_Roformer_Viperx)
#   --vocal-type TYPE     Vocal model type: mdx | mdxnet | polarformer | roformer | auto (default: auto)
#   --vocal-keep WHAT     What to save: instrumental | vocals | both (default) | all | stem1,stem2,...
#   --viperx-model PATH   Same as --vocal-model (deprecated)
#   --viperx-keep WHAT    Same as --vocal-keep (deprecated)
#   --demucs-keep LIST    Stems to keep: drums,bass,other,vocals or all (default)
#   --stem-model NAME     Demucs stem model name (default: htdemucs_ft)
#   --pitch N             Semitones for rubberband (default: 0)
#   --output DIR          Output directory (default: $OUTPUT_DIR/<song_name>)
#   --device NAME         Inference device: cpu | cuda (default: cuda)
#   --shifts N            Demucs shift-averaging passes (default: 1)
#   --demucs-segment N    Demucs segment duration in seconds (default: 0 = auto)
#   --jobs N              Demucs parallel workers (default: 0 = auto)
#   --no-clean            Don't clean output dir (for chained invocations)
#   --input-from-step     Use existing file as input instead of original
#
#
# Examples:
#   pipeline.sh cancion.mp3                                    # full pipeline (vocal + demucs + rubberband)
#   pipeline.sh --pitch 2 cancion.wav                          # only rubberband pitch shift
#   pipeline.sh --vocal-keep instrumental cancion.mp3          # only instrumentals
#   pipeline.sh --vocal-keep bass,drums,guitar cancion.mp3     # keep named stems from a multi-stem model
#   pipeline.sh --vocal-keep all cancion.mp3                   # keep every stem the model produces
#   pipeline.sh --demucs-keep drums,bass cancion.mp3           # only drums + bass
#   pipeline.sh --steps '[...]' cancion.wav                    # chained steps

set -euo pipefail

# Ensure Python scripts flush stdout/stderr line-by-line even when stdout is
# not a tty, so live progress and the last error lines are visible immediately.
export PYTHONUNBUFFERED=1

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

# ── Data root ───────────────────────────────────
# All data paths (input, output, models, configs) resolve from a single root
# controlled by ONDA_DATA_DIR. The backend passes absolute paths already under
# this root, so no host-to-container translation is needed.
ONDA_DATA_DIR="${ONDA_DATA_DIR:-/app/data}"
INPUT_DIR="$ONDA_DATA_DIR/input"
OUTPUT_DIR="$ONDA_DATA_DIR/output"
MODELS_DIR="$ONDA_DATA_DIR/models"
CONFIG_DIR="$ONDA_DATA_DIR/config"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Path to the progress tracker helper (container first, repo-relative for tests).
PROGRESS_TRACKER="${PROGRESS_TRACKER:-/app/tools/progress_tracker.py}"
if [ ! -f "$PROGRESS_TRACKER" ]; then
    PROGRESS_TRACKER="${SCRIPT_DIR}/tools/progress_tracker.py"
fi

# ── Progress reporting ──────────────────────────
START_TIME=$(date +%s)
export START_TIME
export PIPELINE_START_TIME=$START_TIME
STATUS_FILE="${PIPELINE_STATUS_FILE:-$OUTPUT_DIR/pipeline_status.json}"
export STATUS_FILE
mkdir -p "$(dirname "$STATUS_FILE")"
rm -f "$STATUS_FILE"
rm -f "$STATUS_FILE.tracker.json"
CURRENT_STEP=""

VOCAL_MODEL_DISPLAY=""   # friendly name like "BS_Roformer_Viperx"
DEMUCS_MODEL_DISPLAY=""   # friendly name like "htdemucs_ft"

# Resolve a step name to its index in legacy mode.  Indices are assigned
# consecutively to the active steps so the weighted global progress is honest.
_step_name_to_idx() {
    local name="$1"
    case "$name" in
        vocal|starting)      echo "${_VOCAL_IDX:-0}" ;;
        demucs)              echo "${_DEMUCS_IDX:-0}" ;;
        rubberband|complete) echo "${_RUBBERBAND_IDX:-0}" ;;
        *)                   echo 0 ;;
    esac
}

# Single entry point for progress reporting.  ``step_progress`` is the progress
# of the current step in the 0-100 range; the tracker computes the global
# ``progress`` / ``overall_progress``, the ETA and the elapsed seconds.
_report_step() {
    local status="$1"
    local step_name="$2"
    local step_progress="$3"
    local now elapsed step_idx tracker_step_name
    now=$(date +%s)
    elapsed=$((now - START_TIME))

    if [ -n "${STEPS_CONFIG_FILE:-}" ]; then
        # Chained mode: the current step index is known from the main loop.
        step_idx="${CURRENT_STEP_INDEX:-0}"
        # Keep the original contract in --steps mode: ``step`` is an integer.
        tracker_step_name="$step_idx"
    else
        step_idx=$(_step_name_to_idx "$step_name")
        # Legacy mode: ``step`` is the human-readable step name.
        tracker_step_name="$step_name"
    fi

    # ``done`` always marks the last step as finished.
    if [ "$status" = "done" ]; then
        step_idx=$((TOTAL_STEPS - 1))
        if [ -n "${STEPS_CONFIG_FILE:-}" ]; then
            # Chained mode keeps the integer step contract.
            tracker_step_name="$step_idx"
        else
            # Legacy mode reports the human-readable completion marker.
            tracker_step_name="complete"
        fi
    fi

    python3 "$PROGRESS_TRACKER" update-step "$STATUS_FILE" "$step_idx" "$status" "$step_progress" "$elapsed" "$TOTAL_STEPS" "$tracker_step_name" >/dev/null 2>&1 || true
    _sync_per_song_status
}

# Convenience helpers used by the old code paths.
report_progress() {
    _report_step "$@"
}
# Report a step failure, persist its stderr log, and print the last lines.
# Args: step_name exit_code [stderr_log_file] [fallback_message]
report_step_failure() {
    local step_name="${1:-unknown}"
    local exit_code="${2:-1}"
    local stderr_file="${3:-}"
    local fallback_message="${4:-}"
    local failed_dir=""
    local persisted_stderr=""
    local error_message=""
    local last_lines=""

    # run_demucs_step stores its diagnostic log here so the ERR trap can
    # report the real stderr without printing the failure banner twice.
    if [ -z "${stderr_file}" ] && [ "${step_name}" = "demucs" ] && [ -n "${DEMUCS_STEP_LOG:-}" ] && [ -f "${DEMUCS_STEP_LOG}" ]; then
        stderr_file="${DEMUCS_STEP_LOG}"
    fi

    if [ -n "${OUTPUT:-}" ]; then
        failed_dir="${OUTPUT}/_failed_${step_name}"
        persisted_stderr="${failed_dir}/stderr.log"
        mkdir -p "${failed_dir}"
    fi

    if [ -n "${stderr_file}" ] && [ -f "${stderr_file}" ] && [ -s "${stderr_file}" ]; then
        if [ -n "${persisted_stderr}" ]; then
            cp "${stderr_file}" "${persisted_stderr}"
        fi
        last_lines=$(tail -n 20 "${stderr_file}" 2>/dev/null || true)
        error_message=$(tail -n 1 "${stderr_file}" 2>/dev/null | tr -d '\r\n' | head -c 200 || true)
    elif [ -n "${fallback_message}" ]; then
        error_message="${fallback_message}"
        last_lines="${fallback_message}"
        if [ -n "${persisted_stderr}" ]; then
            echo "${fallback_message}" > "${persisted_stderr}"
        fi
    fi

    if [ -z "${error_message}" ]; then
        error_message="Paso ${step_name} fallo con codigo de salida ${exit_code}"
    fi

    echo ""
    echo "❌ Paso ${step_name} fallo. Ultimas lineas:"
    if [ -n "${last_lines}" ]; then
        echo "${last_lines}" | sed 's/^/   /'
    else
        echo "   (stderr no disponible)"
    fi

    # Update status with failure details.
    python3 -c "
import json, os, sys
status_file = '${STATUS_FILE}'
error_message = sys.argv[1]
try:
    if os.path.exists(status_file):
        with open(status_file) as f:
            d = json.load(f)
    else:
        d = {}
except Exception:
    d = {}
d['status'] = 'failed'
d['step'] = '${step_name}'
d['error'] = error_message
d['exit_code'] = ${exit_code}
try:
    os.makedirs(os.path.dirname(status_file) or '.', exist_ok=True)
    with open(status_file, 'w') as f:
        json.dump(d, f)
except Exception:
    pass
" "${error_message}" 2>/dev/null || true
    _sync_per_song_status
}

trap 'report_step_failure "${CURRENT_STEP:-unknown}" $? "${CURRENT_STEP_LOG:-}"' ERR

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

# Return 0 if a PID exists and is actually running.
# ``kill -0`` returns success for zombie/defunct processes, so we also check
# the process state with ps: a leading Z means dead-but-not-reaped.
_process_is_alive() {
    local pid="${1:-}"
    [ -n "$pid" ] || return 1
    if ! kill -0 "$pid" 2>/dev/null; then
        return 1
    fi
    local proc_stat
    proc_stat=$(ps -o stat= -p "$pid" 2>/dev/null || echo "")
    case "$proc_stat" in
        Z*|z*) return 1 ;;
    esac
    return 0
}

# Helper: run a command while capturing per-step output for diagnostics.
# Usage: run_with_elapsed <command...>
run_with_elapsed() {
    # Preserve the outer EXIT trap so temp-dir cleanup still runs after this helper.
    local prev_exit_trap
    prev_exit_trap=$(trap -p EXIT)
    trap 'cleanup_legacy_temps' EXIT

    # Capture per-step output so failure reports can include the real stderr.
    # The log is also exposed to the backend via the _failed_<step>/stderr.log
    # copy performed by report_step_failure.
    local step_log="${CURRENT_STEP_LOG:-}"
    if [ -z "$step_log" ] && [ -n "${OUTPUT:-}" ]; then
        step_log="${OUTPUT}/_step_${CURRENT_STEP:-unknown}.log"
    fi
    local cmd_rc
    if [ -n "$step_log" ]; then
        mkdir -p "$(dirname "$step_log")"
        : > "$step_log"
        "$@" 2>&1 | tee -a "$step_log"
    else
        "$@"
    fi
    cmd_rc=$?
    eval "${prev_exit_trap:-trap - EXIT}"
    return $cmd_rc
}

# Mirror the shared status file into the per-song output directory so the
# backend can read output/<song>/pipeline_status.json instead of relying on
# the legacy shared file that gets overwritten by later jobs.
_sync_per_song_status() {
    if [ -n "${OUTPUT:-}" ] && [ -f "$STATUS_FILE" ]; then
        mkdir -p "$OUTPUT"
        cp -f "$STATUS_FILE" "$OUTPUT/pipeline_status.json" 2>/dev/null || true
    fi
}

# ═══════════════════════════════════════════════════════════
# Multi-step progress reporting (for --steps chaining mode)
# ═══════════════════════════════════════════════════════════

# Initialize the persistent step state file and reset the tracker so the
# chained run starts from a clean global progress of 0.
multi_step_init() {
    python3 - "$STEPS_CONFIG_FILE" "$STEPS_STATE_FILE" "$STATUS_FILE" <<'PYEOF'
import json, os, sys
config_file, state_file, status_file = sys.argv[1:4]

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

# Reset tracker state so overall progress starts from scratch.
for stale in (status_file + '.tracker.json', status_file + '.tracker.json.tmp'):
    try:
        os.remove(stale)
    except FileNotFoundError:
        pass
PYEOF
}

# Update progress for a specific step and refresh pipeline_status.json.
# The tracker is the only writer of progress/eta/elapsed fields.
multi_step_progress() {
    local step_status="$1"
    local step_idx="$2"
    local progress_val="$3"

    # Update the persistent step state file first.
    python3 - "$STEPS_STATE_FILE" "$step_idx" "$step_status" "$progress_val" <<'PYEOF'
import json, sys
state_file, step_idx, step_status, progress_val = sys.argv[1:5]
step_idx = int(step_idx)
progress_val = int(progress_val)
with open(state_file) as f:
    state = json.load(f)
if 0 <= step_idx < len(state['steps']):
    # Step progress is monotonic; a completed step stays at 100.
    state['steps'][step_idx]['progress'] = max(
        progress_val, state['steps'][step_idx].get('progress', 0)
    )
    state['steps'][step_idx]['status'] = step_status
with open(state_file, 'w') as f:
    json.dump(state, f)
PYEOF

    CURRENT_STEP_INDEX=$step_idx
    local step_name
    step_name=$(python3 -c "import json; steps=json.load(open('$STEPS_CONFIG_FILE')); print(steps[$step_idx].get('type',''))" 2>/dev/null || echo "")

    _report_step "$step_status" "$step_name" "$progress_val"

    # Keep the detailed steps array in the status file for the UI.
    python3 - "$STEPS_STATE_FILE" "$STATUS_FILE" <<'PYEOF'
import json, os, sys
state_file, status_file = sys.argv[1:3]
with open(state_file) as f:
    state = json.load(f)
with open(status_file) as f:
    data = json.load(f)
data['steps'] = state['steps']
tmp = status_file + '.tmp'
with open(tmp, 'w') as f:
    json.dump(data, f)
    f.flush()
    os.fsync(f.fileno())
os.replace(tmp, status_file)
PYEOF
    _sync_per_song_status
}

# Detect whether a vocal model directory contains a BS PolarFormer ONNX model.
# Heuristic: explicit --vocal-type polarformer, OR a YAML config with
# ``model.use_pope: True`` (the canonical PolarFormer flag), OR the directory
# or .onnx filename contains "polarformer".
#
# This must run before is_onnx_model_dir() so PolarFormer does not fall into
# the generic MDXNet ONNX path.
is_polarformer_model_dir() {
    local model_path="$1"
    local model_dir="$model_path"
    if [ -f "$model_path" ]; then
        model_dir="$(dirname "$model_path")"
    fi

    case "$VOCAL_TYPE" in
        polarformer) return 0 ;;
        mdx|mdxnet|roformer|scnet) return 1 ;;
    esac

    # YAML with the canonical PolarFormer flag.
    local yaml_file
    yaml_file=$(ls "${model_dir}"/*.yaml "${model_dir}"/*.yml 2>/dev/null | head -1 || true)
    if [ -n "$yaml_file" ]; then
        local has_pope
        has_pope=$(python3 - <<PY
import yaml, sys
try:
    cfg = yaml.load(open('${yaml_file}'), Loader=yaml.FullLoader)
    if cfg.get('model', {}).get('use_pope') is True:
        sys.exit(0)
except Exception:
    pass
sys.exit(1)
PY
        ) && return 0
    fi

    # Model directory or ONNX filename contains "polarformer".
    local base
    base=$(basename "$model_dir" | tr '[:upper:]' '[:lower:]')
    if [[ "$base" =~ polarformer ]]; then
        return 0
    fi
    local onnx_name
    onnx_name=$(ls "${model_dir}"/*.onnx 2>/dev/null | head -1 || true)
    if [ -n "$onnx_name" ]; then
        onnx_name=$(basename "$onnx_name" | tr '[:upper:]' '[:lower:]')
        if [[ "$onnx_name" =~ polarformer ]]; then
            return 0
        fi
    fi

    return 1
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
        polarformer|roformer) return 1 ;;
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
        polarformer) return 1 ;;
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
        polarformer) return 1 ;;
    esac

    if ls "${model_dir}"/*.onnx >/dev/null 2>&1; then
        return 0
    fi

    return 1
}

# Resolve the effective model config source for a model name.
# Priority:
#   1. User-saved YAML   (config/model_configs/<name>.yaml)
#   2. UVR-style JSON    (config/model_configs/<name>.json)
#   3. Model-shipped YAML in the model directory
#   4. Model-shipped JSON in the model directory
# Prints "<kind>:<path>" or nothing if no config is found.
_resolve_model_config_source() {
    local model_name="$1"
    local model_dir="${2:-}"
    local real_model_dir real_model_name
    real_model_dir=$(readlink -f "$model_dir" 2>/dev/null || echo "$model_dir")
    real_model_name=$(basename "$real_model_dir" 2>/dev/null || echo "$model_name")

    local d candidate
    for d in "${CONFIG_DIR}/model_configs" "${ONDA_DATA_DIR}/model_configs"; do
        for candidate in "${d}/${model_name}.yaml" "${d}/${real_model_name}.yaml"; do
            if [ -f "$candidate" ]; then
                echo "user_yaml:${candidate}"
                return 0
            fi
        done
    done

    for d in "${CONFIG_DIR}/model_configs" "${ONDA_DATA_DIR}/model_configs"; do
        for candidate in "${d}/${model_name}.json" "${d}/${real_model_name}.json"; do
            if [ -f "$candidate" ]; then
                echo "uvr_json:${candidate}"
                return 0
            fi
        done
    done

    if [ -n "$model_dir" ] && [ -d "$model_dir" ]; then
        for candidate in "${model_dir}/${model_name}.yaml" "${model_dir}/${real_model_name}.yaml"; do
            if [ -f "$candidate" ]; then
                echo "model_yaml:${candidate}"
                return 0
            fi
        done
        for candidate in "${model_dir}/${model_name}.json" "${model_dir}/${real_model_name}.json" "${model_dir}/model_config.json"; do
            if [ -f "$candidate" ]; then
                echo "model_json:${candidate}"
                return 0
            fi
        done
    fi

    return 0
}

# Read a value from a model config source (user YAML, UVR JSON or model config).
# Usage: _read_model_config_value <source> <yaml_path> <json_key> [default]
# yaml_path is a dot-separated path for YAML (e.g. "inference.dim_t").
_read_model_config_value() {
    local source="$1"
    local yaml_path="$2"
    local json_key="$3"
    local default_val="${4:-}"
    local path="${source#*:}"
    local kind="${source%%:*}"

    case "$kind" in
        user_yaml|model_yaml)
            python3 -c "import yaml; d=yaml.load(open('$path'), Loader=yaml.FullLoader); keys='$yaml_path'.split('.'); v=d;
for k in keys:
    v = v.get(k) if isinstance(v, dict) else None
print(v if v is not None else '')" 2>/dev/null || echo "$default_val"
            ;;
        uvr_json|model_json)
            python3 -c "import json,sys; v=json.load(open('$path')).get('$json_key'); print(v if v is not None else '')" 2>/dev/null || echo "$default_val"
            ;;
        *)
            echo "$default_val"
            ;;
    esac
}

# Read model.manifest.json defaults and export them as VOCAL_* variables.
# This is the final fallback when no user-saved or model-shipped config exists.
# It never overrides values already set by a higher-priority source.
_apply_manifest_flags() {
    local model_dir="$1"
    local manifest="${model_dir}/model.manifest.json"
    [ -f "$manifest" ] || return 0

    python3 - "$manifest" << 'PYEOF'
import json, os, sys
manifest = sys.argv[1]
try:
    with open(manifest) as f:
        data = json.load(f)
except Exception:
    sys.exit(0)

flags = data.get('flags', {})

def get_default(name):
    v = flags.get(name, {})
    if isinstance(v, dict):
        return v.get('default')
    return v

mapping = {
    'segment_size': 'VOCAL_DIM_T',
    'num_overlap': 'VOCAL_NUM_OVERLAP',
    'batch_size': 'VOCAL_BATCH_SIZE',
    'chunk_size': 'VOCAL_CHUNK_SIZE',
}
for flag_name, env_name in mapping.items():
    if os.environ.get(env_name):
        continue
    v = get_default(flag_name)
    if v is not None:
        print(f"export {env_name}={v}")
PYEOF
}

# Read user-saved model config (config/model_configs/<model>.yaml) or UVR-style
# JSON and derive RoFormer inference parameters. User YAML takes precedence,
# then JSON, then the model-shipped YAML, then model.manifest.json flags.
# segment_size maps directly to dim_t, overlap is a float converted to an
# integer overlap factor (1/overlap), and batch_size is used as-is when >0.
# This prevents training-style YAML values (e.g. dim_t=3105) from being used
# for inference.
_apply_roformer_model_config_overrides() {
    local model_dir="$1"
    local model_name
    model_name=$(basename "$model_dir")
    # Accept either a directory named after the model or a generic symlink
    # (e.g. /app/data/models/model). Resolve symlinks so the real basename is
    # used as a fallback lookup key.
    local real_model_dir
    real_model_dir=$(readlink -f "$model_dir" 2>/dev/null || echo "$model_dir")
    local real_model_name
    real_model_name=$(basename "$real_model_dir")

    local source
    source=$(_resolve_model_config_source "$model_name" "$model_dir")
    if [ -z "$source" ]; then
        # No user/model config file: fall back to manifest flags.
        eval "$(_apply_manifest_flags "$model_dir")"
        return 0
    fi

    local kind path
    kind="${source%%:*}"
    path="${source#*:}"

    case "$kind" in
        user_yaml)
            echo "   ℹ️  RoFormer user config override: ${path}"
            ;;
        uvr_json)
            echo "   ℹ️  RoFormer UVR JSON config override: ${path}"
            ;;
        model_yaml|model_json)
            echo "   ℹ️  RoFormer model config override: ${path}"
            ;;
    esac

    local seg overlap batch chunk
    seg=$(_read_model_config_value "$source" "inference.dim_t" "segment_size" "0")
    overlap=$(_read_model_config_value "$source" "inference.num_overlap" "num_overlap" "0")
    batch=$(_read_model_config_value "$source" "inference.batch_size" "batch_size" "0")
    chunk=$(_read_model_config_value "$source" "inference.chunk_size" "chunk_size" "0")

    # For model JSON overlap may be stored as a float; convert to integer factor.
    if [ "$kind" = "uvr_json" ] || [ "$kind" = "model_json" ]; then
        if [ -n "$overlap" ]; then
            overlap=$(python3 -c "import sys; v=float('$overlap'); print(int(round(1.0/v))) if v>0 else sys.exit(1)" 2>/dev/null || echo "")
        fi
    fi

    if [ -n "$seg" ] && [ "$seg" -gt 0 ] 2>/dev/null; then
        VOCAL_DIM_T="$seg"
    fi
    if [ -n "$overlap" ] && [ "$overlap" -gt 0 ] 2>/dev/null; then
        VOCAL_NUM_OVERLAP="$overlap"
    fi
    if [ -n "$batch" ] && [ "$batch" -gt 0 ] 2>/dev/null; then
        VOCAL_BATCH_SIZE="$batch"
    fi
    if [ -n "$chunk" ] && [ "$chunk" -ge 0 ] 2>/dev/null; then
        VOCAL_CHUNK_SIZE="$chunk"
    fi
}

# Resolve a bare model name to an absolute model directory under MODELS_DIR.
# If the input is already an existing path (file or directory), it is returned
# unchanged. The lookup searches the known model category roots for a matching
# directory or weight file. This is a defensive fallback so the pipeline also
# accepts model names, not only full paths.
resolve_model_path() {
    local name="$1"
    if [ -z "$name" ]; then
        return
    fi
    # Existing path: return as-is (directory or parent of a file).
    if [ -d "$name" ]; then
        echo "$name"
        return
    fi
    if [ -f "$name" ]; then
        dirname "$name"
        return
    fi

    local subdirs="VR_Models MDX_Net_Models RoFormer_Models Demucs_Models Demucs_ONNX"
    for sub in $subdirs; do
        local root="$MODELS_DIR/$sub"
        [ -d "$root" ] || continue

        # Match a model-specific subdirectory.
        local candidate_dir="$root/$name"
        if [ -d "$candidate_dir" ]; then
            for ext in ckpt pth onnx th safetensors; do
                if ls "$candidate_dir"/*.$ext >/dev/null 2>&1; then
                    echo "$candidate_dir"
                    return
                fi
            done
        fi

        # Match a weight file sitting directly in the category root.
        for ext in ckpt pth onnx th safetensors; do
            if [ -f "$root/$name.$ext" ]; then
                echo "$root"
                return
            fi
        done
    done
}

# Build a human-readable "not found" error for a model name. It lists the
# category roots that were searched and the model directories available in them.
model_not_found_error() {
    local name="$1"
    local label="$2"
    local msg="❌ ${label} model not found: ${name}"
    msg="${msg}",
    msg="${msg} searched under: $MODELS_DIR"
    local subdirs="VR_Models MDX_Net_Models RoFormer_Models Demucs_Models Demucs_ONNX"
    for sub in $subdirs; do
        local root="$MODELS_DIR/$sub"
        if [ -d "$root" ]; then
            local found
            found=$(ls -1 "$root" 2>/dev/null | head -20 | tr '\n' ' ')
            if [ -n "$found" ]; then
                msg="${msg}; ${sub}: ${found}"
            else
                msg="${msg}; ${sub}: (empty)"
            fi
        fi
    done
    echo "$msg"
}

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
        local resolved
        resolved=$(resolve_model_path "$model_path")
        if [ -n "$resolved" ] && [ -d "$resolved" ]; then
            model_dir="$resolved"
            model_path="$resolved"
        else
            model_not_found_error "$model_path" "Vocal" >&2
            exit 2
        fi
    fi

    if is_mdx_model_dir "$model_dir"; then
        if [ ! -f /app/inference_mdx.py ]; then
            echo "❌ inference_mdx.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Detected MDX-C vocal model"
        local model_name
        model_name=$(basename "$model_dir")
        local source
        source=$(_resolve_model_config_source "$model_name" "$model_dir")
        local mdx_overlap="8"
        local mdx_batch_size="1"
        if [ -n "$source" ]; then
            mdx_overlap=$(_read_model_config_value "$source" "inference.num_overlap" "num_overlap" "8")
            mdx_batch_size=$(_read_model_config_value "$source" "inference.batch_size" "batch_size" "1")
            local kind
            kind="${source%%:*}"
            case "$kind" in
                user_yaml) echo "   ℹ️  MDX-C user config override: ${source#*:}" ;;
                uvr_json)  echo "   ℹ️  MDX-C UVR JSON config override: ${source#*:}" ;;
                model_yaml|model_json) echo "   ℹ️  MDX-C model config override: ${source#*:}" ;;
            esac
        fi
        echo "   ℹ️  MDX-C effective params: overlap=${mdx_overlap}, batch_size=${mdx_batch_size}"
        run_with_elapsed python3 -u /app/inference_mdx.py \
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
        local model_name
        model_name=$(basename "$model_dir")
        local source
        source=$(_resolve_model_config_source "$model_name" "$model_dir")
        local scnet_config_arg=""
        local scnet_chunk_size="" scnet_num_overlap="" scnet_batch_size=""
        if [ -n "$source" ]; then
            local kind path
            kind="${source%%:*}"
            path="${source#*:}"
            case "$kind" in
                user_yaml) echo "   ℹ️  SCNet user config override: ${path}" ;;
                uvr_json)  echo "   ℹ️  SCNet UVR JSON config override: ${path}" ;;
                model_yaml|model_json) echo "   ℹ️  SCNet model config override: ${path}" ;;
            esac
            scnet_chunk_size=$(_read_model_config_value "$source" "inference.chunk_size" "chunk_size" "")
            scnet_num_overlap=$(_read_model_config_value "$source" "inference.num_overlap" "num_overlap" "")
            scnet_batch_size=$(_read_model_config_value "$source" "inference.batch_size" "batch_size" "")
            if [ "$kind" = "user_yaml" ] || [ "$kind" = "uvr_json" ]; then
                if [ -n "$scnet_chunk_size" ] || [ -n "$scnet_num_overlap" ] || [ -n "$scnet_batch_size" ]; then
                    local model_yaml
                    model_yaml=$(ls "${model_dir}"/*.yaml 2>/dev/null | head -1)
                    if [ -n "$model_yaml" ]; then
                        local merged_config
                        merged_config="${output_dir}/.scnet_config.yaml"
                        mkdir -p "${output_dir}"
                        python3 - "$model_yaml" "$scnet_chunk_size" "$scnet_num_overlap" "$scnet_batch_size" "$merged_config" << 'PYEOF'
import yaml, sys
src_path, chunk, overlap, batch, dst = sys.argv[1:6]
with open(src_path) as f:
    cfg = yaml.full_load(f)
if not isinstance(cfg.get('inference'), dict):
    cfg['inference'] = {}
if chunk:
    cfg['inference']['chunk_size'] = int(float(chunk))
if overlap:
    cfg['inference']['num_overlap'] = int(float(overlap))
if batch:
    cfg['inference']['batch_size'] = int(float(batch))
with open(dst, 'w') as f:
    yaml.dump(cfg, f, default_flow_style=False)
PYEOF
                        scnet_config_arg="--config ${merged_config}"
                    fi
                fi
            fi
        fi
        # Effective values read by inference_scnet.py.
        local eff_config_path
        eff_config_path="${scnet_config_arg#--config }"
        if [ -z "$eff_config_path" ]; then
            eff_config_path=$(ls "${model_dir}"/*.yaml 2>/dev/null | head -1)
        fi
        local eff_chunk="?" eff_overlap="?" eff_batch="?"
        if [ -n "$eff_config_path" ] && [ -f "$eff_config_path" ]; then
            eff_chunk=$(python3 -c "import yaml; c=yaml.load(open('$eff_config_path'), Loader=yaml.FullLoader); print(c.get('inference',{}).get('chunk_size', c.get('audio',{}).get('chunk_size','?')))" 2>/dev/null || echo "?")
            eff_overlap=$(python3 -c "import yaml; c=yaml.load(open('$eff_config_path'), Loader=yaml.FullLoader); print(c.get('inference',{}).get('num_overlap','?'))" 2>/dev/null || echo "?")
            eff_batch=$(python3 -c "import yaml; c=yaml.load(open('$eff_config_path'), Loader=yaml.FullLoader); print(c.get('inference',{}).get('batch_size','?'))" 2>/dev/null || echo "?")
        fi
        echo "   ℹ️  SCNet effective params: chunk_size=${eff_chunk}, num_overlap=${eff_overlap}, batch_size=${eff_batch}"
        run_with_elapsed python3 -u /app/inference_scnet.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            ${scnet_config_arg} \
            "${model_dir}" "${input_file}" "${output_dir}"
    elif is_polarformer_model_dir "$model_dir"; then
        if [ ! -f /app/inference_polarformer.py ]; then
            echo "❌ inference_polarformer.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Detected BS PolarFormer ONNX vocal model"
        local model_name
        model_name=$(basename "$model_dir")
        local source
        source=$(_resolve_model_config_source "$model_name" "$model_dir")
        local pf_chunk_size="882000"
        local pf_num_overlap="2"
        local pf_batch_size="4"
        if [ -n "$source" ]; then
            pf_chunk_size=$(_read_model_config_value "$source" "inference.chunk_size" "chunk_size" "882000")
            pf_num_overlap=$(_read_model_config_value "$source" "inference.num_overlap" "num_overlap" "2")
            pf_batch_size=$(_read_model_config_value "$source" "inference.batch_size" "batch_size" "4")
            # UVR JSON stores overlap as a float factor; PolarFormer expects an integer.
            local kind
            kind="${source%%:*}"
            case "$kind" in
                user_yaml) echo "   ℹ️  PolarFormer user config override: ${source#*:}" ;;
                uvr_json)  echo "   ℹ️  PolarFormer UVR JSON config override: ${source#*:}" ;;
                model_yaml|model_json) echo "   ℹ️  PolarFormer model config override: ${source#*:}" ;;
            esac
            if [ "$kind" = "uvr_json" ] || [ "$kind" = "model_json" ]; then
                if [ -n "$pf_num_overlap" ]; then
                    pf_num_overlap=$(python3 -c "import sys; v=float('$pf_num_overlap'); print(int(round(1.0/v))) if v>0 else sys.exit(1)" 2>/dev/null || echo "2")
                fi
            fi
        fi
        echo "   ℹ️  PolarFormer effective params: chunk_size=${pf_chunk_size}, num_overlap=${pf_num_overlap}, batch_size=${pf_batch_size}"
        run_with_elapsed python3 -u /app/inference_polarformer.py \
            --pipeline-status "$STATUS_FILE" \
            --device "$DEVICE" \
            --chunk-size "${pf_chunk_size}" \
            --batch-size "${pf_batch_size}" \
            "${model_dir}" "${input_file}" "${output_dir}" "${pf_num_overlap}"
    elif is_onnx_model_dir "$model_dir"; then
        if [ ! -f /app/inference_onnx.py ]; then
            echo "❌ inference_onnx.py not found" >&2
            exit 2
        fi
        echo "   ℹ️  Detected MDXNet ONNX vocal model"
        local model_name
        model_name=$(basename "$model_dir")
        local source
        source=$(_resolve_model_config_source "$model_name" "$model_dir")
        local onnx_overlap="4"
        if [ -n "$source" ]; then
            onnx_overlap=$(_read_model_config_value "$source" "inference.num_overlap" "overlap" "4")
            # UVR JSON stores overlap as a float factor; inference_onnx.py expects a float.
            local kind
            kind="${source%%:*}"
            case "$kind" in
                user_yaml) echo "   ℹ️  MDXNet ONNX user config override: ${source#*:}" ;;
                uvr_json)  echo "   ℹ️  MDXNet ONNX UVR JSON config override: ${source#*:}" ;;
                model_yaml|model_json) echo "   ℹ️  MDXNet ONNX model config override: ${source#*:}" ;;
            esac
        fi
        echo "   ℹ️  MDXNet ONNX effective params: overlap=${onnx_overlap}"
        run_with_elapsed python3 -u /app/inference_onnx.py \
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

        # Apply user-saved/model config overrides so training YAML values
        # (e.g. dim_t=3105) do not leak into inference.
        _apply_roformer_model_config_overrides "$model_dir"
        local roformer_dim_t="${VOCAL_DIM_T:-}"
        local roformer_batch="${VOCAL_BATCH_SIZE:-}"
        local roformer_overlap="${VOCAL_NUM_OVERLAP:-${yaml_num_overlap}}"
        local extra_args=()
        if [ -n "$roformer_dim_t" ]; then
            extra_args+=("--dim-t" "$roformer_dim_t")
        fi
        if [ -n "$roformer_batch" ]; then
            extra_args+=("--batch-size" "$roformer_batch")
        fi

        # Pass effective chunk size to inference via environment (0 = whole song).
        # User-saved/model config takes precedence over the model-shipped YAML.
        local effective_chunk_size="${VOCAL_CHUNK_SIZE:-${yaml_chunk_size}}"
        ONDA_CHUNK_SIZE="${effective_chunk_size}" run_with_elapsed python3 -u /app/inference_universal.py \
            --pipeline-status "$STATUS_FILE" \
            --step-idx "${CURRENT_STEP_INDEX:-${_VOCAL_IDX:-0}}" \
            --total-steps "$TOTAL_STEPS" \
            "${extra_args[@]}" \
            "${model_dir}" "${input_file}" "${output_dir}" "${roformer_overlap}"
    fi
}

# Deprecated alias for backward compatibility
run_viperx_step() {
    run_vocal_step "$@"
}

# Apply fallback Demucs parameters from the effective model config source
# when the caller did not explicitly pass --shifts / --demucs-segment / --jobs.
# Priority: user YAML > UVR JSON > model-shipped YAML/JSON.
apply_demucs_fallback_config() {
    local model_name="${1:-htdemucs_ft}"

    if $SHIFTS_SET_EXPLICITLY && $DEMUCS_SEGMENT_SET_EXPLICITLY && $JOBS_SET_EXPLICITLY; then
        return 0
    fi

    # Demucs model YAMLs live one level under models/Demucs (e.g. Demucs_v4/htdemucs_ft.yaml).
    local model_yaml model_dir
    model_yaml=$(find "${MODELS_DIR}/Demucs_Models/models/Demucs" -maxdepth 2 -name "${model_name}.yaml" -print -quit 2>/dev/null || true)
    model_dir="${MODELS_DIR}/Demucs_Models/models/Demucs"
    if [ -n "$model_yaml" ]; then
        model_dir=$(dirname "$model_yaml")
    fi

    local source
    source=$(_resolve_model_config_source "$model_name" "$model_dir")
    [ -z "$source" ] && return 0

    local kind path
    kind="${source%%:*}"
    path="${source#*:}"

    case "$kind" in
        user_yaml)
            echo "   ℹ️  Demucs user config override: ${path}"
            ;;
        uvr_json)
            echo "   ℹ️  Demucs UVR JSON config override: ${path}"
            ;;
        model_yaml|model_json)
            echo "   ℹ️  Demucs model config override: ${path}"
            ;;
    esac

    if ! $SHIFTS_SET_EXPLICITLY; then
        SHIFTS=$(_read_model_config_value "$source" "demucs.shifts" "shifts" "1")
    fi
    if ! $DEMUCS_SEGMENT_SET_EXPLICITLY; then
        DEMUCS_SEGMENT=$(_read_model_config_value "$source" "demucs.segment" "segment" "0")
    fi
    if ! $JOBS_SET_EXPLICITLY; then
        JOBS=$(_read_model_config_value "$source" "demucs.jobs" "jobs" "0")
    fi

    # Ensure integer-looking values for the demucs worker CLI.
    SHIFTS=$(python3 -c "print(int(float('${SHIFTS:-1}')))" 2>/dev/null || echo "1")
    DEMUCS_SEGMENT=$(python3 -c "print(int(float('${DEMUCS_SEGMENT:-0}')))" 2>/dev/null || echo "0")
    JOBS=$(python3 -c "print(int(float('${JOBS:-0}')))" 2>/dev/null || echo "0")

    echo "   ℹ️  Demucs effective params: shifts=${SHIFTS}, segment=${DEMUCS_SEGMENT}, jobs=${JOBS}"
}

# Run a Demucs step using tools/demucs_worker.py (official demucs.api).
# Args: model_name, input_file, output_dir, [expected_stems_count], [step_index]
# If step_index is empty the legacy report_progress path is used; otherwise
# multi_step_progress is updated with real progress parsed from JSON events.
run_demucs_step() {
    local model_name="$1"
    local input_file="$2"
    local output_dir="$3"
    local expected_stems="${4:-4}"
    local step_idx="${5:-}"
    local worker_pid=""
    local prev_exit_trap
    prev_exit_trap=$(trap -p EXIT)

    # Defensive default: demucs always emits at least one progress bar.
    if [ "${expected_stems}" -le 0 ] 2>/dev/null; then
        expected_stems=4
    fi

    # Python interpreter: prefer the container venv, fall back to host python3.
    local PY="${PYTHON:-/opt/venv/bin/python3}"
    if [ ! -x "$PY" ]; then
        PY="python3"
    fi

    # Worker script: container path first, then repo-relative for host tests.
    local DEMUCS_WORKER="${DEMUCS_WORKER:-/app/tools/demucs_worker.py}"
    if [ ! -f "$DEMUCS_WORKER" ]; then
        DEMUCS_WORKER="${SCRIPT_DIR}/tools/demucs_worker.py"
    fi

    mkdir -p "${output_dir}"
    local events_file="${output_dir}/.demucs_events.jsonl"
    local step_log="${output_dir}/.demucs_worker.log"
    rm -f "${events_file}" "${step_log}"

    local worker_args=(
        "${DEMUCS_WORKER}"
        --model "${model_name}"
        --device "${DEVICE}"
        --input "${input_file}"
        --out "${output_dir}"
        --shifts "${SHIFTS:-1}"
        --segment "${DEMUCS_SEGMENT:-0}"
        --jobs "${JOBS:-0}"
    )

    # Launch worker with stdout/stderr redirected to files inside output_dir.
    # This avoids keeping the caller's pipe open, which previously caused EOF
    # to never arrive and the pipeline to hang forever.
    (
        exec "$PY" "${worker_args[@]}"
    ) > "${events_file}" 2> "${step_log}" &
    worker_pid=$!

    # Always clean up the worker when the function exits.
    # Use ${var:-} so set -u never aborts the trap before cleanup.
    trap 'kill_wait "${worker_pid:-}"; cleanup_legacy_temps' EXIT

    # Read JSON events as they arrive. Track line count so we only process new
    # events and detect silence (no new event for 120s -> abort).
    local lines_read=0
    local last_event_time
    last_event_time=$(date +%s)
    local last_progress=0
    local done_seen=false
    local silence_timeout=120

    local poll_interval=1
    while _process_is_alive "$worker_pid"; do
        local total_lines current_time elapsed_since_event
        total_lines=$(wc -l < "${events_file}" 2>/dev/null || echo 0)
        current_time=$(date +%s)
        elapsed_since_event=$((current_time - last_event_time))

        if [ "${total_lines}" -gt "${lines_read}" ]; then
            local line
            local new_events
            new_events=$(tail -n +$((lines_read + 1)) "${events_file}" 2>/dev/null)
            while IFS= read -r line; do
                # Skip empty lines.
                [ -z "$line" ] && continue
                # Parse the JSON event defensively with Python.
                local event_pct event_name
                event_name=$(python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('event',''))" "$line" 2>/dev/null || true)
                if [ "$event_name" = "progress" ]; then
                    event_pct=$(python3 -c "import json,sys; print(int(float(json.loads(sys.argv[1]).get('pct',0))))" "$line" 2>/dev/null || echo 0)
                    # Enforce monotonic progress — never go backwards.
                    if [ "${event_pct}" -lt "${last_progress}" ]; then
                        event_pct=${last_progress}
                    fi
                    last_progress=${event_pct}

                    _report_step "running" "demucs" "${event_pct}"
                elif [ "$event_name" = "done" ]; then
                    done_seen=true
                    _report_step "running" "demucs" 100
                elif [ "$event_name" = "error" ]; then
                    : # Worker will exit with a non-zero code; handled after wait.
                fi
                last_event_time=$(date +%s)
            done <<< "${new_events}"
            lines_read=${total_lines}
        fi

        # Silence detection: 120s without any new event means the worker is stuck.
        if [ "${elapsed_since_event}" -ge "${silence_timeout}" ]; then
            echo "⚠️  Demucs worker silent for ${silence_timeout}s, aborting..." >&2
            kill -INT "$worker_pid" 2>/dev/null || true
            # Give the worker a moment to shut down cleanly.
            sleep 1
            break
        fi

        sleep "${poll_interval}"
    done

    # Drain any events written between the last poll and worker exit.
    local total_lines
    total_lines=$(wc -l < "${events_file}" 2>/dev/null || echo 0)
    if [ "${total_lines}" -gt "${lines_read}" ]; then
        local remaining
        remaining=$(tail -n +$((lines_read + 1)) "${events_file}" 2>/dev/null)
        while IFS= read -r line; do
            [ -z "$line" ] && continue
            local event_name
            event_name=$(python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('event',''))" "$line" 2>/dev/null || true)
            [ "$event_name" = "done" ] && done_seen=true
        done <<< "${remaining}"
        lines_read=${total_lines}
    fi

    # Wait for the worker without set -e so we can inspect its exit code.
    local old_set_e=false
    case $- in *e*) old_set_e=true ;; esac
    set +e
    wait "$worker_pid"
    local worker_rc=$?
    if $old_set_e; then
        set -e
    fi

    # Restore the outer EXIT trap before returning.
    eval "${prev_exit_trap:-trap - EXIT}"

    # Final validation: success requires exit code 0, a 'done' event, and the
    # expected stems present on disk.
    local final_rc=${worker_rc}
    if [ "${final_rc}" -eq 0 ]; then
        if ! $done_seen; then
            final_rc=99
            echo "⚠️  Demucs worker exited 0 but no 'done' event was seen" >&2
        else
            local stem_count
            stem_count=$(find "${output_dir}" -maxdepth 3 -type f -iname "*.wav" 2>/dev/null | wc -l)
            if [ "${stem_count}" -lt "${expected_stems}" ]; then
                final_rc=40
                echo "⚠️  Demucs worker exited 0 but only ${stem_count}/${expected_stems} stems found" >&2
            fi
        fi
    fi

    if [ "${final_rc}" -ne 0 ]; then
        # Make the diagnostic log available to the ERR trap so it prints the
        # real stderr exactly once, instead of reporting again here.
        if [ ! -s "${step_log}" ]; then
            echo "Demucs worker died before completing the step (exit code ${final_rc})" >> "${step_log}"
        fi
        DEMUCS_STEP_LOG="${step_log}"
    else
        rm -f "${step_log}"
    fi

    return ${final_rc}
}


# ── Parse flags ──────────────────────────────────
VOCAL=false             # auto-detected: true when vocal-specific flags are passed
VOCAL_KEEP="both"
VOCAL_MODEL="$MODELS_DIR/VR_Models/BS_Roformer_Viperx"
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
        --vocal-model)  VOCAL_MODEL="$2"; VOCAL=true; shift 2 ;;
        --vocal-type)   VOCAL_TYPE="$2"; VOCAL=true; shift 2 ;;
        --vocal-keep)   VOCAL_KEEP="$2"; VOCAL=true; shift 2 ;;
        --viperx-model) VOCAL_MODEL="$2"; VOCAL=true; shift 2 ;;
        --viperx-keep)  VOCAL_KEEP="$2"; VOCAL=true; shift 2 ;;
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

# Make flags visible to report_progress without leaking unbound variables.
export SHIFTS DEMUCS_SEGMENT JOBS

# ── Helpers: GPU detection and naming ──
# Ask torch whether CUDA is usable; nvidia-smi alone is not enough, because the
# host may expose a GPU while the torch build in the container is CPU-only.
_query_torch_cuda() {
    python3 - <<'PYEOF'
import json, subprocess
try:
    import torch
    print(json.dumps({
        "available": torch.cuda.is_available(),
        "name": torch.cuda.get_device_name(0) if torch.cuda.is_available() else ""
    }))
except ImportError:
    # Torch no está instalado (p. ej. runner de tests en el host): usamos
    # nvidia-smi como fallback para no romper entornos de desarrollo.
    try:
        out = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=name", "--format=csv,noheader"],
            text=True, stderr=subprocess.DEVNULL
        )
        name = out.strip().split("\n")[0].strip()
        if name:
            print(json.dumps({"available": True, "name": name}))
        else:
            print(json.dumps({"available": False, "name": ""}))
    except Exception:
        print(json.dumps({"available": False, "name": ""}))
except Exception as e:
    print(json.dumps({"available": False, "name": "", "error": str(e)}))
PYEOF
}

_nvidia_smi_responds() {
    command -v nvidia-smi &>/dev/null && nvidia-smi &>/dev/null
}

# Write a failed status before exiting from the device guard, so the UI has
# something to read even when the pipeline never started.
_write_guard_failure_status() {
    local error_msg="$1"
    local exit_code="${2:-1}"
    python3 - "$error_msg" "$exit_code" "$STATUS_FILE" <<'PYEOF'
import json, os, sys
error_msg = sys.argv[1]
exit_code = int(sys.argv[2])
status_file = sys.argv[3]
d = {
    "status": "failed",
    "step": "device",
    "error": error_msg,
    "exit_code": exit_code,
    "device": "cpu",
    "gpu_type": "N/A"
}
try:
    os.makedirs(os.path.dirname(status_file), exist_ok=True)
    with open(status_file, 'w') as f:
        json.dump(d, f)
except Exception:
    pass
PYEOF
}

_detect_gpu_backend() {
    local torch_info available
    torch_info=$(_query_torch_cuda 2>/dev/null || echo '{"available":false,"name":""}')
    available=$(python3 -c "import json,sys; print('true' if json.loads(sys.argv[1]).get('available') else 'false')" "$torch_info" 2>/dev/null || echo false)
    if [ "$available" = "true" ]; then
        echo "cuda"
    else
        echo "cpu"
    fi
}

_gpu_name() {
    local torch_info name
    torch_info=$(_query_torch_cuda 2>/dev/null || echo '{"available":false,"name":""}')
    name=$(python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('name',''))" "$torch_info" 2>/dev/null || echo "")
    echo "$name"
}

# ── Validate requested/available device ──
DETECTED_DEVICE=$(_detect_gpu_backend)
GPU_NAME=$(_gpu_name)
if [ "$DEVICE" = "cpu" ]; then
    GPU_TYPE="N/A"
elif [ "$DETECTED_DEVICE" = "cuda" ] && [ -n "$GPU_NAME" ]; then
    GPU_TYPE="$GPU_NAME"
else
    GPU_TYPE="N/A"
fi
export DEVICE GPU_TYPE

SMI_BUT_NO_TORCH_CUDA=false
if [ "$DETECTED_DEVICE" != "cuda" ] && _nvidia_smi_responds; then
    SMI_BUT_NO_TORCH_CUDA=true
fi

if $DEVICE_SET_EXPLICITLY; then
    if [ "$DEVICE" = "cuda" ] && [ "$DETECTED_DEVICE" != "cuda" ]; then
        cause_msg="nvidia-smi no está disponible o no responde en este entorno."
        if [ "$SMI_BUT_NO_TORCH_CUDA" = "true" ]; then
            cause_msg="GPU presente según nvidia-smi, pero torch no ve CUDA (¿backend CUDA no montado o torch compilado solo para CPU?)."
        fi
        echo "❌ Error: se pidió --device cuda pero no hay GPU usable disponible." >&2
        echo "   Causa: $cause_msg" >&2
        echo "   Si realmente quieres ejecutar en CPU (LENTO: minutos en lugar de segundos), usa:" >&2
        echo "       --device cpu" >&2
        _write_guard_failure_status "se pidió --device cuda pero no hay GPU usable disponible. Causa: $cause_msg" 1
        exit 1
    fi
    if [ "$DEVICE" = "cpu" ]; then
        echo "⚠️  AVISO: el pipeline va a ejecutarse en CPU. Esto será LENTO (minutos en lugar de segundos)." >&2
    fi
    if [ "$DEVICE" = "cuda" ] && [ -n "$GPU_NAME" ]; then
        echo "   ✅ Usando dispositivo: cuda ($GPU_NAME)"
    fi
else
    # Auto-detect: never silently fall back to CPU.
    if [ "$DETECTED_DEVICE" != "cuda" ]; then
        echo "⚠️  Auto-detección: no se ha detectado GPU usable." >&2
        if [ "$SMI_BUT_NO_TORCH_CUDA" = "true" ]; then
            echo "   nvidia-smi ve una GPU, pero torch no está compilado con CUDA / no ve el backend CUDA." >&2
        fi
        echo "   El trabajo se ejecutaría en CPU, lo cual es LENTO (minutos en lugar de segundos)." >&2
        echo "   Para continuar en CPU de forma explícita, usa:" >&2
        echo "       --device cpu" >&2
        echo "   O bien establece ONDA_ALLOW_CPU=1 como variable de entorno." >&2
        if [ "${ONDA_ALLOW_CPU:-0}" != "1" ]; then
            echo "❌ Error: no se permite ejecutar en CPU sin decisión explícita." >&2
            _write_guard_failure_status "no se permite ejecutar en CPU sin decisión explícita" 1
            exit 1
        fi
        echo "   ⚠️  Continuando en CPU por decisión explícita (ONDA_ALLOW_CPU=1). Esto será LENTO." >&2
        DEVICE="cpu"
    else
        if [ -n "$GPU_NAME" ]; then
            echo "   ✅ Auto-detectado dispositivo: cuda ($GPU_NAME)"
        else
            echo "   ✅ Auto-detectado dispositivo: cuda"
        fi
        DEVICE="cuda"
    fi
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
OUTPUT="${OUTPUT:-$OUTPUT_DIR/${SONG}}"

# Ensure temporary vocal/demucs dirs are always removed, even on error or cancellation.
cleanup_legacy_temps() {
    if [ -n "${OUTPUT:-}" ]; then
        rm -rf "${OUTPUT}/_vocal" "${OUTPUT}/_demucs" 2>/dev/null || true
        rm -f "${OUTPUT}"/*.eta 2>/dev/null || true
    fi
    rm -f "${STATUS_FILE}.eta" 2>/dev/null || true
}
trap 'cleanup_legacy_temps' EXIT

# ── Auto-detect steps: if no step was explicitly requested and not in --steps mode,
#    enable all steps for backward compatibility (full pipeline).
if ! $VOCAL && ! $DEMUCS && ! $RUBBERBAND && [ -z "$STEPS_JSON" ]; then
    VOCAL=true
    DEMUCS=true
    RUBBERBAND=true
fi

# Resolve bare vocal model names to paths as a defensive fallback.
if $VOCAL; then
    _resolved_vocal=$(resolve_model_path "${VOCAL_MODEL}")
    if [ -n "$_resolved_vocal" ]; then
        VOCAL_MODEL="$_resolved_vocal"
    fi
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
                run_vocal_step "${STEP_MODEL:-$MODELS_DIR/VR_Models/BS_Roformer_Viperx}" "${CURRENT_INPUT}" "${STEP_TMP}"
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
                        run_with_elapsed rubberband --fine --pitch "${PITCH}" --quiet "${SRC}" "${STEP_TMP}/${stem_name}.wav"
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

    # Final progress report (before deleting step state)
    _report_step "done" "complete" 100

    # ── Final cleanup ──
    rm -rf "${ROUTED_DIR}" "${STEPS_STATE_FILE}" "${STEPS_CONFIG_FILE}" 2>/dev/null || true
    # Remove per-step diagnostic logs on success; keep them on failure.
    rm -f "${OUTPUT}"/_step_*.log 2>/dev/null || true
    # Remove any ETA sidecar files that should never be exposed to the user.
    rm -f "${STATUS_FILE}.eta" "${OUTPUT}"/*.eta 2>/dev/null || true

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

# ── Assign step indices and total step count for legacy mode ──
# Steps get consecutive indices so the global progress is always honest:
# when step N finishes, the global value is > 50% as long as step N+1 exists.
_VOCAL_IDX=""; _DEMUCS_IDX=""; _RUBBERBAND_IDX=""
TOTAL_STEPS=0
if $VOCAL; then
    _VOCAL_IDX=$TOTAL_STEPS
    TOTAL_STEPS=$((TOTAL_STEPS + 1))
fi
if $DEMUCS; then
    _DEMUCS_IDX=$TOTAL_STEPS
    TOTAL_STEPS=$((TOTAL_STEPS + 1))
fi
if $RUBBERBAND; then
    _RUBBERBAND_IDX=$TOTAL_STEPS
    TOTAL_STEPS=$((TOTAL_STEPS + 1))
fi
[ "$TOTAL_STEPS" -eq 0 ] && TOTAL_STEPS=1
export TOTAL_STEPS

# ── Model display names for status reporting ─────
VOCAL_MODEL_DISPLAY="${VOCAL_MODEL##*/}"    # strip path, keep filename
VOCAL_MODEL_DISPLAY="${VOCAL_MODEL_DISPLAY%.*}"  # strip extension
DEMUCS_MODEL_DISPLAY="$DEMUCS_MODEL"
export VOCAL_MODEL_DISPLAY DEMUCS_MODEL_DISPLAY

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
if $VOCAL; then
    MODEL_DIR="${VOCAL_MODEL}"
    if [ -d "$MODEL_DIR" ]; then
        VOCAL_YAML=$(ls "${MODEL_DIR}"/*.yaml 2>/dev/null | head -1)
        if [ -n "$VOCAL_YAML" ]; then
            VOCAL_DIM_T=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader)['inference']['dim_t'])" 2>/dev/null || echo "")
            VOCAL_NUM_OVERLAP=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader)['inference']['num_overlap'])" 2>/dev/null || echo "")
            VOCAL_BATCH_SIZE=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader)['inference']['batch_size'])" 2>/dev/null || echo "")
            VOCAL_CHUNK_SIZE=$(python3 -c "import yaml; print(yaml.load(open('$VOCAL_YAML'), Loader=yaml.FullLoader).get('inference',{}).get('chunk_size',0))" 2>/dev/null || echo "0")
            echo "   ℹ️  Model YAML: dim_t=${VOCAL_DIM_T}, overlap=${VOCAL_NUM_OVERLAP}, batch=${VOCAL_BATCH_SIZE}, chunk=${VOCAL_CHUNK_SIZE}"
        fi
        # User-saved/model config overrides YAML inference parameters.
        # This is the source of truth for UVR-style models and prevents using
        # training values such as dim_t=3105 at inference time.
        _apply_roformer_model_config_overrides "$MODEL_DIR"
        if [ -n "${VOCAL_DIM_T:-}" ]; then
            echo "   ℹ️  RoFormer effective inference params: dim_t=${VOCAL_DIM_T}, overlap=${VOCAL_NUM_OVERLAP:-}, batch=${VOCAL_BATCH_SIZE:-}, chunk=${ONDA_CHUNK_SIZE:-${VOCAL_CHUNK_SIZE:-0}}"
        fi
    fi
fi

# Export chunk size for RoFormer inference (0 = whole song)
export ONDA_CHUNK_SIZE="${VOCAL_CHUNK_SIZE:-0}"
export VOCAL_DIM_T VOCAL_NUM_OVERLAP VOCAL_BATCH_SIZE

# ── Smart defaults: Vocal model ya separa vocals, Demucs no necesita repetir ──
if $VOCAL && $DEMUCS && [ "${DEMUCS_KEEP}" = "all" ]; then
    DEMUCS_KEEP="drums,bass,other"
    echo "   ℹ️  Vocal model activo → Demucs vocals excluido (ya existe vocals)"
fi

echo "═══════════════════════════════════════"
echo "🎵 Onda Pipeline"
echo "   Input:    ${INPUT}"
echo "   Vocal:    ${VOCAL} (keep: ${VOCAL_KEEP})"
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
if $VOCAL; then
    echo ""
    echo "🔪 Vocal model → vocal + instrumental..."
    TMP_VOCAL="${OUTPUT}/_vocal"
    mkdir -p "${TMP_VOCAL}"  # must exist before progress file write
    CURRENT_STEP="vocal"
    _report_step "running" "vocal" 0
    # Pre-flight: verify model path exists (file or directory)
    vocal_model_dir="${VOCAL_MODEL}"
    if [ -f "${vocal_model_dir}" ]; then
        vocal_model_dir="$(dirname "${vocal_model_dir}")"
    fi
    if [ ! -d "${vocal_model_dir}" ]; then
        # Last-chance resolution and a detailed error message.
        _resolved_vocal=$(resolve_model_path "${VOCAL_MODEL}")
        if [ -n "$_resolved_vocal" ] && [ -d "$_resolved_vocal" ]; then
            vocal_model_dir="$_resolved_vocal"
            VOCAL_MODEL="$_resolved_vocal"
        else
            vocal_err=$(model_not_found_error "${VOCAL_MODEL}" "Vocal")
            echo "${vocal_err}" >&2
            report_step_failure "vocal" 2 "" "${vocal_err}"
            exit 2
        fi
    fi
    # Launch inference — Python writes pipeline_status.json directly on each chunk.
    # Effective num_overlap defaults to the model YAML; user/model config overrides
    # are applied inside run_vocal_step.
    VOCAL_OVERLAP_INT="${VOCAL_NUM_OVERLAP:-4}"

    # Delegate to the same step function used in chained mode so user/UVR/model
    # config precedence and effective-param logging is identical for all vocal
    # model types (MDX-C, SCNet, MDXNet ONNX, RoFormer).
    run_vocal_step "${vocal_model_dir}" "${INPUT}" "${TMP_VOCAL}" || {
        vocal_rc=$?
        echo "❌ Vocal step failed with exit code ${vocal_rc}" >&2
        report_step_failure "vocal" "${vocal_rc}" "${OUTPUT}/_step_${CURRENT_STEP:-unknown}.log"
        exit "${vocal_rc}"
    }
    echo "   ✅ Vocal model done"

    # Find instrumental (for demucs or for keep=all/list)
    INSTRUMENTAL=$(find "${TMP_VOCAL}" -maxdepth 1 -type f \( -iname "*instrumental*" -o -iname "*no_vocals*" \) | head -1)

    # Copy based on --vocal-keep flag.
    # Supports:
    #   both, vocals, instrumental       (legacy 2-stem models)
    #   all                              (keep every stem produced by the model)
    #   comma-separated list             (keep only the named stems)
    # The named-stem mode lets the API run N-stem RoFormer models (e.g. SW 6-stem)
    # without hardcoding stem names per model.
    _copy_vocal_keep_stems() {
        local keep_spec="$1"
        local src_dir="$2"
        local dst_dir="$3"
        local song="${4:-}"

        # Legacy 2-stem aliases are handled by the caller; this helper only
        # processes "all" and explicit comma-separated stem lists.
        case "${keep_spec}" in
            all)
                local f
                for f in "${src_dir}"/*.wav "${src_dir}"/*.flac; do
                    [ -e "$f" ] || continue
                    local base stem_name
                    base=$(basename "$f")
                    # RoFormer output is <song>_<stem>.wav; derive the stem name.
                    if [ -n "$song" ]; then
                        stem_name="${base#"${song}_"}"
                    else
                        stem_name="$base"
                    fi
                    stem_name="${stem_name%.*}"
                    # Skip the raw mix / input copy if present.
                    if [ "$stem_name" = "$song" ] || [ "$stem_name" = "$base" ]; then
                        continue
                    fi
                    cp "$f" "${dst_dir}/${stem_name}.wav"
                    echo "   ✅ ${stem_name} → ${dst_dir}/${stem_name}.wav"
                done
                ;;
            *,*)
                local IFS=','
                local stem
                for stem in ${keep_spec}; do
                    stem=$(echo "$stem" | tr -d '[:space:]')
                    [ -n "$stem" ] || continue
                    local src
                    src=$(find "${src_dir}" -maxdepth 1 -type f -iname "*${stem}*" | head -1)
                    if [ -n "$src" ]; then
                        cp "$src" "${dst_dir}/${stem}.wav"
                        echo "   ✅ ${stem} → ${dst_dir}/${stem}.wav"
                    else
                        echo "   ⚠️  Stem '${stem}' not found in vocal output"
                    fi
                done
                ;;
            *)
                # Single stem name without commas (e.g. "drums").
                local stem src
                stem=$(echo "$keep_spec" | tr -d '[:space:]')
                src=$(find "${src_dir}" -maxdepth 1 -type f -iname "*${stem}*" | head -1)
                if [ -n "$src" ]; then
                    cp "$src" "${dst_dir}/${stem}.wav"
                    echo "   ✅ ${stem} → ${dst_dir}/${stem}.wav"
                else
                    echo "   ⚠️  Stem '${stem}' not found in vocal output"
                fi
                ;;
        esac
    }

    case "${VOCAL_KEEP}" in
        both|vocals|instrumental)
            # Legacy behaviour for 2-stem vocal models.
            VOCAL_VOCAL=$(find "${TMP_VOCAL}" -maxdepth 1 -type f -iname "*vocal*" ! -iname "*instrumental*" | head -1)
            KEEP_VOCALS=false; KEEP_INST=false
            case "${VOCAL_KEEP}" in
                both)           KEEP_VOCALS=true; KEEP_INST=true ;;
                vocals)         KEEP_VOCALS=true ;;
                instrumental)   KEEP_INST=true ;;
            esac

            if $KEEP_VOCALS && [ -n "${VOCAL_VOCAL}" ]; then
                cp "${VOCAL_VOCAL}" "${OUTPUT}/vocals.wav"
                echo "   ✅ vocals → ${OUTPUT}/vocals.wav"
            elif [ -n "${VOCAL_VOCAL}" ]; then
                echo "   🗑️  vocals discarded (--vocal-keep ${VOCAL_KEEP})"
            fi
            if $KEEP_INST && [ -n "${INSTRUMENTAL}" ]; then
                cp "${INSTRUMENTAL}" "${OUTPUT}/instrumental.wav"
                echo "   ✅ instrumental → ${OUTPUT}/instrumental.wav"
            elif [ -n "${INSTRUMENTAL}" ]; then
                echo "   🗑️  instrumental discarded (--vocal-keep ${VOCAL_KEEP})"
            fi
            ;;
        *)
            # New behaviour: keep every stem or a named subset.
            _copy_vocal_keep_stems "${VOCAL_KEEP}" "${TMP_VOCAL}" "${OUTPUT}" "${SONG}"
            ;;
    esac

    # If demucs is off but rubberband is on, stems come from vocal dir
    if ! $DEMUCS && $RUBBERBAND; then
        STEM_DIR="${TMP_VOCAL}"
    fi
fi

# ══════════════════════════════════════════════════════
# STEP 2: Demucs stem model → drums, bass, other, vocals
# ══════════════════════════════════════════════════════
if $DEMUCS; then
    DEMUCS_INPUT="${INSTRUMENTAL:-${INPUT}}"
    echo ""
    echo "🥁 ${DEMUCS_MODEL} → drums, bass, other, vocals..."
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

    _report_step "running" "demucs" 0

    # Run Demucs and report real progress parsed from its stderr output
    # instead of counting output WAV files, which caused jumpy progress.
    run_demucs_step "$DEMUCS_MODEL" "${DEMUCS_INPUT}" "${TMP_DEM}" "$DEMUCS_EXPECTED"
    DEMUCS_RC=$?
    if [ $DEMUCS_RC -ne 0 ]; then
        echo "❌ Demucs failed with exit code $DEMUCS_RC" >&2
        exit $DEMUCS_RC
    fi

    echo "   ✅ ${DEMUCS_MODEL} done"

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
if $VOCAL && $DEMUCS; then
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
        # Stems from demucs or vocal model — apply rubberband to selected stems
        for stem in bass other vocals; do
            if [[ "${DEMUCS_KEEP}" == "all" ]] || [[ ",${DEMUCS_KEEP}," == *",${stem},"* ]]; then
                SRC=$(find "${STEM_DIR}" -maxdepth 1 -iname "*${stem}*" | head -1)
                if [ -n "${SRC}" ]; then
                    run_with_elapsed rubberband --fine --pitch "${PITCH}" --quiet "${SRC}" "${OUTPUT}/${stem}.wav"
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
        run_with_elapsed rubberband --fine --pitch "${PITCH}" --quiet "${INPUT}" "${OUT_FILE}"
        echo "   ✅ pitch shift → ${OUT_FILE}"
    fi
fi

_report_step "done" "complete" 100

# ── Cleanup temps ────────────────────────────────
rm -rf "${OUTPUT}/_vocal" "${OUTPUT}/_demucs" 2>/dev/null || true
# Remove per-step diagnostic logs on success; keep them on failure.
rm -f "${OUTPUT}"/_step_*.log 2>/dev/null || true
# Remove any ETA sidecar files that should never be exposed to the user.
rm -f "${STATUS_FILE}.eta" "${OUTPUT}"/*.eta 2>/dev/null || true

echo ""
echo "═══════════════════════════════════════"
echo "✅ Pipeline complete!"
echo ""
ls -lh "${OUTPUT}"/*.wav 2>/dev/null | awk '{print "   " $NF " (" $5 ")"}' || true
echo "═══════════════════════════════════════"
