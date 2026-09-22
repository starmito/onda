#!/bin/bash
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="${ONDA_ROOT:-$DEPLOY_DIR}"

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

# ── Release invariants ───────────────────────────────────────────────────────
# Requires build.sh functions to be available: source it before calling this.
check_release_invariants() {
    read_version
    if ! check_version_files; then
        echo "" >&2
        echo "Diff of versioned files against the working tree:" >&2
        git -C "$ROOT" diff -- VERSION onda/_version.py pyproject.toml frontend/package.json 2>/dev/null || true
        echo "" >&2
        echo "ERROR: version files do not match VERSION ($ONDAP_VERSION). Deploy never modifies versioned files." >&2
        echo "Fix the files above or run 'bash build.sh --write-version' manually, then commit." >&2
        return 1
    fi

    if [ "${ONDA_ALLOW_UNTAGGED:-0}" = "1" ]; then
        echo "⚠️  ONDA_ALLOW_UNTAGGED=1: allowing deploy without release tag. Image will be tagged as ${ONDAP_VERSION}-dev" >&2
        IMAGE_TAG="${ONDAP_VERSION}-dev"
        export IMAGE_TAG
        return 0
    fi

    local tag="onda-$ONDAP_VERSION"
    if ! git -C "$ROOT" rev-parse --verify "$tag" >/dev/null 2>&1; then
        echo "ERROR: release tag '$tag' does not exist." >&2
        echo "Create it with: git -C \"$ROOT\" tag -a \"$tag\" -m \"Release $ONDAP_VERSION\" && git push origin \"$tag\"" >&2
        echo "Or set ONDA_ALLOW_UNTAGGED=1 for a development build." >&2
        return 1
    fi

    local tag_sha head_sha
    tag_sha="$(git -C "$ROOT" rev-list -n1 "$tag")"
    head_sha="$(git -C "$ROOT" rev-parse HEAD)"
    if ! git -C "$ROOT" merge-base --is-ancestor "$tag_sha" "$head_sha" 2>/dev/null; then
        echo "ERROR: release tag '$tag' ($tag_sha) is not an ancestor of HEAD ($head_sha)." >&2
        echo "The tag must point to a commit reachable from the current HEAD." >&2
        return 1
    fi

    IMAGE_TAG="$ONDAP_VERSION"
    export IMAGE_TAG
}

# Verify that the built image contains every path the pipeline invokes.
# Fails the deploy if a script/binary is missing, so silent drift is impossible.
check_image_pipeline_paths() {
    local image="${1:-onda:${ONDA_IMAGE_TAG:-$ONDAP_VERSION}}"
    docker run --rm --entrypoint '' "$image" bash -c '
set -euo pipefail
paths=(
  /app/pipeline.sh
  /app/tools/progress_tracker.py
  /app/tools/demucs_worker.py
  /app/inference_universal.py
  /app/inference_mdx.py
  /app/inference_scnet.py
  /app/inference_onnx.py
  /app/inference_polarformer.py
  /app/keydetect.py
  /app/onda/detect_gpu.sh
  /usr/local/bin/detect_gpu.sh
  /usr/local/bin/onda-backend
)
missing=()
for p in "${paths[@]}"; do
  if [ ! -e "$p" ]; then missing+=("$p"); fi
done
for b in python3 rubberband demucs; do
  if ! command -v "$b" >/dev/null 2>&1; then missing+=("$b"); fi
done
if [ "${#missing[@]}" -gt 0 ]; then
  echo "ERROR: built image is missing required pipeline paths/binaries:" >&2
  for m in "${missing[@]}"; do echo "  - $m" >&2; done
  exit 1
fi
echo "All required pipeline paths present"
'
}

# Evitar que se ejecute el cuerpo del deploy cuando el script se sourcea
# (por ejemplo, desde los tests) para poder reutilizar la funcion.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then

cd "$ROOT"

echo "🔍 Detectando hardware..."
GPU=$(bash onda/detect_gpu.sh)
echo "🎯 Hardware detectado: $GPU"

# Resolver versiones desde VERSION (misma logica que build.sh).
# Es necesario exportarlas porque docker-compose.yml las inyecta como ARG
# en build time y .dockerignore excluye .git, por lo que el contenedor no
# puede calcularlas por si mismo.
source "$DEPLOY_DIR/build.sh" --version
export ONDAP_VERSION GUI_VERSION

# Comprobar invariantes de release ANTES de tocar cualquier fichero o imagen.
# Esto evita que un despliegue modifique ficheros versionados o etiquete mal.
check_release_invariants

# La etiqueta de la imagen puede diferenciar builds release vs desarrollo.
export ONDA_IMAGE_TAG="${IMAGE_TAG:-$ONDAP_VERSION}"

# Directorios montados como bind volumes (deben pertenecer al usuario host)
BIND_DIRS="data/input data/output data/input_rubberband data/daw-data data/config data/logs data/models"

repair_bind_dir_permissions "$BIND_DIRS"

# Asegurar que el script del pipeline sea ejecutable en el host (y por tanto
# dentro del contenedor, ya que se monta como bind volume).
chmod +x pipeline.sh

case $GPU in
  cuda)
    echo "🚀 Desplegando con aceleracion NVIDIA CUDA..."
    docker compose -f docker-compose.yml -f docker-compose.cuda.yml up -d --build
    ;;
  *)
    echo "🚀 Desplegando en modo CPU..."
    docker compose up -d --build
    ;;
esac

# Verificar que la imagen construida lleva el tag esperado.
if ! docker image inspect "onda:$ONDA_IMAGE_TAG" >/dev/null 2>&1; then
    echo "ERROR: image onda:$ONDA_IMAGE_TAG was not built" >&2
    exit 1
fi

# Verificar que la imagen contiene todo lo que pipeline.sh ejecuta.
if ! check_image_pipeline_paths "onda:$ONDA_IMAGE_TAG"; then
    echo "ERROR: built image onda:$ONDA_IMAGE_TAG is incomplete" >&2
    exit 1
fi

# Verificar que el contenedor arrancado reporta la misma version en las tres capas.
echo "🔍 Verificando version del despliegue..."
HEALTH=""
for _ in $(seq 1 30); do
    HEALTH=$(curl -s "http://127.0.0.1:${ONDA_PORT:-3000}/api/health" || true)
    if [ -n "$HEALTH" ]; then
        break
    fi
    sleep 1
done

if [ -z "$HEALTH" ]; then
    echo "ERROR: health endpoint did not become available" >&2
    exit 1
fi

DEPLOYED_VERSION=$(echo "$HEALTH" | python3 -c 'import sys, json; print(json.load(sys.stdin).get("version", "unknown"))' 2>/dev/null || echo "unknown")
if [ "$DEPLOYED_VERSION" != "$ONDAP_VERSION" ]; then
    echo "ERROR: deployed version mismatch. Expected $ONDAP_VERSION, got $DEPLOYED_VERSION" >&2
    exit 1
fi

echo "✅ Onda $ONDAP_VERSION desplegado en http://localhost:${ONDA_PORT:-3000}"

fi
