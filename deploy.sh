#!/bin/bash
set -euo pipefail

# Repara los directorios montados como bind volumes: crea los que falten y,
# si detecta que el owner no coincide con el usuario de referencia, corrige la
# propiedad recursivamente. NUNCA borra el contenido.
#
# Uso:
#   repair_bind_dir_permissions "$BIND_DIRS"
#
# Variables de entorno (solo para tests):
#   REPAIR_COMPARE_UID  - fuerza un UID de comparacion distinto para poder
#                         probar la rama de deteccion sin crear dirs root-owned.
repair_bind_dir_permissions() {
    local dirs="$1"
    local target_uid="${2:-$(id -u)}"
    local target_gid="${3:-$(id -g)}"
    local compare_uid="${REPAIR_COMPARE_UID:-$target_uid}"
    local actual_uid

    for dir in $dirs; do
        mkdir -p "$dir"
        actual_uid=$(stat -c '%u' "$dir" 2>/dev/null || echo 0)
        if [ "$actual_uid" != "$compare_uid" ]; then
            # Intentar sin sudo primero (suficiente cuando ya tenemos permisos
            # o en entornos de test); si falla, usar sudo para directorios que
            # pertenezcan a root tras ejecuciones previas con privilegios.
            if ! chown -R "${target_uid}:${target_gid}" "$dir" 2>/dev/null; then
                sudo chown -R "${target_uid}:${target_gid}" "$dir"
            fi
        fi
    done
}

# Evitar que se ejecute el cuerpo del deploy cuando el script se sourcea
# (por ejemplo, desde los tests) para poder reutilizar la funcion.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then

cd "$(dirname "$0")"

echo "🔍 Detectando hardware..."
GPU=$(bash onda/detect_gpu.sh)
echo "🎯 Hardware detectado: $GPU"

# Resolver versiones desde los tags de git (misma lógica que build.sh).
# Es necesario exportarlas porque docker-compose.yml las inyecta como ARG
# en build time y .dockerignore excluye .git, por lo que el contenedor no
# puede calcularlas por sí mismo.
source ./build.sh --version
export ONDAP_VERSION GUI_VERSION

# Regenerar los ficheros de versión que consumen el health check y el build
# de la imagen, para que coincidan con la versión resuelta por los tags.
generate_version_files

# Directorios montados como bind volumes (deben pertenecer al usuario host)
BIND_DIRS="data/input data/output data/input_rubberband data/daw-data data/config data/logs data/models"

repair_bind_dir_permissions "$BIND_DIRS"

# Asegurar que el script del pipeline sea ejecutable en el host (y por tanto
# dentro del contenedor, ya que se monta como bind volume).
chmod +x pipeline.sh

case $GPU in
  cuda)
    echo "🚀 Desplegando con aceleración NVIDIA CUDA..."
    docker compose -f docker-compose.yml -f docker-compose.cuda.yml up -d --build
    ;;
  *)
    echo "🚀 Desplegando en modo CPU..."
    docker compose up -d --build
    ;;
esac

echo "✅ Onda desplegado en http://localhost:${ONDA_PORT:-3000}"

fi
