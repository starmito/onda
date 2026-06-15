# Onda v2 — Inferencia (CUDA 13.2, PyTorch 2.12.0)
# Multi-stage: builder compila deps, runtime slim
# Multi-platform: ARG DEVICE=cuda | cpu | rocm
ARG DEVICE=cuda

# ── Base stages (platform-specific PyTorch) ──────────
FROM python:3.12-slim AS base-cpu
RUN pip install --no-cache-dir --target /deps \
    torch==2.12.0+cpu \
    torchaudio==2.11.0+cpu \
    --index-url https://download.pytorch.org/whl/cpu

FROM python:3.12-slim AS base-cuda
RUN pip install --no-cache-dir --target /deps \
    torch==2.12.0 \
    torchaudio==2.11.0

FROM python:3.12-slim AS base-rocm
RUN pip install --no-cache-dir --target /deps \
    torch==2.12.0 \
    torchaudio==2.11.0 \
    --index-url https://download.pytorch.org/whl/rocm6.2

FROM base-${DEVICE} AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Demucs 4.0.1 — sin dependencias (torchaudio se instala aparte)
RUN pip install --no-cache-dir --target /deps \
    demucs==4.0.1 --no-deps

# Dependencias restantes fijadas
COPY requirements-docker.txt /tmp/
RUN SKLEARN_ALLOW_DEPRECATED_SKLEARN_PACKAGE_INSTALL=True \
    pip install --no-cache-dir --target /deps -r /tmp/requirements-docker.txt

# ── Runtime stage ────────────────────────────────────
FROM python:3.12-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    libsndfile1 \
    rubberband-cli \
    ffmpeg \
    libtk8.6 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /deps /usr/local/lib/python3.12/site-packages/

# Demucs CLI entry point (pip --target omite console scripts)
RUN printf '#!/bin/bash\ncd /tmp\nexec env PYTHONPATH=/usr/local/lib/python3.12/site-packages python -m demucs "$@"\n' > /usr/local/bin/demucs && \
    chmod +x /usr/local/bin/demucs

WORKDIR /app
COPY . .
# Ensure pipeline.sh is at /pipeline.sh for docker exec calls
COPY pipeline.sh /pipeline.sh
RUN chmod +x /pipeline.sh

# Non-root user (UID 1000 = starmito)
RUN adduser --uid 1000 --disabled-password starmito && \
    chown -R starmito:starmito /app /pipeline.sh
USER starmito

ENTRYPOINT ["python"]
