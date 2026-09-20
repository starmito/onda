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
#   tools/verify-deps.sh onda:v3.4.14 ""
#       # control A/A real: no se instala nada; se ejecuta la misma
#       # configuracion dos veces y se mide el ruido de medicion.
#
# Codigo de salida:
#   0 = la diferencia entre configuraciones no supera el ruido medido
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
# 4. El audio de prueba es sintetico y con contenido real.
#    Se genera una mezcla estereo de 30 s con senoides, ruido y un tren de
#    impulsos, para que las cuatro pistas de Demucs tengan energia que separar.
#    Un tono puro hace que algunas pistas sean ruido numerico y arruina la
#    comparacion.
#
# 5. El criterio de aceptacion se fija con un control A/A interno.
#    Se ejecuta dos veces la misma configuracion y la diferencia entre esas
#    dos corridas es el ruido de medicion. La candidata se acepta si su
#    diferencia respecto a la primera no supera ese ruido con un margen.
# -----------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_IMAGE="onda:v3.4.14"
REQ_FILES=(
  "$REPO_ROOT/requirements-common.txt"
  "$REPO_ROOT/requirements-docker.txt"
)

SILENCE_THRESHOLD=-60.0
MARGIN=1.5
MIN_THRESHOLD=0.3

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

IMAGE="${1:-$DEFAULT_IMAGE}"

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

# Si se pasan dos argumentos, el segundo es la lista explicita de paquetes.
# Si es la cadena vacia, se interpreta como "sin paquetes" (control A/A real).
if [[ $# -ge 2 ]]; then
  PACKAGES="$2"
else
  PACKAGES="$(read_default_packages)"
fi

if [[ -z "$PACKAGES" ]]; then
  MODE="AA"
  info "Modo control A/A (lista de paquetes vacia)"
  info "Imagen:    $IMAGE"
  info "Paquetes:  (ninguno)"
else
  MODE="AB"
  info "Imagen:    $IMAGE"
  info "Paquetes:  $PACKAGES"
fi

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
# Audio de prueba sintetico: mezcla estereo con contenido real para separar.
# Componentes:
#   - 100 Hz  (bajo, esperado en bass)
#   - 250 Hz + 1000 Hz con vibrato (mas parecido a voz, esperado en vocals)
#   - 800 Hz  (armonicos medios, esperado en other)
#   - 1500 Hz (agudos, esperado en other)
#   - ruido blanco de bajo nivel
#   - tren de impulsos de 440 Hz, 60 ms cada 500 ms (contenido ritmico, drums)
# Todo se mezcla con pesos y se limita a -0.5 dB para evitar clipping.
# -----------------------------------------------------------------------------
TEST_WAV="$INPUT_DIR/test_mix.wav"
FFMPEG_FILTER="
sine=frequency=100:duration=30[s0];
aevalsrc=exprs='0.3*(sin(2*PI*250*t+5*sin(2*PI*5*t))+0.4*sin(2*PI*1000*t+8*sin(2*PI*5*t)))':s=44100:d=30[voc];
sine=frequency=800:duration=30[s2];
sine=frequency=1500:duration=30[s3];
anoisesrc=a=0.02:r=44100:d=30[noise];
aevalsrc=exprs='0.8*sin(2*PI*440*t)*gt(0.06,t*2-floor(t*2))':s=44100:d=30[imp];
[s0][voc][s2][s3][noise][imp]amix=inputs=6:duration=longest:normalize=0:weights='1.2 1.2 1 1 0.3 1.5'[pre];
[pre]alimiter=limit=-0.5dB:level=true[out]
"
ffmpeg -y -filter_complex "$FFMPEG_FILTER" -map "[out]" \
  -ar 44100 -ac 2 -c:a pcm_s16le "$TEST_WAV" >/dev/null 2>&1
ok "Audio de prueba generado: $TEST_WAV"

# -----------------------------------------------------------------------------
# Variables de entorno que el entrypoint habria preparado, salvo el servidor.
# El backend CUDA se toma del volumen de solo lectura.
# -----------------------------------------------------------------------------
BASE_PYTHONPATH="/app/lib_v5:/opt/pytorch-backends/cuda"
BASE_LD="/opt/pytorch-backends/cuda/torch/lib"
USER_SITE="/app/.local/lib/python3.12/site-packages"

cat > "$WORKDIR/run_pipeline.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
LABEL="$1"
OUTPUT="/work/output/$LABEL"
mkdir -p "$OUTPUT"
if /app/pipeline.sh --device cpu --demucs-keep all --stem-model htdemucs --output "$OUTPUT" /work/input/test_mix.wav > "/work/${LABEL}_pipeline.log" 2>&1; then
  rc=0
else
  rc=$?
fi
tail -n 40 "/work/${LABEL}_pipeline.log"
exit $rc
EOF
chmod +x "$WORKDIR/run_pipeline.sh"

cat > "$WORKDIR/run_candidate.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
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
bash /work/run_pipeline.sh candidate
EOF
chmod +x "$WORKDIR/run_candidate.sh"

# -----------------------------------------------------------------------------
# Funcion para ejecutar el pipeline en un contenedor desechable
# -----------------------------------------------------------------------------
run_step() {
  local label="$1"
  shift
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
    bash "$@"
}

# -----------------------------------------------------------------------------
# Ejecutar las dos pasadas A/A y, en modo A/B, la candidata
# -----------------------------------------------------------------------------
START=$SECONDS

run_step "run1 (baseline)" /work/run_pipeline.sh run1
ok "Run1 completado"

run_step "run2 (control A/A)" /work/run_pipeline.sh run2
ok "Run2 completado"

if [[ "$MODE" == "AB" ]]; then
  run_step "candidate (pip install -U)" /work/run_candidate.sh
  ok "Candidata completada"
fi

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

abs_diff() {
  awk "BEGIN {d=($2)-($1); if(d<0)d=-d; printf \"%.3f\", d}"
}

lte() {
  awk "BEGIN {print (($1) <= ($2)) ? 1 : 0}"
}

OUT_DIR="$OUTPUT_DIR"

if [[ ! -d "$OUT_DIR/run1" || ! -d "$OUT_DIR/run2" ]]; then
  fail "no se encontraron los directorios de salida"
  exit 1
fi
if [[ "$MODE" == "AB" && ! -d "$OUT_DIR/candidate" ]]; then
  fail "no se encontro el directorio de salida de la candidata"
  exit 1
fi

# -----------------------------------------------------------------------------
# Tabla de comparacion
# -----------------------------------------------------------------------------
printf '\n'
printf '%s%s%s\n' "$C_BLD" '== Resultados ==' "$C_OFF"
printf '%-12s %10s %12s %12s %12s | %10s %12s %12s %12s | %10s %12s %12s %12s\n' \
  "pista" "dur_1" "bytes_1" "mean_1" "max_1" "dur_2" "bytes_2" "mean_2" "max_2" "dur_C" "mean_C" "max_C" "bytes_C"
printf '%s\n' '---------------------------------------------------------------------------------------------------------------------------------------------------------------------'

PASS=true
declare -A RUIDO UMBRAL NOTE_AA NOTE_AB

for stem in vocals drums bass other; do
  f1="$OUT_DIR/run1/${stem}.wav"
  f2="$OUT_DIR/run2/${stem}.wav"
  fC="$OUT_DIR/candidate/${stem}.wav"

  if [[ ! -f "$f1" || ! -f "$f2" ]]; then
    printf '%-12s %s\n' "$stem" "FALTA_EN_ALGUNA_CONFIGURACION"
    PASS=false
    continue
  fi

  read -r dur1 bytes1 mean1 max1 <<< "$(measure "$f1")"
  read -r dur2 bytes2 mean2 max2 <<< "$(measure "$f2")"

  if [[ "$MODE" == "AB" && -f "$fC" ]]; then
    read -r durC bytesC meanC maxC <<< "$(measure "$fC")"
  else
    durC="N/A"; bytesC="N/A"; meanC="N/A"; maxC="N/A"
  fi

  printf '%-12s %10s %12s %12s %12s | %10s %12s %12s %12s | %10s %12s %12s %12s\n' \
    "$stem" "$dur1" "$bytes1" "$mean1" "$max1" "$dur2" "$bytes2" "$mean2" "$max2" "$durC" "$meanC" "$maxC" "$bytesC"

  # Ruido de medicion (diferencia entre las dos pasadas A/A).
  ddur_aa="$(abs_diff "$dur1" "$dur2")"
  dmean_aa="$(abs_diff "$mean1" "$mean2")"
  dmax_aa="$(abs_diff "$max1" "$max2")"
  ruido="$(awk "BEGIN {m=$dmean_aa; if($dmax_aa>m)m=$dmax_aa; printf \"%.3f\", m}")"
  RUIDO[$stem]="$ruido"

  # Umbral: ruido con margen, nunca por debajo del minimo absoluto.
  umbral="$(awk "BEGIN {m=$ruido*$MARGIN; if(m<$MIN_THRESHOLD)m=$MIN_THRESHOLD; printf \"%.3f\", m}")"
  UMBRAL[$stem]="$umbral"

  # Veredicto A/A por pista.
  ok_aa=true
  note_aa=""
  if [[ "$mean1" == "NA" || "$mean2" == "NA" ]]; then
    ok_aa=false
    note_aa="error de medicion"
  elif [[ "$(awk "BEGIN {print ($mean1 < $SILENCE_THRESHOLD && $mean2 < $SILENCE_THRESHOLD) ? 1 : 0}")" -eq 1 ]]; then
    note_aa="N/S: pista silenciosa (<-60 dB)"
  else
    if [[ "$(lte "$ddur_aa" "0.001")" -ne 1 ]]; then
      ok_aa=false
      note_aa="duracion distinta en A/A"
    elif [[ "$(lte "$dmean_aa" "$umbral")" -ne 1 ]]; then
      ok_aa=false
      note_aa="ruido mean excede umbral"
    elif [[ "$(lte "$dmax_aa" "$umbral")" -ne 1 ]]; then
      ok_aa=false
      note_aa="ruido max excede umbral"
    fi
  fi
  NOTE_AA[$stem]="$note_aa"
  [[ "$ok_aa" == true ]] || PASS=false

  # Veredicto A/B por pista (solo si hay candidata).
  ok_ab=true
  note_ab=""
  if [[ "$MODE" == "AB" ]]; then
    if [[ ! -f "$fC" ]]; then
      ok_ab=false
      note_ab="falta pista en candidata"
    elif [[ "$mean1" == "NA" || "$meanC" == "NA" ]]; then
      ok_ab=false
      note_ab="error de medicion"
    else
      ddur_ab="$(abs_diff "$dur1" "$durC")"
      dmean_ab="$(abs_diff "$mean1" "$meanC")"
      dmax_ab="$(abs_diff "$max1" "$maxC")"
      if [[ "$(awk "BEGIN {print ($mean1 < $SILENCE_THRESHOLD && $meanC < $SILENCE_THRESHOLD) ? 1 : 0}")" -eq 1 ]]; then
        note_ab="N/S: pista silenciosa (<-60 dB)"
      elif [[ "$(lte "$ddur_ab" "0.001")" -ne 1 ]]; then
        ok_ab=false
        note_ab="duracion distinta"
      elif [[ "$(lte "$dmean_ab" "$umbral")" -ne 1 ]]; then
        ok_ab=false
        note_ab="mean excede ruido+margen"
      elif [[ "$(lte "$dmax_ab" "$umbral")" -ne 1 ]]; then
        ok_ab=false
        note_ab="max excede ruido+margen"
      fi
    fi
    [[ "$ok_ab" == true ]] || PASS=false
  fi
  NOTE_AB[$stem]="$note_ab"
done

# -----------------------------------------------------------------------------
# Resumen de veredictos por pista
# -----------------------------------------------------------------------------
printf '\n'
printf '%s%s%s\n' "$C_BLD" '== Veredicto ==' "$C_OFF"
printf '%-12s %12s %12s | %s\n' "pista" "ruido" "umbral" "estado / nota"
printf '%s\n' '--------------------------------------------------------------------------------'

for stem in vocals drums bass other; do
  umbral="${UMBRAL[$stem]:-NA}"
  ruido="${RUIDO[$stem]:-NA}"
  note_aa="${NOTE_AA[$stem]:-}"
  note_ab="${NOTE_AB[$stem]:-}"

  if [[ "$MODE" == "AA" ]]; then
    if [[ -n "$note_aa" ]]; then
      estado="$note_aa"
    else
      estado="A/A OK (ruido <= umbral)"
    fi
  else
    if [[ -n "$note_ab" ]]; then
      estado="$note_ab"
    elif [[ -n "$note_aa" && "$note_aa" == N/S* ]]; then
      estado="N/S en baseline"
    else
      estado="A/B OK (diferencia <= ruido+margen)"
    fi
  fi

  printf '%-12s %12s %12s | %s\n' "$stem" "$ruido" "$umbral" "$estado"
done

ELAPSED=$((SECONDS - START))
printf '\nTiempo total: %dm %ds\n' $((ELAPSED / 60)) $((ELAPSED % 60))

# -----------------------------------------------------------------------------
# Resultado y limpieza
# -----------------------------------------------------------------------------
if $PASS; then
  ok "VALIDACION ACEPTADA"
  rm -rf "$WORKDIR"
  exit 0
else
  fail "VALIDACION RECHAZADA: la candidata altera la salida mas alla del ruido de medicion"
  warn "Workdir conservado para inspeccion: $WORKDIR"
  exit 1
fi
