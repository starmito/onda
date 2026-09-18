#!/bin/bash
set -euo pipefail

# Test para deploy.sh::repair_bind_dir_permissions
#
# Objetivo: verificar que la reparacion de permisos de bind dirs:
#   - crea directorios que no existen,
#   - preserva el contenido existente,
#   - corrige el owner cuando la deteccion indica que no coincide,
#   - es idempotente.
#
# Limitacion: sin sudo no podemos crear directorios realmente root-owned, asi
# que usamos la variable REPAIR_COMPARE_UID para forzar la rama de deteccion.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_SH="$REPO_ROOT/deploy.sh"

# Cargar solo la funcion; el guard de BASH_SOURCE evita que se ejecute el deploy.
# shellcheck source=deploy.sh
source "$DEPLOY_SH"

TEST_DIR="$REPO_ROOT/daw-data/tmp/deploy-perms-test"
TEST_FILE="$TEST_DIR/preserved-file.txt"

cleanup() {
    rm -rf "$TEST_DIR"
}

trap cleanup EXIT

CURRENT_UID=$(id -u)
CURRENT_GID=$(id -g)

# 1) Creacion de directorio inexistente
if [ -d "$TEST_DIR" ]; then
    rm -rf "$TEST_DIR"
fi
repair_bind_dir_permissions "$TEST_DIR"
if [ ! -d "$TEST_DIR" ]; then
    echo "FAIL: el directorio no se creo" >&2
    exit 1
fi
if [ "$(stat -c '%u:%g' "$TEST_DIR")" != "$CURRENT_UID:$CURRENT_GID" ]; then
    echo "FAIL: el owner tras crear no es el usuario actual" >&2
    exit 1
fi

# 2) Rama de deteccion de owner distinto (sin sudo, via hook)
#    La funcion solo hace chown al usuario actual, asi que debe conservar el
#    fichero y dejar el owner correcto.
echo "datos de prueba" > "$TEST_FILE"
REPAIR_COMPARE_UID=99999 repair_bind_dir_permissions "$TEST_DIR" "$CURRENT_UID" "$CURRENT_GID"
if [ ! -f "$TEST_FILE" ]; then
    echo "FAIL: el fichero se borro durante la reparacion forzada" >&2
    exit 1
fi
if [ "$(stat -c '%u:%g' "$TEST_DIR")" != "$CURRENT_UID:$CURRENT_GID" ]; then
    echo "FAIL: el owner tras la reparacion forzada no es el usuario actual" >&2
    exit 1
fi
if [ "$(cat "$TEST_FILE")" != "datos de prueba" ]; then
    echo "FAIL: el contenido del fichero cambio durante la reparacion" >&2
    exit 1
fi

# 3) Idempotencia: segunda ejecucion con owner correcto no altera nada
repair_bind_dir_permissions "$TEST_DIR"
if [ ! -f "$TEST_FILE" ]; then
    echo "FAIL: el fichero se borro en la pasada idempotente" >&2
    exit 1
fi
if [ "$(stat -c '%u:%g' "$TEST_DIR")" != "$CURRENT_UID:$CURRENT_GID" ]; then
    echo "FAIL: el owner cambio en la pasada idempotente" >&2
    exit 1
fi
if [ "$(cat "$TEST_FILE")" != "datos de prueba" ]; then
    echo "FAIL: el contenido del fichero cambio en la pasada idempotente" >&2
    exit 1
fi

# 4) Multiples directorios a la vez (formato usado en produccion)
TEST_DIR_A="$REPO_ROOT/daw-data/tmp/deploy-perms-test-a"
TEST_DIR_B="$REPO_ROOT/daw-data/tmp/deploy-perms-test-b"
rm -rf "$TEST_DIR_A" "$TEST_DIR_B"
trap 'rm -rf "$TEST_DIR" "$TEST_DIR_A" "$TEST_DIR_B"' EXIT
repair_bind_dir_permissions "$TEST_DIR_A $TEST_DIR_B"
if [ ! -d "$TEST_DIR_A" ] || [ ! -d "$TEST_DIR_B" ]; then
    echo "FAIL: no se crearon todos los directorios del listado" >&2
    exit 1
fi
if [ "$(stat -c '%u:%g' "$TEST_DIR_A")" != "$CURRENT_UID:$CURRENT_GID" ]; then
    echo "FAIL: owner incorrecto en directorio A" >&2
    exit 1
fi
if [ "$(stat -c '%u:%g' "$TEST_DIR_B")" != "$CURRENT_UID:$CURRENT_GID" ]; then
    echo "FAIL: owner incorrecto en directorio B" >&2
    exit 1
fi

echo "PASS: todas las comprobaciones de deploy-perms pasaron"
