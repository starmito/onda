# Onda v3.1.1 — Contenedor unificado (Python + Go + Svelte)
# GPU auto-detect en runtime via entrypoint.sh
# Build: docker compose build
# Deploy: docker compose up -d  (o bash deploy.sh para auto-detectar GPU)

# ── Stage 1: Compilar frontend Svelte ───────────────────
FROM node:22-alpine AS frontend-builder
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --silent
COPY frontend/ ./
RUN npm run build

# ── Stage 2: Compilar backend Go ────────────────────────
FROM golang:1.26-alpine AS go-builder
WORKDIR /src
COPY backend/ ./backend/
COPY --from=frontend-builder /src/dist/ ./backend/internal/api/dist/
COPY VERSION ./
RUN cd backend && GOTOOLCHAIN=go1.26.0 go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /onda-backend ./cmd/onda/
RUN chmod +x /onda-backend

# ── Stage 3: Dependencias Python (torch CPU en build time) ─
FROM python:3.12-slim AS python-base
ENV PIP_ROOT_USER_ACTION=ignore
ENV PIP_NO_PYTHON_VERSION_WARNING=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir torch==2.11.0+cpu torchaudio==2.11.0+cpu torchvision==0.26.0+cpu --index-url https://download.pytorch.org/whl/cpu

# Demucs con --no-deps (no necesita torch en build)
RUN pip install --no-cache-dir demucs==4.0.1 --no-deps
RUN printf '#!/bin/bash\ncd /tmp\nexec python -m demucs "$@"\n' > /usr/local/bin/demucs && \
    chmod +x /usr/local/bin/demucs

# Dependencias comunes SIN torch (numpy, scipy, etc.)
COPY requirements-common.txt /tmp/
RUN SKLEARN_ALLOW_DEPRECATED_SKLEARN_PACKAGE_INSTALL=True \
    pip install --no-cache-dir -r /tmp/requirements-common.txt

# Paquetes que dependen de torch (torch CPU ya está instalado, pip NO descargará CUDA)
RUN pip install --no-cache-dir \
    diffq pytorch_lightning ml_collections onnx2pytorch \
    rotary_embedding_torch segmentation_models_pytorch \
    transformers timm torchmetrics spafe julius \
    torch_audiomentations asteroid openunmix dora-search \
    torchcodec==0.12.0

# ── Stage 4: Imagen final ────────────────────────────────
FROM python:3.12-slim AS runtime

ARG USER_UID=1000
ARG USER_GID=1000

# Solo lo necesario para PRODUCCIÓN (sin build-essential)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libsndfile1 \
    rubberband-cli \
    ffmpeg \
    aubio-tools \
    sox \
    && rm -rf /var/lib/apt/lists/*

# Python deps (desde python-base)
COPY --from=python-base /usr/local/lib/python3.12/site-packages/ /usr/local/lib/python3.12/site-packages/
COPY --from=python-base /usr/local/bin/demucs /usr/local/bin/demucs

# Go backend
COPY --from=go-builder /onda-backend /usr/local/bin/onda-backend
RUN chmod +x /usr/local/bin/onda-backend

# Pipeline script
COPY pipeline.sh /app/pipeline.sh
RUN chmod +x /app/pipeline.sh

# ViperX inference
COPY inference_universal.py /app/inference_universal.py
COPY lib_v5/ /app/lib_v5/

# GPU detection
COPY onda/detect_gpu.sh /usr/local/bin/detect_gpu.sh
RUN chmod +x /usr/local/bin/detect_gpu.sh

# Entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# VERSION file
COPY VERSION /VERSION
RUN mkdir -p /usr/share/nginx/html && cp /VERSION /usr/share/nginx/html/VERSION

# UVR model catalog
COPY uvr_models.json /app/uvr_models.json
COPY hf_models.json /app/hf_models.json

# Crear usuario no privilegiado (mismo UID/GID que el instalador host)
RUN groupadd -g ${USER_GID} appgroup && \
    useradd -m -u ${USER_UID} -g appgroup -d /app -s /bin/bash appuser

# Directorios runtime (bind mounts del host) propiedad del usuario
RUN mkdir -p /input /output /input_rubberband /config /daw-data /opt/pytorch-backends && \
    chown -R ${USER_UID}:${USER_GID} /input /output /input_rubberband /config /daw-data /app /opt/pytorch-backends

# Symlink para el backend Go (espera /pipeline.sh)
RUN ln -sf /app/pipeline.sh /pipeline.sh

# Symlink para modelos: docker-compose monta ./models en /app/models,
# pero el backend y pipeline.sh usan /models como ruta base.
RUN ln -sf /app/models /models

WORKDIR /app
EXPOSE 3000

USER appuser

ENTRYPOINT ["/entrypoint.sh"]
