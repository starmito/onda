#!/usr/bin/env bash
#
# tools/check-deps.sh — Comprueba que las dependencias Python de Onda estan TODAS fijadas con
# '==' y que coinciden con el estado REALMENTE instalado (requirements.lock).
#
# Uso:
#   tools/check-deps.sh                  # verifica los 3 .txt contra requirements.lock
#   tools/check-deps.sh onda:v3.4.15     # ademas, compara el freeze de esa imagen con requirements.lock
#
# Comprueba:
#   (a) que ninguna linea de los tres requirements quede sin '=='
#   (b) que cada paquete declarado coincida con la version de requirements.lock (cuando aparezca alli)
#   (c) informa de las diferencias en vez de fallar en silencio
#
# Codigos de salida:
#   0 = sin errores (puede haber avisos/informacion)
#   1 = hay errores (linea sin '==', o version declarada != instalada, o freeze distinto del lock)
#
# Los nombres de paquete se normalizan segun PEP 503 (minusculas, runs de -_. -> -) para que
# p.ej. `PyYAML`, `pyyaml` o `torch_audiomentations`/`torch-audiomentations` comparen igual.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCK_FILE="$REPO_ROOT/requirements.lock"
REQ_FILES=(
  "requirements-common.txt"
  "requirements-docker.txt"
  "requirements-docker-amd.txt"
)
IMG="${1:-}"

if [[ -t 1 ]]; then
  C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'; C_BLD=$'\033[1m'; C_OFF=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_YEL=""; C_BLD=""; C_OFF=""
fi

errors=0
warnings=0
infos=0

ok()   { printf '%s[ ok ]%s %s\n' "$C_GRN" "$C_OFF" "$*"; }
warn() { printf '%s[warn]%s %s\n' "$C_YEL" "$C_OFF" "$*"; warnings=$((warnings + 1)); }
err()  { printf '%s[FAIL]%s %s\n' "$C_RED" "$C_OFF" "$*"; errors=$((errors + 1)); }
info() { printf '%s[info]%s %s\n' "$C_BLD" "$C_OFF" "$*"; infos=$((infos + 1)); }

# PEP 503: nombre normalizado (minusculas, runs de -_. -> -)
norm_name() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[-_.][-_.]*/-/g'; }

# quita espacios a ambos lados
trim() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
}

printf '%s== check-deps: requirements vs estado instalado ==%s\n' "$C_BLD" "$C_OFF"

if [[ ! -f "$LOCK_FILE" ]]; then
  err "no existe $LOCK_FILE (es el inventario de referencia; sin el no se puede comparar)"
  exit 1
fi

# ---------------------------------------------------------------- requirements.lock
declare -A LOCK_VER=()
lock_count=0
lineno=0
while IFS= read -r raw || [[ -n "$raw" ]]; do
  lineno=$((lineno + 1))
  line="${raw%$'\r'}"
  line="$(trim "$line")"
  [[ -z "$line" ]] && continue
  case "$line" in \#*) continue ;; esac
  if [[ "$line" == *"=="* ]]; then
    n="$(trim "${line%%==*}")"
    v="$(trim "${line#*==}")"
    LOCK_VER["$(norm_name "$n")"]="$v"
    lock_count=$((lock_count + 1))
  else
    warn "requirements.lock:$lineno no es 'nombre==version' y se ignora: $line"
  fi
done < "$LOCK_FILE"

printf '\n'
ok "requirements.lock: $lock_count paquetes leidos"

# ---------------------------------------------------------------- requirements (a) y (b)
declare -A DECLARED_IN=()
total_declared=0
total_unpinned=0

for rel in "${REQ_FILES[@]}"; do
  f="$REPO_ROOT/$rel"
  printf '\n%s-- %s --%s\n' "$C_BLD" "$rel" "$C_OFF"
  if [[ ! -f "$f" ]]; then
    err "$rel: el fichero no existe"
    continue
  fi

  file_declared=0
  file_unpinned=0
  file_mismatch=0
  file_absent=0
  lineno=0

  while IFS= read -r raw || [[ -n "$raw" ]]; do
    lineno=$((lineno + 1))
    line="${raw%$'\r'}"
    line="$(trim "$line")"
    [[ -z "$line" ]] && continue
    case "$line" in \#*) continue ;; esac

    # (a) toda linea de paquete debe llevar '=='
    if [[ "$line" != *"=="* ]]; then
      err "$rel:$lineno  SIN '==' (no fijado): $line"
      file_unpinned=$((file_unpinned + 1))
      total_unpinned=$((total_unpinned + 1))
      continue
    fi

    n="$(trim "${line%%==*}")"
    v="$(trim "${line#*==}")"
    key="$(norm_name "$n")"
    file_declared=$((file_declared + 1))
    total_declared=$((total_declared + 1))
    DECLARED_IN["$key"]="${DECLARED_IN[$key]:-}${rel} "

    # (b) comparar con lo instalado, solo si el paquete aparece en el lock
    if [[ -z "${LOCK_VER[$key]:-}" ]]; then
      info "$rel:$lineno  '$n==$v' -> NO aparece en requirements.lock (instalado fuera de site-packages, o no instalado)"
      file_absent=$((file_absent + 1))
      continue
    fi

    lockv="${LOCK_VER[$key]}"
    if [[ "$v" == "$lockv" ]]; then
      :
    elif [[ "${lockv%%+*}" == "${v%%+*}" ]]; then
      info "$rel:$lineno  '$n==$v' -> instalado '$lockv' (difiere solo en el sufijo local '+...'; esperado para torch/torchaudio/torchvision)"
    else
      err "$rel:$lineno  '$n==$v' -> INSTALADO: '$n==$lockv'  MISMATCH"
      file_mismatch=$((file_mismatch + 1))
    fi
  done < "$f"

  if [[ $file_unpinned -gt 0 || $file_mismatch -gt 0 ]]; then
    printf '%s      %s: %d declarados, %d sin fijar, %d con version distinta de la instalada%s\n' \
      "$C_RED" "$rel" "$file_declared" "$file_unpinned" "$file_mismatch" "$C_OFF"
  else
    ok "$rel: $file_declared paquetes declarados, todos con '==', todos coherentes con requirements.lock"
  fi
done

# ---------------------------------------------------------------- extras: solo en el lock
extras=()
for key in "${!LOCK_VER[@]}"; do
  [[ -z "${DECLARED_IN[$key]:-}" ]] && extras+=("$key")
done

printf '\n%s-- Resumen --%s\n' "$C_BLD" "$C_OFF"
info "requirements.lock: $lock_count paquetes instalados (referencia)"
info "declarados en los 3 requirements: $total_declared ($total_unpinned sin fijar)"
info "solo en el lock (dependencias transitivas, no declaradas a mano): ${#extras[@]}"

# ---------------------------------------------------------------- (opcional) freeze de una imagen
if [[ -n "$IMG" ]]; then
  printf '\n%s-- Imagen: %s --%s\n' "$C_BLD" "$IMG" "$C_OFF"
  TMPD="$(mktemp -d)"
  trap 'rm -rf "$TMPD"' EXIT

  if ! docker image inspect "$IMG" >/dev/null 2>&1; then
    err "la imagen '$IMG' no existe en esta maquina (docker image inspect ha fallado)"
  elif docker run --rm --entrypoint bash "$IMG" -lc 'python3 -m pip freeze' > "$TMPD/raw" 2> "$TMPD/raw.err"; then
    LC_ALL=C sort -o "$TMPD/freeze" "$TMPD/raw"
    grep -vE '^[[:space:]]*(#|$)' "$LOCK_FILE" | LC_ALL=C sort > "$TMPD/lock"

    printf '\n'
    if diff -u "$TMPD/lock" "$TMPD/freeze" > "$TMPD/diff.txt"; then
      ok "el freeze de '$IMG' es IDENTICO a requirements.lock ($(grep -c '==' "$TMPD/freeze") paquetes)"
    else
      err "el freeze de '$IMG' DIFIERE de requirements.lock (- lock / + imagen):"
      sed 's/^/      /' "$TMPD/diff.txt"
      printf '%s      Si la imagen nueva es la buena: regenerar requirements.lock y revisar el diff.%s\n' \
        "$C_YEL" "$C_OFF"
    fi
  else
    err "no se ha podido obtener el freeze de '$IMG':"
    sed 's/^/      /' "$TMPD/raw.err" 2>/dev/null || true
  fi
fi

# ---------------------------------------------------------------- resultado final
printf '\n'
if [[ $errors -gt 0 ]]; then
  printf '%sRESULTADO: %d error(es), %d aviso(s), %d info%s\n' "$C_RED" "$errors" "$warnings" "$infos" "$C_OFF"
  exit 1
fi
printf '%sRESULTADO: OK — 0 errores, %d aviso(s), %d info%s\n' "$C_GRN" "$warnings" "$infos" "$C_OFF"
exit 0
