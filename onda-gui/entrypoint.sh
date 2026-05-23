#!/bin/sh
set -e

# ── Resolve host UID/GID ──
HOST_UID=$(stat -c "%u" /input 2>/dev/null || echo 1000)
HOST_GID=$(stat -c "%g" /input 2>/dev/null || echo 1000)
DOCKER_GID=$(stat -c "%g" /var/run/docker.sock 2>/dev/null || echo 983)

echo "→ host uid:gid = $HOST_UID:$HOST_GID  docker gid = $DOCKER_GID"

# ── Create groups ──
addgroup -g "$HOST_GID" hostgroup 2>/dev/null || echo "hostgroup exists"
addgroup -g "$DOCKER_GID" dockergroup 2>/dev/null || echo "dockergroup exists"

# ── Create apiuser for Python API server ──
if ! id apiuser >/dev/null 2>&1; then
    adduser -D -u "$HOST_UID" -G hostgroup apiuser
    echo "→ apiuser created"
else
    addgroup apiuser hostgroup 2>/dev/null || true
    echo "→ apiuser exists"
fi
addgroup apiuser dockergroup 2>/dev/null || true

# ── Start Python API server ──
echo "→ Starting Python API on :3001..."
su apiuser -c "python3 /usr/lib/onda-api.py" &
sleep 1

# ── Start nginx ──
echo "→ Starting nginx..."
exec nginx -g "daemon off;"
