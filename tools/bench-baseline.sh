#!/usr/bin/env bash
# tools/bench-baseline.sh — Mide la versión desplegada de Onda hablando con la API.
#
# Uso: bash tools/bench-baseline.sh --label <etiqueta> [opciones]
#
# Opciones:
#   --label <etiqueta>       Requerido. Etiqueta del benchmark (p. ej. "base").
#   --host <url>             URL base de Onda (default: http://localhost:3000).
#   --preset <nombre>        Preset a usar. Si no se indica, usa el default de la API.
#   --input <ruta>           Clip de prueba existente. Si no existe, se genera.
#   --duration <segundos>    Duración del clip generado (default: 10).
#   --output-dir <dir>       Directorio de salida (default: .hermes/bench).
#   --sample-interval <seg>  Intervalo de muestreo (default: 0.5).
#   --max-wait <seg>         Tiempo máximo de espera por el trabajo (default: 3600).
#   --help                   Muestra esta ayuda.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

LABEL=""
HOST="http://localhost:3000"
PRESET=""
INPUT_CLIP=""
CLIP_DURATION=10
OUTPUT_DIR="${REPO_ROOT}/.hermes/bench"
SAMPLE_INTERVAL=0.5
MAX_WAIT=3600

usage() {
    cat <<'EOF'
Uso: bash tools/bench-baseline.sh --label <etiqueta> [opciones]

Opciones:
  --label <etiqueta>       Requerido. Etiqueta del benchmark (p. ej. "base").
  --host <url>             URL base de Onda (default: http://localhost:3000).
  --preset <nombre>        Preset a usar. Si no se indica, usa el default de la API.
  --input <ruta>           Clip de prueba existente. Si no existe, se genera.
  --duration <segundos>    Duración del clip generado (default: 10).
  --output-dir <dir>       Directorio de salida (default: .hermes/bench).
  --sample-interval <seg>  Intervalo de muestreo (default: 0.5).
  --max-wait <seg>         Tiempo máximo de espera por el trabajo (default: 3600).
  --help                   Muestra esta ayuda.
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --label)
            LABEL="$2"; shift 2 ;;
        --host)
            HOST="$2"; shift 2 ;;
        --preset)
            PRESET="$2"; shift 2 ;;
        --input)
            INPUT_CLIP="$2"; shift 2 ;;
        --duration)
            CLIP_DURATION="$2"; shift 2 ;;
        --output-dir)
            OUTPUT_DIR="$2"; shift 2 ;;
        --sample-interval)
            SAMPLE_INTERVAL="$2"; shift 2 ;;
        --max-wait)
            MAX_WAIT="$2"; shift 2 ;;
        --help)
            usage; exit 0 ;;
        *)
            echo "Opción desconocida: $1" >&2; usage >&2; exit 2 ;;
    esac
done

if [[ -z "$LABEL" ]]; then
    echo "Error: --label es obligatorio" >&2
    usage >&2
    exit 2
fi

# ── Dependencias ──
for dep in curl jq ffmpeg python3; do
    if ! command -v "$dep" >/dev/null 2>&1; then
        echo "Error: dependencia no encontrada: $dep" >&2
        exit 2
    fi
done

mkdir -p "${OUTPUT_DIR}/fixtures"

TIMESTAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT_FILE="${OUTPUT_DIR}/${TIMESTAMP}-${LABEL}.json"

api_get() {
    local path="$1"
    curl -sS -f "${HOST}${path}" || true
}

api_post_json() {
    local path="$1"
    local body="$2"
    curl -sS -f -X POST "${HOST}${path}" \
        -H "Content-Type: application/json" \
        -d "$body" || true
}

# ── Clip de prueba ──
FIXTURE_DIR="${OUTPUT_DIR}/fixtures"
DEFAULT_CLIP="${FIXTURE_DIR}/bench_clip_${CLIP_DURATION}s.wav"

if [[ -n "$INPUT_CLIP" ]]; then
    CLIP_PATH="$INPUT_CLIP"
    if [[ ! -f "$CLIP_PATH" ]]; then
        echo "Error: clip no encontrado: $CLIP_PATH" >&2
        exit 2
    fi
else
    CLIP_PATH="$DEFAULT_CLIP"
    if [[ ! -f "$CLIP_PATH" ]]; then
        echo "Generando clip de prueba: $CLIP_PATH"
        ffmpeg -y -f lavfi -i "sine=frequency=440:duration=${CLIP_DURATION}" \
            -ar 44100 -ac 2 -c:a pcm_s16le "$CLIP_PATH" >/dev/null 2>&1
    fi
fi

CLIP_INFO=$(ffprobe -v error -show_entries stream=sample_rate,channels,duration -of json "$CLIP_PATH")
CLIP_SAMPLE_RATE=$(echo "$CLIP_INFO" | jq -r '.streams[0].sample_rate // empty')
CLIP_CHANNELS=$(echo "$CLIP_INFO" | jq -r '.streams[0].channels // empty')
CLIP_DURATION_REAL=$(echo "$CLIP_INFO" | jq -r '.streams[0].duration // empty')

# ── Subir clip por API ──
echo "Subiendo clip a ${HOST}..."
UPLOAD_RESP=$(curl -sS -f -X POST "${HOST}/api/daw/upload" \
    -F "file=@${CLIP_PATH}" \
    -F "song=onda_bench_clip" || true)

if [[ -z "$UPLOAD_RESP" ]] || ! echo "$UPLOAD_RESP" | jq -e '.path' >/dev/null 2>&1; then
    echo "Advertencia: no se pudo subir el clip por /api/daw/upload; se usará el path relativo por defecto." >&2
    UPLOAD_PATH="daw-data/onda_bench_clip/original.wav"
else
    UPLOAD_PATH=$(echo "$UPLOAD_RESP" | jq -r '.path')
fi

# ── Versiones ──
HEALTH=$(api_get "/api/health")
BACKEND_VERSION=$(echo "$HEALTH" | jq -r '.version // "unknown"')
FRONTEND_VERSION=$(echo "$HEALTH" | jq -r '.components.frontend.version // "unknown"')
PIPELINE_VERSION=$(echo "$HEALTH" | jq -r '.components.pipeline.version // "unknown"')
GPU_TYPE=$(echo "$HEALTH" | jq -r '.components.gpu.type // "unknown"')
GPU_DETAIL=$(echo "$HEALTH" | jq -r '.components.gpu.detail // "unknown"')
GPU_TOTAL_MB=$(echo "$HEALTH" | jq -r '.components.gpu.total_mb // null')

# Versiones de Python/pipeline. Ejecutamos en el host donde corra el script.
PYTHON_DEPS=$(python3 - <<'PY'
import sys
try:
    import torch
    print("torch", torch.__version__)
except Exception:
    print("torch", "unknown")
try:
    import torchvision
    print("torchvision", torchvision.__version__)
except Exception:
    print("torchvision", "unknown")
try:
    import onnxruntime
    print("onnxruntime", onnxruntime.__version__)
except Exception:
    print("onnxruntime", "unknown")
try:
    import demucs
    print("demucs", demucs.__version__)
except Exception:
    print("demucs", "unknown")
PY
)

TORCH_VERSION=$(echo "$PYTHON_DEPS" | awk '/^torch /{print $2}')
TORCHVISION_VERSION=$(echo "$PYTHON_DEPS" | awk '/^torchvision /{print $2}')
ONNX_VERSION=$(echo "$PYTHON_DEPS" | awk '/^onnxruntime /{print $2}')
DEMUCS_VERSION=$(echo "$PYTHON_DEPS" | awk '/^demucs /{print $2}')

# ── Preset ──
ALL_PRESETS=$(api_get "/api/presets")
DEFAULT_PRESET=$(api_get "/api/presets/default" | jq -r '.name // empty')

if [[ -z "$PRESET" ]]; then
    PRESET="${DEFAULT_PRESET:-Voces Total}"
fi

PRESET_JSON=$(echo "$ALL_PRESETS" | jq --arg name "$PRESET" '.[$name] // empty')
if [[ -z "$PRESET_JSON" ]]; then
    echo "Error: preset no encontrado: $PRESET" >&2
    exit 2
fi

# ── Flags de Ajustes → Modelos ──
MODEL_FLAGS_JSON='{}'
MODEL_NAMES=$(echo "$PRESET_JSON" | jq -r '[.steps[]?.model] | unique | .[]' 2>/dev/null || true)
for model in $MODEL_NAMES; do
    CFG=$(api_get "/api/models/${model}/config")
    if [[ -n "$CFG" ]]; then
        MODEL_FLAGS_JSON=$(echo "$MODEL_FLAGS_JSON" | jq --arg m "$model" --argjson cfg "$CFG" '.[$m] = $cfg')
    fi
done

# ── Lanzar trabajo ──
SEPARATE_BODY=$(jq -n --arg preset "$PRESET" --arg input "$UPLOAD_PATH" '{preset: $preset, input: $input}')
echo "Lanzando separación: preset=$PRESET input=$UPLOAD_PATH"
SEPARATE_RESP=$(api_post_json "/api/separate" "$SEPARATE_BODY")
SONG=$(echo "$SEPARATE_RESP" | jq -r '.song // empty')
if [[ -z "$SONG" ]]; then
    echo "Error: no se pudo encolar el trabajo" >&2
    echo "Respuesta: $SEPARATE_RESP" >&2
    exit 2
fi

# ── Muestrear hasta que termine ──
TIMELINE_FILE="${OUTPUT_DIR}/.timeline-${LABEL}.json"
echo '[]' > "$TIMELINE_FILE"
START_EPOCH=$(date +%s)
LAST_STEP_NAME=""
STEP_START_EPOCH=$START_EPOCH
STEP_TIMES_FILE="${OUTPUT_DIR}/.step_times-${LABEL}.json"
echo '{}' > "$STEP_TIMES_FILE"
STATUS="processing"

sample_vram() {
    if command -v nvidia-smi >/dev/null 2>&1; then
        nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits 2>/dev/null | head -n1 | tr -d '[:space:]'
    fi
}

append_timeline() {
    local entry="$1"
    jq --argjson e "$entry" '. + [$e]' "$TIMELINE_FILE" > "${TIMELINE_FILE}.tmp"
    mv "${TIMELINE_FILE}.tmp" "$TIMELINE_FILE"
}

while true; do
    NOW=$(date +%s)
    ELAPSED=$((NOW - START_EPOCH))
    if [[ $ELAPSED -gt $MAX_WAIT ]]; then
        STATUS="timeout"
        break
    fi

    QUEUE=$(api_get "/api/queue/status")
    PROCESSES=$(api_get "/api/processes/status")
    VRAM=$(sample_vram)
    [[ -z "$VRAM" ]] && VRAM="null"

    JOB=$(echo "$QUEUE" | jq --arg song "$SONG" '.jobs[]? | select(.song == $song) // empty')
    CUR_STATUS=$(echo "$JOB" | jq -r '.status // "unknown"')
    CUR_STEP=$(echo "$JOB" | jq -r '.step_name // ""')

    ENTRY=$(jq -n \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson elapsed "$ELAPSED" \
        --argjson queue "$QUEUE" \
        --argjson processes "$PROCESSES" \
        --argjson vram "${VRAM:-null}" \
        '{timestamp: $ts, elapsed_seconds: $elapsed, queue: $queue, processes: $processes, vram_used_mb: $vram}')
    append_timeline "$ENTRY"

    if [[ "$CUR_STEP" != "$LAST_STEP_NAME" && -n "$CUR_STEP" ]]; then
        if [[ -n "$LAST_STEP_NAME" ]]; then
            STEP_ELAPSED=$((NOW - STEP_START_EPOCH))
            jq --arg s "$LAST_STEP_NAME" --argjson dt "$STEP_ELAPSED" '.[$s] = $dt' "$STEP_TIMES_FILE" > "${STEP_TIMES_FILE}.tmp"
            mv "${STEP_TIMES_FILE}.tmp" "$STEP_TIMES_FILE"
        fi
        LAST_STEP_NAME="$CUR_STEP"
        STEP_START_EPOCH=$NOW
    fi

    if [[ "$CUR_STATUS" == "done" || "$CUR_STATUS" == "error" || "$CUR_STATUS" == "blocked_no_gpu" ]]; then
        STATUS="$CUR_STATUS"
        if [[ -n "$LAST_STEP_NAME" ]]; then
            STEP_ELAPSED=$((NOW - STEP_START_EPOCH))
            jq --arg s "$LAST_STEP_NAME" --argjson dt "$STEP_ELAPSED" '.[$s] = $dt' "$STEP_TIMES_FILE" > "${STEP_TIMES_FILE}.tmp"
            mv "${STEP_TIMES_FILE}.tmp" "$STEP_TIMES_FILE"
        fi
        break
    fi

    sleep "$SAMPLE_INTERVAL"
done

END_EPOCH=$(date +%s)
TOTAL_SECONDS=$((END_EPOCH - START_EPOCH))

TIMELINE=$(cat "$TIMELINE_FILE")
STEP_TIMES=$(cat "$STEP_TIMES_FILE")

# ── VRAM pico y media ──
VRAM_PEAK=$(echo "$TIMELINE" | jq '[.[]?.vram_used_mb | numbers] | max // null')
VRAM_MEAN=$(echo "$TIMELINE" | jq '[.[]?.vram_used_mb | numbers] | (add / length) | round // null')

# ── Stems producidos ──
RESULTS=$(api_get "/api/results")
STEMS_JSON='[]'
SONG_RESULTS=$(echo "$RESULTS" | jq --arg song "$SONG" '.[]? | select(.song == $song) // empty')
STEM_FILES=$(echo "$SONG_RESULTS" | jq -r '.files[]? | "\(.name)\t\(.path)"' 2>/dev/null || true)

# Helper Python inline para energía/crest.
calc_stem_stats() {
    local url="$1"
    local tmp_stem="${FIXTURE_DIR}/_tmp_stem.wav"
    python3 - "$url" "$tmp_stem" "$REPO_ROOT" <<'PY'
import sys, json, math, os, subprocess
url = sys.argv[1]
tmp_stem = sys.argv[2]
repo_root = sys.argv[3]
os.makedirs(os.path.dirname(tmp_stem), exist_ok=True)
try:
    subprocess.run(["curl", "-sS", "-f", "-o", tmp_stem, url], check=True)
except Exception as e:
    print(json.dumps({"energy_rms_db": None, "crest_db": None, "error": f"download: {e}"}))
    sys.exit(0)

try:
    import soundfile as sf
    import numpy as np
    data, sr = sf.read(tmp_stem, dtype="float32")
    if data.ndim == 1:
        data = data.reshape(-1, 1)
    rms = math.sqrt(max(float(np.mean(data**2)), 1e-18))
    energy_db = 20 * math.log10(rms)
    peak = float(np.max(np.abs(data)))
    crest_db = 20 * math.log10(max(peak, 1e-9) / max(rms, 1e-9))
    print(json.dumps({"sample_rate": sr, "energy_rms_db": round(energy_db, 3), "crest_db": round(crest_db, 3), "peak": round(peak, 6)}))
except Exception as e:
    # Fallback con ffmpeg astats si soundfile no está disponible.
    try:
        out = subprocess.check_output([
            "ffmpeg", "-i", tmp_stem, "-af", "astats=measure_perchannel=none:measure_overall=RMS_level:metadata=1",
            "-f", "null", "-"
        ], stderr=subprocess.STDOUT, text=True)
        rms_line = [l for l in out.splitlines() if "RMS level" in l]
        if rms_line:
            val = rms_line[0].split(":")[-1].strip().replace(" dB", "")
            energy_db = float(val)
            print(json.dumps({"energy_rms_db": round(energy_db, 3), "crest_db": None, "sample_rate": None, "peak": None}))
        else:
            raise RuntimeError("no RMS line")
    except Exception as e2:
        print(json.dumps({"energy_rms_db": None, "crest_db": None, "error": str(e)}))
PY
}

while IFS=$'\t' read -r name path; do
    [[ -z "$name" ]] && continue
    STATS=$(calc_stem_stats "${HOST}${path}" 2>/dev/null || echo '{"energy_rms_db": null, "crest_db": null}')
    # Limpiar tmp entre stems.
    rm -f "${FIXTURE_DIR}/_tmp_stem.wav"
    STEMS_JSON=$(echo "$STEMS_JSON" | jq --arg n "$name" --arg p "$path" --argjson s "$STATS" '. + [{name: $n, path: $p, energy_rms_db: $s.energy_rms_db, crest_db: $s.crest_db, sample_rate: ($s.sample_rate // null), peak: ($s.peak // null)}]')
done <<< "$STEM_FILES"

# ── Ensamblar JSON final ──
VERSIONS_JSON=$(jq -n \
    --arg backend "$BACKEND_VERSION" \
    --arg pipeline "$PIPELINE_VERSION" \
    --arg frontend "$FRONTEND_VERSION" \
    --arg torch "$TORCH_VERSION" \
    --arg torchvision "$TORCHVISION_VERSION" \
    --arg onnxruntime "$ONNX_VERSION" \
    --arg demucs "$DEMUCS_VERSION" \
    '{onda_backend: $backend, onda_pipeline: $pipeline, onda_frontend: $frontend, torch: $torch, torchvision: $torchvision, onnxruntime: $onnxruntime, demucs: $demucs}')

DEVICE_JSON=$(jq -n \
    --arg type "$GPU_TYPE" \
    --arg detail "$GPU_DETAIL" \
    --argjson total_mb "$GPU_TOTAL_MB" \
    '{type: $type, detail: $detail, vram_total_mb: $total_mb}')

AUDIO_JSON=$(jq -n \
    --arg clip "$CLIP_PATH" \
    --arg upload_path "$UPLOAD_PATH" \
    --argjson duration "${CLIP_DURATION_REAL:-null}" \
    --argjson sample_rate "${CLIP_SAMPLE_RATE:-null}" \
    --argjson channels "${CLIP_CHANNELS:-null}" \
    '{clip_path: $clip, upload_path: $upload_path, duration_seconds: $duration, sample_rate: $sample_rate, channels: $channels}')

SUMMARY_JSON=$(jq -n \
    --arg song "$SONG" \
    --arg status "$STATUS" \
    --argjson total "$TOTAL_SECONDS" \
    --argjson step_times "$STEP_TIMES" \
    --argjson vram_peak "${VRAM_PEAK:-null}" \
    --argjson vram_mean "${VRAM_MEAN:-null}" \
    --argjson stems "$STEMS_JSON" \
    '{song: $song, status: $status, total_seconds: $total, step_seconds: $step_times, vram_peak_mb: $vram_peak, vram_mean_mb: $vram_mean, stems: $stems}')

jq -n \
    --arg label "$LABEL" \
    --arg timestamp "$TIMESTAMP" \
    --arg host "$HOST" \
    --arg preset_name "$PRESET" \
    --argjson sample_interval "$SAMPLE_INTERVAL" \
    --argjson versions "$VERSIONS_JSON" \
    --argjson preset "$PRESET_JSON" \
    --argjson model_flags "$MODEL_FLAGS_JSON" \
    --argjson device "$DEVICE_JSON" \
    --argjson audio "$AUDIO_JSON" \
    --argjson timeline "$TIMELINE" \
    --argjson summary "$SUMMARY_JSON" \
    '{
        meta: {label: $label, timestamp: $timestamp, host: $host, preset_name: $preset_name, sample_interval_seconds: $sample_interval},
        versions: $versions,
        preset: $preset,
        model_flags: $model_flags,
        device: $device,
        audio: $audio,
        timeline: $timeline,
        summary: $summary
    }' > "$OUT_FILE"

# ── Limpiar temporales ──
rm -f "$TIMELINE_FILE" "$STEP_TIMES_FILE" "${FIXTURE_DIR}/_tmp_stem.wav"

# ── Resumen en pantalla ──
echo ""
echo "Benchmark guardado en: $OUT_FILE"
echo "  Preset:    $PRESET"
echo "  Estado:    $STATUS"
echo "  Duración:  ${TOTAL_SECONDS}s"
echo "  VRAM pico: ${VRAM_PEAK:-N/A} MiB"
echo "  VRAM media:${VRAM_MEAN:-N/A} MiB"
echo "  Stems:     $(echo "$STEMS_JSON" | jq -r '[.[]?.name] | join(", ")')"
echo "  Versiones: Onda=$BACKEND_VERSION torch=$TORCH_VERSION demucs=$DEMUCS_VERSION"

if [[ "$STATUS" != "done" ]]; then
    exit 1
fi
exit 0
