# Onda — Backend NVIDIA (CUDA 12.8)
# Multi-stage: compile deps in builder, slim final image

FROM python:3.12-slim AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Install PyTorch with CUDA support
RUN pip install --no-cache-dir --target /deps \
    torch torchaudio torchvision \
    --index-url https://download.pytorch.org/whl/cu128

# Install remaining deps (compiled against PyTorch)
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

# Create demucs CLI entry point (pip --target skips console scripts)
# Run from /tmp to avoid local /app/demucs/ shadowing the pip version
RUN printf '#!/bin/bash\ncd /tmp\nexec env PYTHONPATH=/usr/local/lib/python3.12/site-packages python -m demucs "$@"\n' > /usr/local/bin/demucs && \
    chmod +x /usr/local/bin/demucs

WORKDIR /app
COPY . .

ENTRYPOINT ["python"]
