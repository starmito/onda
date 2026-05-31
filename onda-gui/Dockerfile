# ── Stage 1: Compilar backend Go ──
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY backend/go.mod ./
RUN go mod download 2>/dev/null || true
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /onda-backend ./cmd/onda/

# ── Stage 2: Imagen final ──
FROM nginx:alpine
ARG ONDA_VERSION=v2.1.0-alpha

RUN apk add --no-cache bash docker-cli

# nginx + entrypoint
COPY onda-gui/nginx.conf /etc/nginx/nginx.conf
COPY onda-gui/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Go API binary
COPY --from=builder /onda-backend /usr/bin/onda-backend
RUN chmod +x /usr/bin/onda-backend

# Frontend Svelte
COPY frontend/dist/ /usr/share/nginx/html/
RUN echo "${ONDA_VERSION}" > /usr/share/nginx/html/VERSION

# ── Pipeline + project root ──
# findProjectRoot() walks up from WORKDIR looking for VERSION
COPY pipeline.sh /pipeline.sh
RUN chmod +x /pipeline.sh
COPY VERSION /VERSION
ENV ONDA_ROOT=/

# UVR model catalog (used by GET /api/models/catalog)
RUN mkdir -p /app
COPY uvr_models.json /app/uvr_models.json

# Directorios de montaje (docker-compose monta volumes aquí)
RUN mkdir -p /input /output

ENTRYPOINT ["/entrypoint.sh"]
