#!/usr/bin/env bash
# Onda — centralized build script with per-service automatic versioning from git tags.
#
# Tags:
#   onda-vX.Y.Z  → backend Go + Python pipeline
#   gui-vX.Y.Z   → frontend Svelte
#
# Usage:
#   ./build.sh              native build (backend + frontend + python checks)
#   ./build.sh --docker     build Docker images via docker compose
#   ./build.sh --version    print resolved versions and exit

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

# ── Resolve versions from git tags ───────────────────────────────────────────
# Prefer a tag matching the service prefix; if none exists, fall back to the
# most recent tag available.  Strip the service prefix so the injected version
# is always vX.Y.Z.
ONDAP_VERSION_RAW="$(git describe --tags --match 'onda-*' --abbrev=0 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo 'unknown')"
GUI_VERSION_RAW="$(git describe --tags --match 'gui-*' --abbrev=0 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo 'unknown')"

ONDAP_VERSION="${ONDAP_VERSION_RAW#onda-}"
GUI_VERSION="${GUI_VERSION_RAW#gui-}"

export ONDAP_VERSION GUI_VERSION

# ── Print versions ───────────────────────────────────────────────────────────
print_versions() {
    echo "ONDAP_VERSION (backend + pipeline): $ONDAP_VERSION"
    echo "GUI_VERSION   (frontend Svelte):    $GUI_VERSION"
}

# ── Generate version files ───────────────────────────────────────────────────
generate_version_files() {
    # Python pipeline version module
    printf '__version__ = "%s"\n' "$ONDAP_VERSION" > onda/_version.py

    # Keep pyproject.toml in sync (PEP 440, no leading "v" or prefix)
    PY_VERSION="${ONDAP_VERSION#v}"
    if [ "$PY_VERSION" != "unknown" ]; then
        sed -i "s/^version = \"[^\"]*\"/version = \"$PY_VERSION\"/" pyproject.toml
    fi

    # Native VERSION file used by backend fallback and health endpoint
    printf '%s\n' "$ONDAP_VERSION" > VERSION
}

# ── Native build ─────────────────────────────────────────────────────────────
native_build() {
    echo "🔨 Onda native build"
    print_versions
    generate_version_files

    # Backend Go
    echo "  → Building Go backend..."
    (
        cd backend
        GOTOOLCHAIN=go1.26.0 go mod tidy
        CGO_ENABLED=0 GOOS=linux go build \
            -ldflags "-X github.com/starmito/onda/internal/api.Version=$ONDAP_VERSION" \
            -o /tmp/onda-backend ./cmd/onda/
    )

    # Frontend Svelte
    echo "  → Building frontend..."
    (
        cd frontend
        if [ ! -d node_modules ]; then
            npm ci
        fi
        VITE_ONDA_VERSION="$GUI_VERSION" npm run build
        printf '%s\n' "$GUI_VERSION" > dist/VERSION
    )

    # Python syntax check
    echo "  → Checking Python syntax..."
    python3 -m py_compile onda/_version.py onda/__init__.py onda/cli.py

    echo "✅ Native build complete"
    echo "   Backend binary: /tmp/onda-backend"
    echo "   Frontend dist:  frontend/dist"
}

# ── Docker build ─────────────────────────────────────────────────────────────
docker_build() {
    echo "🐳 Onda Docker build"
    print_versions

    # docker compose will use the ARG values passed below.
    docker compose build \
        --build-arg "ONDAP_VERSION=$ONDAP_VERSION" \
        --build-arg "GUI_VERSION=$GUI_VERSION"

    echo "✅ Docker build complete"
}

# ── Main ─────────────────────────────────────────────────────────────────────
case "${1:-}" in
    --version|-v)
        print_versions
        ;;
    --docker|-d)
        docker_build
        ;;
    ""|--native)
        native_build
        ;;
    --help|-h)
        sed -n '2,10p' "$0"
        ;;
    *)
        echo "Unknown option: $1" >&2
        echo "Usage: $0 [--docker|--version|--help]" >&2
        exit 1
        ;;
esac
