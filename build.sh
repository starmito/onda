#!/usr/bin/env bash
# Onda — centralized build script with per-service automatic versioning.
#
# Version policy:
#   The VERSION file is the single source of truth for the image / package
#   version.  Git tags are used only as a fallback when VERSION is missing or
#   does not contain a valid vX.Y.Z string.
#
#   Why: git describe returns the nearest reachable tag.  If a release tag is
#   placed on a merge commit in main that is not an ancestor of the current
#   branch, describe falls back to an older tag (e.g. v3.4.13 instead of
#   v3.4.14).  Using VERSION avoids that class of error entirely.
#
# Tags (kept for cross-checking and fallback):
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

# ── Version helpers ──────────────────────────────────────────────────────────
read_version_file() {
    local file="$1"
    if [ -f "$file" ]; then
        tr -d '[:space:]' < "$file"
    fi
}

is_valid_version() {
    [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

# Resolve a version using VERSION as the canonical source, with git tags as
# fallback.  $1 = service prefix (onda/gui), $2 = warning label.
resolve_version() {
    local prefix="$1"
    local label="$2"
    local file_version tag_version

    file_version="$(read_version_file VERSION)"

    if is_valid_version "$file_version"; then
        # Warn if the latest reachable tag disagrees, but keep VERSION.
        tag_version="$(git describe --tags --match "${prefix}-*" --abbrev=0 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo '')"
        tag_version="${tag_version#${prefix}-}"
        if [ -n "$tag_version" ] && [ "$tag_version" != "$file_version" ]; then
            echo "⚠️  Warning: VERSION=${file_version} but latest reachable ${prefix}-* tag is ${prefix}-${tag_version}; using VERSION" >&2
        fi
        echo "$file_version"
        return
    fi

    if [ -z "$file_version" ]; then
        echo "⚠️  Warning: VERSION file missing or empty; falling back to git tags for ${label}" >&2
    else
        echo "⚠️  Warning: VERSION='${file_version}' is not a valid vX.Y.Z string; falling back to git tags for ${label}" >&2
    fi

    tag_version="$(git describe --tags --match "${prefix}-*" --abbrev=0 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo 'unknown')"
    tag_version="${tag_version#${prefix}-}"
    echo "$tag_version"
}

# Cross-check the three version files that must stay in sync.
check_version_files_consistent() {
    local expected="$1"
    local py_version pkg_version

    py_version="$(sed -n 's/^__version__ = "\([^"]*\)"$/\1/p' onda/_version.py 2>/dev/null || echo 'unknown')"
    pkg_version="$(sed -n 's/^  "version": "\([^"]*\)",$/\1/p' frontend/package.json 2>/dev/null || echo 'unknown')"

    if [ "$py_version" != "$expected" ]; then
        echo "⚠️  Warning: onda/_version.py has ${py_version}, expected ${expected}" >&2
    fi
    if [ "$pkg_version" != "$expected" ]; then
        echo "⚠️  Warning: frontend/package.json has ${pkg_version}, expected ${expected}" >&2
    fi
}

# ── Resolve versions ─────────────────────────────────────────────────────────
ONDAP_VERSION="$(resolve_version onda 'backend + pipeline')"
GUI_VERSION="$(resolve_version gui 'frontend Svelte')"

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
    check_version_files_consistent "$ONDAP_VERSION"
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
    check_version_files_consistent "$ONDAP_VERSION"

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
        check_version_files_consistent "$ONDAP_VERSION"
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
