# Onda v3.5.0 — Contenedor unificado (Python + Go + Svelte)
# GPU auto-detect en runtime via entrypoint.sh
# Build: docker compose build
# Deploy: docker compose up -d  (o bash deploy.sh para auto-detectar GPU)

# ── Stage 1: Compilar frontend Svelte ───────────────────
FROM node:22-alpine AS frontend-builder
WORKDIR /src

ARG GUI_VERSION
ENV VITE_ONDA_VERSION=$GUI_VERSION

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

ARG ONDAP_VERSION
RUN cd backend && GOTOOLCHAIN=go1.26.0 go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/starmito/onda/internal/api.Version=${ONDAP_VERSION}" -o /onda-backend ./cmd/onda/
RUN chmod +x /onda-backend

# ── Stage 3: Dependencias Python (torch CPU en build time) ─
FROM ubuntu:26.04 AS python-base
ENV PIP_ROOT_USER_ACTION=ignore
ENV PIP_NO_PYTHON_VERSION_WARNING=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    python3.14 \
    python3.14-dev \
    python3.14-venv \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

RUN python3.14 -m venv /opt/venv && \
    /opt/venv/bin/pip install --no-cache-dir --upgrade pip setuptools wheel

RUN /opt/venv/bin/pip install --no-cache-dir torch==2.14.0+cpu torchvision==0.29.0+cpu --index-url https://download.pytorch.org/whl/cpu

# Demucs 4.1.0 con --no-deps (torch ya esta instalado; sphn es su nueva dependencia)
RUN /opt/venv/bin/pip install --no-cache-dir demucs==4.1.0 --no-deps sphn==0.2.1
RUN printf '#!/bin/bash\ncd /tmp\nexec python -m demucs "$@"\n' > /opt/venv/bin/demucs && \
    chmod +x /opt/venv/bin/demucs

# Dependencias comunes SIN torch (numpy, scipy, etc.)
COPY requirements-common.txt /tmp/
RUN SKLEARN_ALLOW_DEPRECATED_SKLEARN_PACKAGE_INSTALL=True \
    /opt/venv/bin/pip install --no-cache-dir -r /tmp/requirements-common.txt

# Paquetes que dependen de torch (torch CPU ya está instalado, pip NO descargará CUDA)
# NOTA: no se instalan asteroid/openunmix/torch_audiomentations porque arrastran torchaudio,
# y Onda v3.5.0 usa demucs 4.1.0 que no lo necesita.
# NOTA: diffq y torchcodec se omiten porque no se usan (diffq solo para modelos
# cuantizados de demucs; torchcodec no es requerido por ningun paquete ni importado por Onda).
RUN /opt/venv/bin/pip install --no-cache-dir \
    pytorch_lightning ml_collections onnx2pytorch \
    rotary_embedding_torch segmentation_models_pytorch \
    transformers timm torchmetrics spafe julius \
    dora-search

# ── Stage 4: Imagen final ────────────────────────────────
FROM ubuntu:26.04 AS runtime

ARG USER_UID=1000
ARG USER_GID=1000
ARG ONDAP_VERSION
ENV ONDAP_VERSION=${ONDAP_VERSION:-unknown}

# Solo lo necesario para PRODUCCIÓN (sin build-essential)
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3.14 \
    python3.14-venv \
    python3-pip \
    libsndfile1=1.2.2-4 \
    rubberband-cli=4.0.0+dfsg-2ubuntu1 \
    ffmpeg=7:8.0.1-3ubuntu2 \
    aubio-tools=0.4.9-5build2 \
    sox=14.7.0.9+ds1-1 \
    && rm -rf /var/lib/apt/lists/*

# Python deps (desde python-base)
COPY --from=python-base /opt/venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Go backend
COPY --from=go-builder /onda-backend /usr/local/bin/onda-backend
RUN chmod +x /usr/local/bin/onda-backend

# Pipeline script
COPY pipeline.sh /app/pipeline.sh
RUN chmod +x /app/pipeline.sh

# Pipeline helpers used by pipeline.sh (progress tracker, demucs worker, etc.).
# Copy the whole directory so no invoked script is left behind or stale.
COPY tools/ /app/tools/
RUN chmod -R +x /app/tools/

# ViperX / MDX inference
COPY inference_universal.py /app/inference_universal.py
COPY inference_mdx.py /app/inference_mdx.py
COPY inference_scnet.py /app/inference_scnet.py
COPY inference_onnx.py /app/inference_onnx.py
COPY inference_polarformer.py /app/inference_polarformer.py
COPY keydetect.py /app/keydetect.py
COPY lib_v5/ /app/lib_v5/
COPY onda/ /app/onda/

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
COPY model_configs/ /app/model_configs/

# Crear usuario no privilegiado (mismo UID/GID que el instalador host).
# Ubuntu 26.04 ya trae un usuario 'ubuntu' con UID/GID 1000; lo renombramos.
RUN usermod -l appuser ubuntu && \
    groupmod -n appgroup ubuntu && \
    usermod -d /app appuser

# Directorios runtime (bind mounts del host) propiedad del usuario
RUN mkdir -p /input /output /input_rubberband /config /daw-data /opt/pytorch-backends /app/.cache && \
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
