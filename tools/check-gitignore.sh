#!/usr/bin/env bash
#
# tools/check-gitignore.sh — Guardián de higiene de gitignore.
#
# Asegura que los directorios de trabajo internos no vuelvan a quedar bajo
# control de versiones (p. ej. tras un `git add -f`).
#
# Uso:
#   tools/check-gitignore.sh
#
# Códigos de salida:
#   0 = todo correcto
#   1 = hay rutas ignoradas que siguen trackeadas

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -t 1 ]]; then
  C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_BLD=$'\033[1m'; C_OFF=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_BLD=""; C_OFF=""
fi

errors=0

ok()  { printf '%s[OK]%s   %s\n' "$C_GRN" "$C_OFF" "$*"; }
fail() { printf '%s[FAIL]%s %s\n' "$C_RED" "$C_OFF" "$*"; errors=$((errors + 1)); }
info() { printf '%s[INFO]%s %s\n' "$C_BLD" "$C_OFF" "$*"; }

printf '%s== check-gitignore: higiene de rutas ignoradas ==%s\n' "$C_BLD" "$C_OFF"

# ---------------------------------------------------------------- .hermes/
# Directorio de trabajo interno: planes, informes y temporales de agentes.
info 'Comprobando que .hermes/ no tiene ficheros trackeados'

cd "$REPO_ROOT"
tracked_hermes=$(git ls-files .hermes/ 2>/dev/null || true)

if [[ -z "$tracked_hermes" ]]; then
  ok '.hermes/ -> ningun fichero bajo control de versiones'
else
  fail '.hermes/ -> hay ficheros trackeados (usar git rm --cached y dejarlos en .gitignore):'
  printf '%s\n' "$tracked_hermes" | sed 's/^/       /'
fi

# ---------------------------------------------------------------- resumen
printf '\n'
if [[ $errors -gt 0 ]]; then
  printf '%sRESULTADO: %d fallo(s)%s\n' "$C_RED" "$errors" "$C_OFF"
  exit 1
fi
printf '%sRESULTADO: OK - %d fallos%s\n' "$C_GRN" "$errors" "$C_OFF"
exit 0
