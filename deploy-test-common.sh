#!/bin/bash
# deploy-test-common.sh — Lógica compartida para validar despliegues de Onda.
# Uso: sourcear desde deploy-test.sh después de definir REPO_DIR y BACKEND.
#
# Variables opcionales:
#   CONTAINER — nombre del contenedor (default: onda)
#   EXTRA_DOCKER_ENV — flags extra para `docker exec` (p. ej. `-e VAR=val`)

set -u

: "${REPO_DIR:?REPO_DIR no definido}"
: "${BACKEND:?BACKEND no definido}"

CONTAINER="${CONTAINER:-onda}"
OUTPUT_ROOT="$REPO_DIR/output"
OUTPUT_BASE="$OUTPUT_ROOT/test-${BACKEND}"

PASS=0
FAIL=0

step() { echo ""; echo "==> $1"; }
ok()   { echo "   ✅ $1"; ((PASS++)); }
fail() { echo "   ❌ $1"; ((FAIL++)); }

run_step() {
    local name="$1"
    shift
    step "$name"
    if "$@"; then
        ok "$name"
    else
        fail "$name"
    fi
}

_find_test_audio() {
    local wav
    wav="$REPO_DIR/input/test_sound.wav"
    if [ ! -f "$wav" ]; then
        echo "   ⚠️  No se encontró test_sound.wav; generándolo..."
        python3 "$REPO_DIR/scripts/gen-test-audio.py"
        cp "$REPO_DIR/test_sound.wav" "$REPO_DIR/input/"
    fi
    echo "$wav"
}

_pythonpath() {
    local py_path="/app/lib_v5"
    if [ "$BACKEND" != "cpu" ]; then
        py_path="${py_path}:/opt/pytorch-backends/${BACKEND}"
    fi
    echo "$py_path"
}

do_test_viperx() {
    local wav model_dir out_dir
    wav="$(_find_test_audio)"
    if [ -z "$wav" ] || [ ! -f "$wav" ]; then
        echo "   ❌ No se encontró test_sound.wav en $REPO_DIR/input"
        return 1
    fi
    echo "   🎵 Audio de entrada: $(basename "$wav")"

    model_dir="/app/models/VR_Models/BS_Roformer_Viperx"
    if ! docker exec "$CONTAINER" test -d "$model_dir"; then
        echo "   ❌ Modelo BS-Roformer no encontrado en $model_dir"
        echo "      Descárgalo con: bash scripts/download-models.sh viperx"
        return 1
    fi

    out_dir="/output/test-${BACKEND}-viperx"
    rm -rf "${OUTPUT_BASE}-viperx"
    mkdir -p "${OUTPUT_BASE}-viperx"

    docker exec ${EXTRA_DOCKER_ENV:-} -e PYTHONPATH="$(_pythonpath)" "$CONTAINER" \
        python3 /app/inference_universal.py \
        "$model_dir" "/input/$(basename "$wav")" "$out_dir" "${VIPERX_OVERLAP:-4}"
}

do_test_demucs() {
    local wav out_dir
    wav="$(_find_test_audio)"
    if [ -z "$wav" ] || [ ! -f "$wav" ]; then
        echo "   ❌ No se encontró test_sound.wav en $REPO_DIR/input"
        return 1
    fi
    echo "   🎵 Audio de entrada: $(basename "$wav")"

    out_dir="/output/test-${BACKEND}-demucs"
    rm -rf "${OUTPUT_BASE}-demucs"
    mkdir -p "${OUTPUT_BASE}-demucs"

    docker exec ${EXTRA_DOCKER_ENV:-} -e PYTHONPATH="$(_pythonpath)" "$CONTAINER" \
        demucs -n htdemucs_ft --device cuda ${DEMUCS_EXTRA:-} -o "$out_dir" "/input/$(basename "$wav")"
}

do_verify_outputs() {
    local all_ok=true wav_count
    for dir in "${OUTPUT_BASE}-viperx" "${OUTPUT_BASE}-demucs"; do
        echo "   📁 Verificando $(basename "$dir")"
        wav_count=0
        while IFS= read -r -d '' wav; do
            wav_count=1
            local size basename_wav container_wav dur
            size=$(stat -c%s "$wav")
            basename_wav=$(basename "$wav")
            container_wav="/output${wav#$REPO_DIR/output}"

            if [ "$size" -lt 102400 ]; then
                echo "   ❌ $basename_wav pesa solo ${size}B (< 100KB)"
                all_ok=false
            fi

            dur=$(docker exec "$CONTAINER" ffprobe -v error \
                -show_entries format=duration -of csv=p=0 "$container_wav" 2>/dev/null || echo "0")

            if awk "BEGIN {exit !($dur >= 10)}"; then
                echo "   ✅ $basename_wav ${size}B ${dur}s"
            else
                echo "   ❌ $basename_wav duración ${dur}s (< 10s)"
                all_ok=false
            fi
        done < <(find "$dir" -type f -name "*.wav" -print0)

        if [ "$wav_count" -eq 0 ]; then
            echo "   ❌ No se encontraron archivos .wav en $(basename "$dir")"
            all_ok=false
        fi
    done

    $all_ok
}

run_all_steps() {
    run_step "a) probar BS-Roformer (inference_universal.py)" do_test_viperx
    run_step "b) probar Demucs htdemucs_ft" do_test_demucs
    run_step "c) verificar tamaño y duración de outputs" do_verify_outputs

    echo ""
    echo "════════════════════════════════════════════════════"
    echo "Resultados: $PASS pasos OK, $FAIL fallos"
    echo "════════════════════════════════════════════════════"

    [ "$FAIL" -eq 0 ]
}
