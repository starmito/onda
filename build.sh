#!/usr/bin/env bash
# Onda — centralized build script. VERSION is the single source of truth.
#
# Version policy:
#   The top-level VERSION file is the single source of truth for releases.
#   Git tags (onda-vX.Y.Z) are release labels created from VERSION, not its
#   source. build.sh and deploy.sh read VERSION and validate that all
#   consumers (onda/_version.py, pyproject.toml, frontend/package.json) match.
#
# Usage:
#   ./build.sh              native build (backend + frontend + python checks)
#   ./build.sh --docker     build Docker images via docker compose
#   ./build.sh --version    print resolved version and exit
#   ./build.sh --write-version  regenerate versioned files from VERSION

set -euo pipefail

ROOT="${ONDA_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
cd "$ROOT"

# ── Read the canonical version from VERSION ──────────────────────────────────
read_version() {
    local version_file="$ROOT/VERSION"
    if [ ! -f "$version_file" ]; then
        echo "ERROR: VERSION file not found at $version_file" >&2
        return 1
    fi
    ONDAP_VERSION="$(tr -d '[:space:]' < "$version_file")"
    if ! [[ "$ONDAP_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "ERROR: VERSION file contains invalid version '$ONDAP_VERSION'. Expected vX.Y.Z" >&2
        return 1
    fi
    # Frontend and backend share the same release cycle.
    GUI_VERSION="$ONDAP_VERSION"
    export ONDAP_VERSION GUI_VERSION
}

# ── Print versions ───────────────────────────────────────────────────────────
print_versions() {
    echo "ONDAP_VERSION (backend + pipeline): $ONDAP_VERSION"
    echo "GUI_VERSION   (frontend Svelte):    $GUI_VERSION"
}

# ── Ensure consumers agree with VERSION (read-only) ──────────────────────────
check_version_files() {
    local fail=0
    local expected_py_version="${ONDAP_VERSION#v}"

    # Python pipeline module
    local py_version
    py_version="$(sed -n 's/^__version__ = "\([^"]*\)".*/\1/p' "$ROOT/onda/_version.py" 2>/dev/null || true)"
    if [ "$py_version" != "$ONDAP_VERSION" ]; then
        echo "ERROR: onda/_version.py ($py_version) does not match VERSION ($ONDAP_VERSION)" >&2
        fail=1
    fi

    # Python package manifest (PEP 440, no leading "v")
    local pp_version
    pp_version="$(sed -n 's/^version = "\([^"]*\)".*/\1/p' "$ROOT/pyproject.toml" 2>/dev/null || true)"
    if [ "$pp_version" != "$expected_py_version" ]; then
        echo "ERROR: pyproject.toml version ($pp_version) does not match VERSION ($expected_py_version)" >&2
        fail=1
    fi

    # Frontend package manifest
    local fe_version
    fe_version="$(sed -n 's/.*"version": "\([^"]*\)".*/\1/p' "$ROOT/frontend/package.json" 2>/dev/null || true)"
    if [ "$fe_version" != "$ONDAP_VERSION" ]; then
        echo "ERROR: frontend/package.json version ($fe_version) does not match VERSION ($ONDAP_VERSION)" >&2
        fail=1
    fi

    return $fail
}

# ── Write a file only if it would change (keeps mtime/idempotent) ────────────
_write_if_changed() {
    local file="$1"
    local content="$2"
    if [ -f "$file" ] && [ "$(cat "$file")" = "$content" ]; then
        return 0
    fi
    printf '%s' "$content" > "$file"
}

# ── Regenerate version files from VERSION (manual use only) ──────────────────
generate_version_files() {
    read_version

    _write_if_changed "$ROOT/onda/_version.py" "$(printf '__version__ = "%s"\n' "$ONDAP_VERSION")"

    local py_version="${ONDAP_VERSION#v}"
    if [ -f "$ROOT/pyproject.toml" ]; then
        local current_pp
        current_pp="$(sed -n 's/^version = "\([^"]*\)".*/\1/p' "$ROOT/pyproject.toml")"
        if [ "$current_pp" != "$py_version" ]; then
            sed -i "s/^version = \"[^\"]*\"/version = \"$py_version\"/" "$ROOT/pyproject.toml"
        fi
    fi

    if [ -f "$ROOT/frontend/package.json" ]; then
        local current_fe
        current_fe="$(sed -n 's/.*\"version\": \"\([^\"]*\)\".*/\1/p' "$ROOT/frontend/package.json")"
        if [ "$current_fe" != "$ONDAP_VERSION" ]; then
            sed -i 's/"version": "[^"]*"/"version": "'"$ONDAP_VERSION"'"/' "$ROOT/frontend/package.json"
        fi
    fi

    _write_if_changed "$ROOT/VERSION" "$(printf '%s\n' "$ONDAP_VERSION")"
}

# ── Native build ─────────────────────────────────────────────────────────────
native_build() {
    echo "🔨 Onda native build"
    read_version
    if ! check_version_files; then
        echo "ERROR: version files do not match VERSION. Run '$0 --write-version' to regenerate." >&2
        exit 1
    fi
    print_versions

    # Backend Go
    echo "  → Building Go backend..."
    (
        cd "$ROOT/backend"
        GOTOOLCHAIN=go1.26.0 go mod tidy
        CGO_ENABLED=0 GOOS=linux go build \
            -ldflags "-X github.com/starmito/onda/internal/api.Version=$ONDAP_VERSION" \
            -o "$ROOT/onda-backend" ./cmd/onda/
    )

    # Frontend Svelte
    echo "  → Building frontend..."
    (
        cd "$ROOT/frontend"
        if [ ! -d node_modules ]; then
            npm ci
        fi
        VITE_ONDA_VERSION="$GUI_VERSION" npm run build
        printf '%s\n' "$GUI_VERSION" > dist/VERSION
    )

    # Python syntax check
    echo "  → Checking Python syntax..."
    python3 -m py_compile "$ROOT/onda/_version.py" "$ROOT/onda/__init__.py" "$ROOT/onda/cli.py"

    echo "✅ Native build complete"
    echo "   Backend binary: $ROOT/onda-backend"
    echo "   Frontend dist:  frontend/dist"
}

# ── Docker build ─────────────────────────────────────────────────────────────
docker_build() {
    echo "🐳 Onda Docker build"
    read_version
    if ! check_version_files; then
        echo "ERROR: version files do not match VERSION. Run '$0 --write-version' to regenerate." >&2
        exit 1
    fi
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
        read_version
        print_versions
        ;;
    --docker|-d)
        docker_build
        ;;
    --write-version|-w)
        generate_version_files
        echo "✅ Version files regenerated from VERSION"
        ;;
    ""|--native)
        native_build
        ;;
    --help|-h)
        sed -n '2,10p' "$0"
        ;;
    *)
        echo "Unknown option: $1" >&2
        echo "Usage: $0 [--docker|--version|--write-version|--help]" >&2
        exit 1
        ;;
esac
