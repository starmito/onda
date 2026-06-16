# Onda v3.1.0 — Contenedor unificado (Python + Go + Nginx + Svelte)
# GPU auto-detect en runtime via entrypoint.sh
# Multi-stage: go-builder → frontend-builder → python-base → runtime

# ── Stage 0: Compilar backend Go ──────────────────────────
FROM golang:1.26-alpine AS go-builder
WORKDIR /src
COPY backend/ ./
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -o /onda-backend ./cmd/onda/
RUN chmod +x /onda-backend

# ── Stage 1: Compilar frontend Svelte ─────────────────────
FROM node:22-alpine AS frontend-builder
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --silent
COPY frontend/ ./
RUN npm run build

# ── Stage 2: Base Python con dependencias comunes ─────────
FROM python:3.12-slim AS python-base

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libsndfile1 \
    rubberband-cli \
    ffmpeg \
    nginx \
    && rm -rf /var/lib/apt/lists/*

# Dependencias comunes (sin torch/torchaudio/onnxruntime-gpu — se instalan en runtime)
COPY requirements-common.txt /tmp/requirements-common.txt
RUN SKLEARN_ALLOW_DEPRECATED_SKLEARN_PACKAGE_INSTALL=True \
    pip install --no-cache-dir -r /tmp/requirements-common.txt

# Demucs 4.0.1 — sin dependencias (torchaudio se instala aparte en runtime)
RUN pip install --no-cache-dir demucs==4.0.1 --no-deps

# Demucs CLI entry point (pip --target omite console scripts)
RUN printf '#!/bin/bash\ncd /tmp\nexec python -m demucs "$@"\n' > /usr/local/bin/demucs && \
    chmod +x /usr/local/bin/demucs

# ── Stage 3: Runtime final (todo combinado) ───────────────
FROM python:3.12-slim AS runtime

# System dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    libsndfile1 \
    rubberband-cli \
    ffmpeg \
    nginx \
    && rm -rf /var/lib/apt/lists/*

# Python deps (common — sin torch-family)
COPY --from=python-base /usr/local/lib/python3.12/site-packages/ /usr/local/lib/python3.12/site-packages/
COPY --from=python-base /usr/local/bin/demucs /usr/local/bin/demucs

# Go backend
COPY --from=go-builder /onda-backend /usr/local/bin/onda-backend
RUN chmod +x /usr/local/bin/onda-backend

# Frontend Svelte
COPY --from=frontend-builder /src/dist/ /usr/share/nginx/html/

# Nginx config
COPY onda-gui/nginx.conf /etc/nginx/nginx.conf

# Pipeline script
COPY pipeline.sh /pipeline.sh
RUN chmod +x /pipeline.sh

# GPU detection script
COPY onda/detect_gpu.sh /usr/local/bin/detect_gpu.sh
RUN chmod +x /usr/local/bin/detect_gpu.sh

# Entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# VERSION file
COPY VERSION /VERSION
RUN cp /VERSION /usr/share/nginx/html/VERSION

# UVR model catalog
COPY uvr_models.json /app/uvr_models.json
COPY hf_models.json /app/hf_models.json

# Nginx temp dirs for non-root
RUN mkdir -p /var/log/nginx /var/cache/nginx /var/run && \
    chown -R 1000:1000 /var/log/nginx /var/cache/nginx && \
    chmod 755 /var/log/nginx /var/cache/nginx

# Create runtime directories
RUN mkdir -p /input /output /input_rubberband /config

# Non-root user
RUN adduser --uid 1000 --disabled-password starmito

WORKDIR /app
EXPOSE 3000

ENTRYPOINT ["/entrypoint.sh"]
