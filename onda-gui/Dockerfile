# ── Stage 0: Compilar frontend Svelte ──
FROM node:22-alpine AS frontend-builder
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --silent
COPY frontend/ ./
RUN npm run build

# ── Stage 1: Compilar backend Go ──
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY backend/ ./
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /onda-backend ./cmd/onda/

# ── Stage 2: Imagen final ──
FROM nginx:alpine

RUN apk add --no-cache bash docker-cli su-exec

# nginx + entrypoint
COPY onda-gui/nginx.conf /etc/nginx/nginx.conf
COPY onda-gui/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Go API binary
COPY --from=builder /onda-backend /usr/bin/onda-backend
RUN chmod +x /usr/bin/onda-backend

# Frontend Svelte (from frontend-builder stage)
COPY --from=frontend-builder /src/dist/ /usr/share/nginx/html/

# ── VERSION file (single source of truth) ──
COPY VERSION /VERSION
COPY VERSION /usr/share/nginx/html/VERSION
ENV ONDA_ROOT=/

# UVR model catalog
RUN mkdir -p /app
COPY uvr_models.json /app/uvr_models.json
COPY hf_models.json /app/hf_models.json

# Directorios de montaje
RUN mkdir -p /input /output /input_rubberband

# Nginx temp dirs for non-root (1000)
RUN mkdir -p /var/log/nginx /var/cache/nginx /var/run && \
    chown -R 1000:1000 /var/log/nginx /var/cache/nginx && \
    chmod 755 /var/log/nginx /var/cache/nginx

ENTRYPOINT ["/entrypoint.sh"]
