#!/bin/sh
set -e

# ── Start Go backend ──
echo "→ Starting Go backend on :3001..."
/usr/bin/onda-backend serve --addr :3001 &
sleep 1

# ── Start nginx ──
echo "→ Starting nginx..."
exec nginx -g "daemon off;"
