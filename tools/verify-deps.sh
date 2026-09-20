#!/usr/bin/env bash
#
# tools/verify-deps.sh — Compara la salida del pipeline de Onda entre la imagen
# actual y esa misma imagen con un conjunto de paquetes candidatos instalados
# encima, en minutos y sin arrancar servidor.
#
# Uso:
#   tools/verify-deps.sh [IMAGEN] [PAQUETES]
#
# Ejemplos:
#   tools/verify-deps.sh
#       # imagen por defecto: onda:v3.4.14
#       # paquetes por defecto: requirements-common.txt + requirements-docker.txt
#   tools/verify-deps.sh onda:v3.4.14 "numpy==2.4.6 scipy==1.18.1"
#       # imagen y paquetes explicitos
#
# Codigo de salida:
#   0 = duracion identica y niveles <= 0,5 dB de diferencia (aceptable)
#   1 = diferencia significativa o error

set -euo pipefail

# -----------------------------------------------------------------------------
# Lecciones aprendidas: por que el metodo es asi
# -----------------------------------------------------------------------------
# 1. NO se arranca servidor.
#    El objetivo es comparar dependencias, no probar la API. Arrancar el backend
#    implica esperar a que escuche, publicar puertos y lidiar con healthchecks.
#    Llamando directamente a /app/pipeline.sh dentro de un docker run --rm se
#    ejecuta solo el paso de inferencia y se acaba el contenedor.
#
# 2. El volumen onda_pytorch-cache se monta en SOLO LECTURA.
#    Ese volumen ya contiene el backend CUDA y onnxruntime-gpu instalados por
#    entrypoint.sh en ejecuciones anteriores. Montarlo :ro evita reinstalarlo
#    en cada arranque y evita que dos corridas concurrentes lo modifiquen.
#
# 3. NO se reconstruye la imagen.
#    Para probar versiones candidatas basta con `pip install -U <pkg>==<ver>`
#    en el contenedor desechable. Asi se ve la resolucion real de dependencias
#    (p. ej. si subir numpy arrastra otro cambio) y no se gasta tiempo en build.
#
# 4. El audio de prueba es sintetico.
#    Se generan 30 s de tono puro con ffmpeg; nunca se usan canciones reales
#    ni los directorios de datos de produccion.
# -----------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_IMAGE="onda:v3.4.14"
REQ_FILES=(
  "$REPO_ROOT/requirements-common.txt"
  "$REPO_ROOT/requirements-docker.txt"
)

IMAGE="${1:-$DEFAULT_IMAGE}"
PACKAGES_ARG="${2:-}"

# -----------------------------------------------------------------------------
# Colores (solo si hay terminal)
# -----------------------------------------------------------------------------
if [[ -t 1 ]]; then
  C_BLD=$'\033[1m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'; C_RED=$'\033[31m'; C_OFF=$'\033[0m'
else
  C_BLD=""; C_GRN=""; C_YEL=""; C_RED=""; C_OFF=""
fi

ok()   { printf '%s[ ok ]%s %s\n' "$C_GRN" "$C_OFF" "$*"; }
warn() { printf '%s[warn]%s %s\n' "$C_YEL" "$C_OFF" "$*"; }
fail() { printf '%s[FAIL]%s %s\n' "$C_RED" "$C_OFF" "$*"; }
info() { printf '%s[INFO]%s %s\n' "$C_BLD" "$C_OFF" "$*"; }

# -----------------------------------------------------------------------------
# Paquetes por defecto: lineas "nombre==version" de los requirements,
# deduplicadas y sin onnxruntime-gpu (vive en el volumen de solo lectura).
# -----------------------------------------------------------------------------
read_default_packages() {
  local seen=() pkg ver line
  declare -A ALREADY
  for req in "${REQ_FILES[@]}"; do
    [[ -f "$req" ]] || { warn "no existe $req"; continue; }
    while IFS= read -r raw || [[ -n "$raw" ]]; do
      line="${raw%%#*}"            # quitar comentarios
      line="${line//[$'\r\n']/}"  # quitar saltos de linea
      line="${line#"${line%%[![:space:]]*}"}"  # trim izq
      line="${line%"${line##*[![:space:]]}"}"  # trim der
      [[ -z "$line" ]] && continue
      [[ "$line" == *"=="* ]] || continue
      pkg="${line%%==*}"
      ver="${line#*==}"
      pkg="${pkg#"${pkg%%[![:space:]]*}"}"; pkg="${pkg%"${pkg##*[![:space:]]}"}"
      ver="${ver#"${ver%%[![:space:]]*}"}"; ver="${ver%"${ver##*[![:space:]]}"}"
      [[ "$pkg" == "onnxruntime-gpu" ]] && continue
      if [[ -z "${ALREADY[$pkg]:-}" ]]; then
        ALREADY[$pkg]=1
        printf '%s ' "${pkg}==${ver}"
      fi
    done < "$req"
  done
}

if [[ -n "$PACKAGES_ARG" ]]; then
  PACKAGES="$PACKAGES_ARG"
else
  PACKAGES="$(read_default_packages)"
fi

if [[ -z "$PACKAGES" ]]; then
  fail "no se han podido leer paquetes candidatos"
  exit 1
fi

info "Imagen:    $IMAGE"
info "Paquetes:  $PACKAGES"

# -----------------------------------------------------------------------------
# Validaciones previas
# -----------------------------------------------------------------------------
for cmd in docker ffmpeg ffprobe; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    fail "falta el comando '$cmd' en el host"
    exit 1
  fi
done

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  fail "la imagen '$IMAGE' no existe en esta maquina"
  exit 1
fi

# -----------------------------------------------------------------------------
# Directorio de trabajo dentro del repo (nunca /tmp)
# -----------------------------------------------------------------------------
mkdir -p "$REPO_ROOT/.hermes"
WORKDIR="$(mktemp -d -p "$REPO_ROOT/.hermes" verify-deps-XXXXXX)"
info "Workdir:   $WORKDIR"

INPUT_DIR="$WORKDIR/input"
OUTPUT_DIR="$WORKDIR/output"
CACHE_DIR="$WORKDIR/cache"
mkdir -p "$INPUT_DIR" "$OUTPUT_DIR" "$CACHE_DIR"

# -----------------------------------------------------------------------------
# Audio sintetico: 30 s de seno estereo
# -----------------------------------------------------------------------------
SINE="$INPUT_DIR/sine30.wav"
ffmpeg -y -f lavfi -i "sine=frequency=1000:duration=30" -ar 44100 -ac 2 -c:a pcm_s16le "$SINE" >/dev/null 2>&1
ok "Audio sintetico generado: $SINE"

# -----------------------------------------------------------------------------
# Variables de entorno que el entrypoint habria preparado, salvo el servidor.
# El backend CUDA se toma del volumen de solo lectura.
# -----------------------------------------------------------------------------
BASE_PYTHONPATH="/app/lib_v5:/opt/pytorch-backends/cuda"
BASE_LD="/opt/pytorch-backends/cuda/torch/lib"
USER_SITE="/app/.local/lib/python3.12/site-packages"

cat > "$WORKDIR/run_baseline.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p /work/output/baseline
if /app/pipeline.sh --device cpu --demucs-keep all --stem-model htdemucs --output /work/output/baseline /work/input/sine30.wav > /work/baseline_pipeline.log 2>&1; then
  rc=0
else
  rc=$?
fi
tail -n 40 /work/baseline_pipeline.log
exit $rc
EOF
chmod +x "$WORKDIR/run_baseline.sh"

cat > "$WORKDIR/run_candidate.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
mkdir -p /work/output/candidate
if pip install --user --no-warn-script-location -U $PACKAGES > /work/candidate_pip.log 2>&1; then
  rc=0
else
  rc=\$?
  echo "pip install fallo con codigo \$rc" >&2
  tail -n 40 /work/candidate_pip.log >&2
  exit \$rc
fi
tail -n 10 /work/candidate_pip.log
export PYTHONPATH="$USER_SITE:$BASE_PYTHONPATH"
if /app/pipeline.sh --device cpu --demucs-keep all --stem-model htdemucs --output /work/output/candidate /work/input/sine30.wav > /work/candidate_pipeline.log 2>&1; then
  rc=0
else
  rc=\$?
fi
tail -n 40 /work/candidate_pipeline.log
exit \$rc
EOF
chmod +x "$WORKDIR/run_candidate.sh"

# -----------------------------------------------------------------------------
# Funcion para ejecutar el pipeline en un contenedor desechable
# -----------------------------------------------------------------------------
run_step() {
  local label="$1"
  local script="$2"
  info "Ejecutando configuracion: $label"
  docker run --rm --gpus all \
    --entrypoint "" \
    -e PYTHONUNBUFFERED=1 \
    -e PYTHONPATH="$BASE_PYTHONPATH" \
    -e LD_LIBRARY_PATH="$BASE_LD${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
    -e ONDA_DATA_DIR=/work \
    -e TORCH_HOME=/app/.cache/torch \
    -e NUMBA_CACHE_DIR=/app/.cache/numba \
    -e XDG_CACHE_HOME=/app/.cache/xdg \
    -e HF_HOME=/app/.cache/hf \
    -v "$WORKDIR:/work" \
    -v "onda_pytorch-cache:/opt/pytorch-backends:ro" \
    -v "$CACHE_DIR:/app/.cache" \
    "$IMAGE" \
    bash "$script"
}

# -----------------------------------------------------------------------------
# Ejecutar las dos configuraciones
# -----------------------------------------------------------------------------
START=$SECONDS

run_step "baseline (imagen actual)" /work/run_baseline.sh
ok "Baseline completado"

run_step "candidata (pip install -U)" /work/run_candidate.sh
ok "Candidata completada"

# -----------------------------------------------------------------------------
# Medir duracion, bytes y niveles de cada pista
# -----------------------------------------------------------------------------
measure() {
  local f="$1"
  local dur bytes mean max
  dur="$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$f" 2>/dev/null || true)"
  bytes="$(stat -c %s "$f" 2>/dev/null || true)"
  local vd
  vd="$(ffmpeg -i "$f" -af volumedetect -f null - 2>&1 || true)"
  mean="$(printf '%s' "$vd" | awk '/mean_volume:/ {print $5}')"
  max="$(printf '%s' "$vd" | awk '/max_volume:/ {print $5}')"
  printf '%s %s %s %s\n' "${dur:-NA}" "${bytes:-NA}" "${mean:-NA}" "${max:-NA}"
}

BASE_OUT="$OUTPUT_DIR/baseline"
CAND_OUT="$OUTPUT_DIR/candidate"

if [[ ! -d "$BASE_OUT" || ! -d "$CAND_OUT" ]]; then
  fail "no se encontraron los directorios de salida"
  exit 1
fi

# -----------------------------------------------------------------------------
# Tabla de comparacion
# -----------------------------------------------------------------------------
printf '\n'
printf '%s%s%s\n' "$C_BLD" '== Resultados ==' "$C_OFF"
printf '%-12s %10s %12s %12s %12s | %10s %12s %12s %12s | %10s %12s %12s\n' \
  "pista" "dur_B" "bytes_B" "mean_B" "max_B" "dur_C" "bytes_C" "mean_C" "max_C" "d_dur" "d_mean" "d_max"
printf '%s\n' '----------------------------------------------------------------------------------------------------------------------------------------------------------------'

PASS=true

for stem in vocals drums bass other; do
  base_file="$BASE_OUT/${stem}.wav"
  cand_file="$CAND_OUT/${stem}.wav"

  if [[ ! -f "$base_file" || ! -f "$cand_file" ]]; then
    printf '%-12s %s\n' "$stem" "FALTA_EN_ALGUNA_CONFIGURACION"
    PASS=false
    continue
  fi

  read -r b_dur b_bytes b_mean b_max <<< "$(measure "$base_file")"
  read -r c_dur c_bytes c_mean c_max <<< "$(measure "$cand_file")"

  if [[ "$b_dur" == "NA" || "$c_dur" == "NA" || "$b_mean" == "NA" || "$c_mean" == "NA" || "$b_max" == "NA" || "$c_max" == "NA" ]]; then
    printf '%-12s %s\n' "$stem" "ERROR_MEDICION"
    PASS=false
    continue
  fi

  d_dur="$(awk "BEGIN {d= $c_dur - $b_dur; printf \"%.6f\", (d<0?-d:d)}")"
  d_mean="$(awk "BEGIN {d= $c_mean - $b_mean; printf \"%.3f\", (d<0?-d:d)}")"
  d_max="$(awk "BEGIN {d= $c_max - $b_max; printf \"%.3f\", (d<0?-d:d)}")"

  printf '%-12s %10s %12s %12s %12s | %10s %12s %12s %12s | %10s %12s %12s\n' \
    "$stem" "$b_dur" "$b_bytes" "$b_mean" "$b_max" "$c_dur" "$c_bytes" "$c_mean" "$c_max" "$d_dur" "$d_mean" "$d_max"

  ok_dur="$(awk "BEGIN {print ($d_dur <= 0.001) ? 0 : 1}")"
  ok_mean="$(awk "BEGIN {print ($d_mean <= 0.5) ? 0 : 1}")"
  ok_max="$(awk "BEGIN {print ($d_max <= 0.5) ? 0 : 1}")"

  if [[ "$ok_dur" -ne 0 || "$ok_mean" -ne 0 || "$ok_max" -ne 0 ]]; then
    PASS=false
  fi
done

ELAPSED=$((SECONDS - START))
printf '\nTiempo total: %dm %ds\n' $((ELAPSED / 60)) $((ELAPSED % 60))

# -----------------------------------------------------------------------------
# Resultado y limpieza
# -----------------------------------------------------------------------------
if $PASS; then
  ok "VALIDACION ACEPTADA: duracion identica y niveles <= 0,5 dB"
  rm -rf "$WORKDIR"
  exit 0
else
  fail "VALIDACION RECHAZADA: la candidata altera la salida mas alla del umbral"
  warn "Workdir conservado para inspeccion: $WORKDIR"
  exit 1
fi
