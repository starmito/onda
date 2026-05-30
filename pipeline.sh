#!/usr/bin/env bash
# Onda Pipeline — Modular step-based audio separation
#
# Usage:
#   pipeline.sh [flags] <input_audio>
#
# Flags (any combination):
#   --viperx              BS-Roformer-Viperx → vocal + instrumental
#   --viperx-keep WHAT    What to save: instrumental | vocals | both (default)
#   --viperx-model PATH   ViperX model path (default: /app/models/VR_Models/BS_Roformer_Viperx)
#   --demucs              HTDemucs_ft → drums, bass, other, vocals
#   --demucs-keep LIST    Stems to keep: drums,bass,other,vocals or all (default)
#   --demucs-model NAME   Demucs model name (default: htdemucs_ft)
#   --rubberband          Pitch shift all stems except drums
#   --pitch N             Semitones for rubberband (default: 0)
#   --output DIR          Output directory (default: /output/<song_name>)
#   --segment-size N      ViperX segment size (default: 256)
#   --overlap N           ViperX overlap ratio (default: 0.25)
#   --chunk-size N        Processing chunk size (default: 0 = auto)
#   --batch-size N        Processing batch size (default: 0 = auto)
#   --device NAME         Inference device: cpu | cuda (default: cuda)
#   --shifts N            Demucs shift-averaging passes (default: 1, paper uses 10)
#   --demucs-segment N    Demucs segment duration in seconds (default: 0 = auto)
#   --jobs N              Demucs parallel workers (default: 0 = auto)
#
# Default (no flags): --viperx --demucs --rubberband
#
# Examples:
#   pipeline.sh cancion.mp3                                    # full pipeline
#   pipeline.sh --rubberband --pitch 2 cancion.wav             # only pitch
#   pipeline.sh --viperx --viperx-keep instrumental cancion.mp3 # only instrumental
#   pipeline.sh --viperx --demucs cancion.mp3                  # vocals + stems
#   pipeline.sh --demucs --rubberband --pitch -1 song.wav      # stems + pitch

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
STATUS_FILE="/tmp/onda_pipeline_status.json"
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
{"status":"$status","step":"$step","progress":$progress_float,"song":"${SONG:-}","elapsed":$elapsed,"eta":$eta,"vocal_model":"$VIPERX_MODEL_DISPLAY","stem_model":"$DEMUCS_MODEL_DISPLAY","segment_size":$SEGMENT_SIZE,"overlap":$OVERLAP,"chunk_size":$CHUNK_SIZE,"batch_size":$BATCH_SIZE,"device":"$DEVICE","shifts":$SHIFTS,"demucs_segment":$DEMUCS_SEGMENT,"jobs":$JOBS}
JSONEOF
}
trap 'report_progress "error" "${CURRENT_STEP:-unknown}" 0' ERR

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
d['eta']=$eta
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


# ── Parse flags ──────────────────────────────────
VIPERX=false
VIPERX_KEEP="both"
VIPERX_MODEL="/app/models/VR_Models/BS_Roformer_Viperx"
DEMUCS=false
DEMUCS_KEEP="all"
DEMUCS_MODEL="htdemucs_ft"
RUBBERBAND=false
PITCH=0
OUTPUT=""
SEGMENT_SIZE=256
OVERLAP=0.25
CHUNK_SIZE=0
BATCH_SIZE=0
DEVICE="cuda"
SHIFTS=1
DEMUCS_SEGMENT=0
JOBS=0

INPUT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --viperx)       VIPERX=true; shift ;;
        --viperx-keep)  VIPERX_KEEP="$2"; shift 2 ;;
        --viperx-model) VIPERX_MODEL="$2"; shift 2 ;;
        --demucs)       DEMUCS=true; shift ;;
        --demucs-keep)  DEMUCS_KEEP="$2"; shift 2 ;;
        --demucs-model) DEMUCS_MODEL="$2"; shift 2 ;;
        --rubberband)   RUBBERBAND=true; shift ;;
        --pitch)        PITCH="$2"; shift 2 ;;
        --output)       OUTPUT="$2"; shift 2 ;;
        --segment-size) SEGMENT_SIZE="$2"; shift 2 ;;
        --overlap)      OVERLAP="$2"; shift 2 ;;
        --chunk-size)   CHUNK_SIZE="$2"; shift 2 ;;
        --batch-size)   BATCH_SIZE="$2"; shift 2 ;;
        --device)       DEVICE="$2"; shift 2 ;;
        --shifts)       SHIFTS="$2"; shift 2 ;;
        --demucs-segment) DEMUCS_SEGMENT="$2"; shift 2 ;;
        --jobs)         JOBS="$2"; shift 2 ;;
        -*)             echo "Unknown flag: $1"; exit 1 ;;
        *)              INPUT="$1"; shift ;;
    esac
done

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

# Default: all steps if none specified
if ! $VIPERX && ! $DEMUCS && ! $RUBBERBAND; then
    VIPERX=true; DEMUCS=true; RUBBERBAND=true
fi

# ── Validate ─────────────────────────────────────
if [ -z "$INPUT" ]; then
    echo "Usage: pipeline.sh [--viperx] [--demucs] [--rubberband] [--pitch N] <input>"
    exit 1
fi
if [ ! -f "$INPUT" ]; then
    echo "❌ File not found: $INPUT"
    exit 1
fi

# ── Smart defaults: ViperX ya separa vocals, Demucs no necesita repetir ──
if $VIPERX && $DEMUCS && [ "${DEMUCS_KEEP}" = "all" ]; then
    DEMUCS_KEEP="drums,bass,other"
    echo "   ℹ️  ViperX activo → Demucs vocals excluido (ya existe vocals_viperx)"
fi

SONG=$(basename "${INPUT%.*}")
OUTPUT="${OUTPUT:-/output/${SONG}}"

echo "═══════════════════════════════════════"
echo "🎵 Onda Pipeline"
echo "   Input:    ${INPUT}"
echo "   Viperx:   ${VIPERX} (keep: ${VIPERX_KEEP})"
echo "   Demucs:   ${DEMUCS} (keep: ${DEMUCS_KEEP})"
echo "   Rubber:   ${RUBBERBAND} (pitch: ${PITCH})"
echo "   Output:   ${OUTPUT}"
echo "═══════════════════════════════════════"

mkdir -p "${OUTPUT}"

# Clean previous output to prevent accumulation of old stems
rm -f "${OUTPUT}"/*.wav 2>/dev/null || true

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
    # Pre-flight: verify model path exists
    if [ ! -d "${VIPERX_MODEL}" ]; then
        echo "❌ ViperX model not found: ${VIPERX_MODEL}" >&2
        exit 2
    fi
    if [ ! -f /app/inference_universal.py ]; then
        echo "❌ inference_universal.py not found" >&2
        exit 2
    fi
    # Convert overlap ratio (e.g. 0.25) to integer count (e.g. 4) for inference_universal.py
    # The script expects: model_dir input output [num_overlap]
    # Uses python3 for reliable float->int conversion (awk locale-dependent)
    VIPERX_OVERLAP_INT=$(python3 -c "o=${OVERLAP}; print(int(o) if o >= 1 else int(1/o))")
    # Launch inference with progress file for real-time progress tracking
    # Must use a bind-mounted path so both host and container can access the same file
    VIPERX_PROGRESS_FILE="${TMP_VIP}/progress.json"
    rm -f "$VIPERX_PROGRESS_FILE"
    python3 /app/inference_universal.py \
        --progress-file "$VIPERX_PROGRESS_FILE" \
        "${VIPERX_MODEL}" "${INPUT}" "${TMP_VIP}" ${VIPERX_OVERLAP_INT} &
    VIPERX_PID=$!

    # Background loop: read progress file every second, map chunk/total to step range
    while kill -0 $VIPERX_PID 2>/dev/null; do
        if [ -f "$VIPERX_PROGRESS_FILE" ]; then
            chunk=$(python3 -c "import json; print(json.load(open("$VIPERX_PROGRESS_FILE")).get("chunk",0))" 2>/dev/null || echo 0)
            total=$(python3 -c "import json; print(json.load(open("$VIPERX_PROGRESS_FILE")).get("total",0))" 2>/dev/null || echo 0)
            if [ -n "$chunk" ] && [ -n "$total" ] && [ "$total" -gt 0 ]; then
                step_pct=$(( chunk * 100 / total ))
                global_pct=$(( VIPERX_START + (step_pct * (VIPERX_END - VIPERX_START) / 100) ))
                report_progress "running" "viperx" $global_pct
            fi
        fi
        sleep 1
    done
    wait $VIPERX_PID
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

    # Run demucs, capture stdout, parse tqdm percentage for real-time progress
    # Demucs uses \r (carriage return) for tqdm progress bars — split them into lines
    demucs "${DEMUCS_ARGS[@]}" "${DEMUCS_INPUT}" 2>&1 | \
    tr '\r' '\n' | \
    while IFS= read -r line; do
        echo "$line"  # still echo for logging
        if [[ "$line" =~ ([0-9]+)% ]]; then
            pct="${BASH_REMATCH[1]}"
            global_pct=$(( DEMUCS_START + (pct * (DEMUCS_END - DEMUCS_START) / 100) ))
            report_progress "running" "demucs" $global_pct
        fi
    done
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
