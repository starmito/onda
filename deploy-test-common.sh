#!/bin/bash
# deploy-test-common.sh — Lógica compartida para validar despliegues GPU de Onda.
# Uso: sourcear desde deploy-test-cuda.sh / deploy-test-rocm.sh después de definir
#   REPO_DIR, BACKEND, COMPOSE_FILE.
#
# Variables opcionales:
#   CONTAINER — nombre del contenedor (default: onda)
#   EXTRA_DOCKER_ENV — flags extra para `docker exec` (p. ej. `-e VAR=val`)

set -u

: "${REPO_DIR:?REPO_DIR no definido}"
: "${BACKEND:?BACKEND no definido}"
: "${COMPOSE_FILE:?COMPOSE_FILE no definido}"

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

do_cd() {
    cd "$REPO_DIR"
}

do_git_pull() {
    git pull
}

do_build() {
    docker compose -f docker-compose.yml -f "$COMPOSE_FILE" build --no-cache
}

do_up() {
    docker compose -f docker-compose.yml -f "$COMPOSE_FILE" up -d --force-recreate
}

do_wait() {
    sleep 3
}

do_check_gpu_log() {
    docker logs "$CONTAINER" 2>&1 | grep -q "GPU detected: ${BACKEND}"
}

_find_input_flac() {
    find "$REPO_DIR/input" -maxdepth 1 -type f -iname "*.flac" | head -1
}

_pythonpath() {
    local py_path="/app/lib_v5"
    if [ "$BACKEND" != "cpu" ]; then
        py_path="${py_path}:/opt/pytorch-backends/${BACKEND}"
    fi
    echo "$py_path"
}

do_test_viperx() {
    local flac model_dir out_dir
    flac="$(_find_input_flac)"
    if [ -z "$flac" ]; then
        echo "   ❌ No se encontró ningún archivo .flac en $REPO_DIR/input"
        return 1
    fi
    echo "   🎵 FLAC de entrada: $(basename "$flac")"

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
        "$model_dir" "/input/$(basename "$flac")" "$out_dir" 4
}

do_test_demucs() {
    local flac out_dir
    flac="$(_find_input_flac)"
    if [ -z "$flac" ]; then
        echo "   ❌ No se encontró ningún archivo .flac en $REPO_DIR/input"
        return 1
    fi
    echo "   🎵 FLAC de entrada: $(basename "$flac")"

    out_dir="/output/test-${BACKEND}-demucs"
    rm -rf "${OUTPUT_BASE}-demucs"
    mkdir -p "${OUTPUT_BASE}-demucs"

    docker exec ${EXTRA_DOCKER_ENV:-} -e PYTHONPATH="$(_pythonpath)" "$CONTAINER" \
        demucs -n htdemucs_ft --device cuda -o "$out_dir" "/input/$(basename "$flac")"
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
    run_step "a) cd al repositorio" do_cd
    run_step "b) git pull" do_git_pull
    run_step "c) docker compose build --no-cache" do_build
    run_step "d) docker compose up -d --force-recreate" do_up
    run_step "e) esperar 3s" do_wait
    run_step "f) verificar log 'GPU detected: ${BACKEND}'" do_check_gpu_log
    run_step "g) probar BS-Roformer (inference_universal.py)" do_test_viperx
    run_step "h) probar Demucs htdemucs_ft" do_test_demucs
    run_step "i) verificar tamaño y duración de outputs" do_verify_outputs

    echo ""
    echo "════════════════════════════════════════════════════"
    echo "Resultados: $PASS pasos OK, $FAIL fallos"
    echo "════════════════════════════════════════════════════"

    [ "$FAIL" -eq 0 ]
}
