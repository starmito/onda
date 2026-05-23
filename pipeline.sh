#!/usr/bin/env bash
# Onda Pipeline — Modular step-based audio separation
#
# Usage:
#   pipeline.sh [flags] <input_audio>
#
# Flags (any combination):
#   --viperx              BS-Roformer-Viperx → vocal + instrumental
#   --viperx-keep WHAT    What to save: instrumental | vocals | both (default)
#   --demucs              HTDemucs_ft → drums, bass, other, vocals
#   --demucs-keep LIST    Stems to keep: drums,bass,other,vocals or all (default)
#   --rubberband          Pitch shift all stems except drums
#   --pitch N             Semitones for rubberband (default: 0)
#   --output DIR          Output directory (default: /output/<song_name>)
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

# ── Progress reporting ──────────────────────────
START_TIME=$(date +%s)
STATUS_FILE="/tmp/pipeline_status.json"
CURRENT_STEP=""

report_progress() {
    local status="$1"
    local step="$2"
    local progress="$3"
    local now elapsed eta
    now=$(date +%s)
    elapsed=$((now - START_TIME))
    eta=0
    if [ "$progress" -gt 0 ] && [ "$elapsed" -gt 0 ]; then
        eta=$(( (elapsed * 100 / progress) - elapsed ))
    fi
    cat > "$STATUS_FILE" << JSONEOF
{"status":"$status","step":"$step","progress":$progress,"song":"${SONG:-}","elapsed":$elapsed,"eta":$eta}
JSONEOF
}
trap 'report_progress "error" "${CURRENT_STEP:-unknown}" 0' ERR


# ── Parse flags ──────────────────────────────────
VIPERX=false
VIPERX_KEEP="both"
DEMUCS=false
DEMUCS_KEEP="all"
RUBBERBAND=false
PITCH=0
OUTPUT=""

INPUT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --viperx)       VIPERX=true; shift ;;
        --viperx-keep)  VIPERX_KEEP="$2"; shift 2 ;;
        --demucs)       DEMUCS=true; shift ;;
        --demucs-keep)  DEMUCS_KEEP="$2"; shift 2 ;;
        --rubberband)   RUBBERBAND=true; shift ;;
        --pitch)        PITCH="$2"; shift 2 ;;
        --output)       OUTPUT="$2"; shift 2 ;;
        -*)             echo "Unknown flag: $1"; exit 1 ;;
        *)              INPUT="$1"; shift ;;
    esac
done

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

SONG=$(basename "${INPUT%.*}")
OUTPUT="${OUTPUT:-/output/${SONG}}"

VIPERX_MODEL="/app/models/VR_Models/BS_Roformer_Viperx"

echo "═══════════════════════════════════════"
echo "🎵 Onda Pipeline"
echo "   Input:    ${INPUT}"
echo "   Viperx:   ${VIPERX} (keep: ${VIPERX_KEEP})"
echo "   Demucs:   ${DEMUCS} (keep: ${DEMUCS_KEEP})"
echo "   Rubber:   ${RUBBERBAND} (pitch: ${PITCH})"
echo "   Output:   ${OUTPUT}"
echo "═══════════════════════════════════════"

mkdir -p "${OUTPUT}"

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
    report_progress "running" "viperx" 5
    python /app/inference_universal.py "${VIPERX_MODEL}" "${INPUT}" "${TMP_VIP}" 8
    echo "   ✅ Viperx done"

    report_progress "running" "viperx" 35

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
    report_progress "running" "demucs" 45
    demucs -n htdemucs_ft -o "${TMP_DEM}" "${DEMUCS_INPUT}"
    echo "   ✅ HTDemucs_ft done"

    # Find stem directory
    DEMUCS_OUT=$(find "${TMP_DEM}" -type d -name "htdemucs_ft" | head -1)
    report_progress "running" "demucs" 75
    STEM_DIR=$(find "${DEMUCS_OUT}" -maxdepth 1 -type d ! -name "htdemucs_ft" | head -1)
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

# ══════════════════════════════════════════════════════
# STEP 3: Rubberband → pitch shift (skip drums)
# ══════════════════════════════════════════════════════
if $RUBBERBAND; then
    echo ""
    echo "🎛️  Rubberband — pitch ${PITCH} semitones"

    if [ -n "${STEM_DIR}" ]; then
        report_progress "running" "rubberband" 80
        # Stems from demucs or viperx — apply rubberband to selected stems
        for stem in bass other vocals; do
            if [[ "${DEMUCS_KEEP}" == "all" ]] || [[ ",${DEMUCS_KEEP}," == *",${stem},"* ]]; then
                SRC=$(find "${STEM_DIR}" -maxdepth 1 -iname "*${stem}*" | head -1)
                if [ -n "${SRC}" ]; then
                    rubberband --pitch "${PITCH}" --quiet "${SRC}" "${OUTPUT}/${stem}.wav"
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
        rubberband --pitch "${PITCH}" --quiet "${INPUT}" "${OUT_FILE}"
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
ls -lh "${OUTPUT}"/*.wav 2>/dev/null | awk '{print "   " $NF " (" $5 ")"}'
echo "═══════════════════════════════════════"
