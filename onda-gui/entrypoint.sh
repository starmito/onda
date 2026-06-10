#!/bin/sh
set -e

# ── Running as root: create dirs, chown, then start services ──
if [ "$(id -u)" = "0" ]; then
    # Create writable directories (avoids Docker creating them as root on bind mount)
    mkdir -p /input /output /input_rubberband /config/model_configs
    chown -R 1000:1000 /input /output /input_rubberband /config /var/log/nginx /var/cache/nginx 2>/dev/null || true
fi

# ── Start Go backend as user 1000 (needs write access to volumes) ──
echo "→ Starting Go backend on :3001..."
su-exec 1000:0 /usr/bin/onda-backend serve --addr :3001 &
sleep 1

# ── Start nginx as current user (root — only reads files, needs /dev/stdout) ──
echo "→ Starting nginx..."
exec nginx -g "daemon off;"
