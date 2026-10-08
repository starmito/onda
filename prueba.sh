#!/bin/bash
# prueba.sh — Banco de PRUEBAS de Onda (contenedor `onda-prueba`, puerto 3010)
#
# Qué hace:
#   1) Construye el árbol de trabajo actual como imagen `onda:prueba`  (SIN tags de git)
#   2) Recrea SOLO el contenedor del banco de pruebas
#   3) Espera al health del 3010 y lo verifica
#
# Qué NO hace (a propósito):
#   · NUNCA toca producción (`onda`, puerto 3000) ni su imagen `onda:v3.5.17`
#   · NUNCA crea tags de git ni toca `main`
#   · NUNCA usa `deploy.sh`
#   · NUNCA lanza nada en CPU: el banco de pruebas corre con GPU (`runtime: nvidia`)
#
# Uso:
#   ./prueba.sh              construir + recrear + verificar health
#   ./prueba.sh --build      solo construir la imagen
#   ./prueba.sh --health     solo verificar que el banco de pruebas responde
#   ./prueba.sh --logs       ver las últimas líneas del banco de pruebas
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BANCO="/home/starmito/.docker/onda-prueba"     # proyecto de pruebas gestionado por Arcane
PUERTO=3010
IMAGEN="onda:prueba"
ESPERA_MAX=180                                  # segundos de espera al health

rojo()  { printf '\033[31m%s\033[0m\n' "$*"; }
verde() { printf '\033[32m%s\033[0m\n' "$*"; }
info()  { printf '\033[36m%s\033[0m\n' "$*"; }

health_json() { curl -s -m 10 "http://127.0.0.1:${PUERTO}/api/health" 2>/dev/null || true; }

verificar_health() {
    local t=0
    info "⏳ Esperando al health del banco de pruebas (:${PUERTO}) …"
    while [ "$t" -lt "$ESPERA_MAX" ]; do
        local h
        h="$(health_json)"
        if [ -n "$h" ] && echo "$h" | grep -q '"ok":true'; then
            echo "$h" | python3 -c '
import sys, json
d = json.load(sys.stdin)
g, o = d.get("gpu", {}), d.get("onnxruntime", {})
print("\n  versión app      :", d.get("backend", {}).get("version"))
print("  GPU              :", g.get("type"), "|", g.get("free_mb"), "MiB libres | torch usa la GPU:", g.get("usable_by_torch"))
print("  onnxruntime      :", o.get("version"), "|", (o.get("providers") or ["?"])[0])
print("  disco libre      :", d.get("disk", {}).get("detail"))
' 2>/dev/null || echo "$h"
            verde "✅ Banco de pruebas OK en http://192.168.1.87:${PUERTO}"
            return 0
        fi
        sleep 5; t=$((t + 5))
    done
    rojo "❌ El banco de pruebas no respondió en ${ESPERA_MAX}s. Últimas líneas:"
    docker logs --tail 25 onda-prueba 2>&1 | sed 's/^/    /'
    return 1
}

case "${1:-}" in
    --health) verificar_health; exit $? ;;
    --logs)   docker logs --tail 60 onda-prueba 2>&1; exit 0 ;;
    --build)  SOLO_BUILD=1 ;;
    "")       SOLO_BUILD=0 ;;
    *)        rojo "Opción no reconocida: $1"; sed -n '16,22p' "$0"; exit 2 ;;
esac

# ── 1) Construir la imagen del banco de pruebas ────────────────────────────
cd "$REPO"
# La versión del banco se marca con sufijo para no confundirla con producción
ONDAP_VERSION="$(cat VERSION 2>/dev/null | tr -d '[:space:]')-prueba"
GUI_VERSION="$ONDAP_VERSION"
export ONDAP_VERSION GUI_VERSION ONDA_IMAGE_TAG=prueba

info "🔨 Construyendo ${IMAGEN} desde el árbol de trabajo (versión base: ${ONDAP_VERSION}) …"
info "   (no se crean tags de git; producción no se toca)"
docker compose -f docker-compose.yml -f docker-compose.cuda.yml build

if [ "$SOLO_BUILD" = "1" ]; then
    verde "✅ Imagen ${IMAGEN} construida."
    docker image inspect "$IMAGEN" --format '   {{.Id}}  {{.Created}}' 2>/dev/null | head -1
    exit 0
fi

# ── 2) Recrear SOLO el contenedor del banco de pruebas ─────────────────────
info "♻️  Recreando el contenedor del banco de pruebas (proyecto onda-prueba) …"
cd "$BANCO"
docker compose -p onda-prueba up -d --force-recreate

# ── 3) Verificar ───────────────────────────────────────────────────────────
verificar_health
echo
info "ℹ️  Producción (puerto 3000) no se ha tocado:"
docker ps --filter name=^onda$ --format '   {{.Names}} | {{.Image}} | {{.Status}}' || true
