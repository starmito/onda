#!/bin/sh
set -e

# ── Running as root? Create dirs, chown, then drop privileges ──
if [ "$(id -u)" = "0" ]; then
    # Create writable directories (avoids Docker creating them as root on bind mount)
    mkdir -p /input /output /input_rubberband /config/model_configs
    chown -R 1000:1000 /input /output /input_rubberband /config /var/log/nginx /var/cache/nginx 2>/dev/null || true

    # Drop to user 1000 and re-exec this same script
    exec su-exec 1000:1000 /entrypoint.sh
fi

# ── Running as user 1000 — start services ──

# ── Start Go backend ──
echo "→ Starting Go backend on :3001..."
/usr/bin/onda-backend serve --addr :3001 &
sleep 1

# ── Start nginx (port 3000, non-root via /tmp/nginx.pid) ──
echo "→ Starting nginx..."
exec nginx -g "daemon off;"
