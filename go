#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_GO="$SCRIPT_DIR/.tools/go/bin/go"

if [ -x "$LOCAL_GO" ]; then
    exec "$LOCAL_GO" "$@"
fi

if command -v go >/dev/null 2>&1; then
    exec go "$@"
fi

cat >&2 <<'EOF'
ERROR: no se encontró Go.

Opciones:
1. Instala Go en el PATH (https://go.dev/doc/install)
2. O coloca una distribución local en: ./.tools/go/bin/go

EOF
exit 1
