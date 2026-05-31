# Onda v2.1.1

AI-powered audio source separation with GPU acceleration. Split songs into vocals, drums, bass, and other stems using state-of-the-art deep learning models.

Built on top of UVR (Ultimate Vocal Remover) techniques with a modern web UI, Docker orchestration, and sequential job queue.

## Features

### Core Separation

- **Vocal removal & stem splitting:** Extract vocals, drums, bass, and other from any audio file
- **Multiple AI models:**
  - **Roformer family** — ViperX (BS Roformer), MelBand Roformer, PolarFormer
  - **Demucs** — htdemucs_ft (PyTorch), htdemucs, htdemucs_6s, hdemucs_mmi
  - **Demucs ONNX** — 56 specialists (vocals, drums, bass, other per model)
  - **MDX-Net** — Kim Vocal 1 & 2, UVR MDX-Net Main
  - **SCnet** — 8 models for additional separation quality
- **Pitch control:** Semitone shifting (-12 to +12) applied before separation
- **~60x realtime** on RTX 5060 Ti (3:54 song in ~4 seconds)

### Model Management

- **Model catalog:** Browse and download 98 UVR models with real file sizes (sourced via HTTP HEAD from GitHub Releases, HuggingFace, and Facebook CDN)
- **Per-model configuration:** Segment size, overlap, chunk size, batch size, shifts, segment duration, parallel jobs
- **VRAM calculator:** Real-time GPU memory estimation with interactive sliders — additive formula prevents unrealistic high-end predictions
- **Model deletion:** Remove model files directly from the UI

### Pipeline & Queue

- **Pipeline editor:** Visual drag-and-drop pipeline builder with SVG flow graph
- **Sequential queue:** FIFO job processing with persistent results across page reloads
- **Multi-step pipeline:** ViperX vocal extraction → Demucs stem splitting → pitch shifting — all in one run

### Web UI

- **Dark-themed Svelte 5 interface** with TypeScript
- **Audio player** with waveform visualization and per-stem mute/solo
- **Status bar** with real-time health indicators (backend, frontend, pipeline, GPU, disk, Docker)
- **Version mismatch detection** across all components
- **GPU monitor** with VRAM usage and temperature polling

## Hardware Requirements

- **NVIDIA GPU** with CUDA support (tested on RTX 5060 Ti 16 GB)
- Minimum **16 GB system RAM**
- ~28 GB disk space for all 98 models (individual models range from 64 MB to 3.2 GB)

## Software Requirements

- **Docker** 24+ with `docker compose` plugin
- **NVIDIA Container Toolkit** (`nvidia-container-toolkit`)
- Linux host (tested on Ubuntu 24.04 / Debian)

## Quick Start

```bash
# 1. Clone the repo
git clone https://github.com/starmito/onda.git
cd onda
git checkout v2.1.0-alpha

# 2. Create .env file (or use defaults)
cat > .env << 'EOF'
MODEL_DIR=./models
HOST_UID=1000
HOST_GID=1000
ONDA_PORT=3000
EOF

# 3. Create required directories
mkdir -p models input output

# 4. Download at least one model (e.g., ViperX)
mkdir -p models/VR_Models/BS_Roformer_Viperx
# Download .ckpt from: https://github.com/TRvlvr/model_repo/releases
# Place .ckpt file in models/VR_Models/BS_Roformer_Viperx/

# 5. Build and start
docker compose up -d --build

# 6. Open http://localhost:3000
```

### Directory Layout

```
onda/
├── models/          # Model files (~27 GB, mounted as /models)
├── input/           # Upload directory (auto-created)
└── output/          # Results directory (auto-created)
```

## Architecture

Onda runs as two Docker containers orchestrated by `docker compose`:

**`onda` — Inference container (Python + PyTorch + CUDA)**
- PyTorch with CUDA support
- Demucs, Roformer, MDX-Net inference scripts
- Pipeline orchestrator (`pipeline.sh`)
- Health check verifies CUDA availability via PyTorch

**`onda-gui` — API + Frontend container (Go + nginx + Svelte)**
- Go API server on port 3001 (proxied through nginx)
- nginx serves the Svelte 5 frontend and reverse-proxies API calls
- Docker socket mounted for container orchestration
- Model catalog (`uvr_models.json`) with 98 entries

### Stack

- **Frontend:** Svelte 5 + TypeScript, served by nginx
- **Backend:** Go API server (port 3001 internally, exposed on 3000)
- **Inference:** Python/PyTorch in `onda` container (Demucs, Roformer, MDX-Net)
- **Orchestration:** `docker compose` with health checks and GPU passthrough

## API Reference

### Health & System

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | System health (backend, frontend, pipeline, GPU, disk, Docker, version mismatch) |
| GET | `/api/gpu` | GPU availability check |
| GET | `/api/gpu/info` | GPU VRAM usage, temperature, and runtime |
| GET | `/api/gpu/vram-calculator` | VRAM estimate with `?models=` query param |

### Models

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/models` | List available preset configurations |
| GET | `/api/models/list` | Locally installed models with sizes and categories |
| GET | `/api/models/catalog` | UVR model catalog (98 models with real file sizes and download URLs) |
| GET | `/api/models/{name}/config` | Get per-model inference configuration |
| POST | `/api/models/{name}/config` | Save per-model inference configuration |
| POST | `/api/models/download` | Download model from HuggingFace (`{"source": "huggingface", "repo": "..."}`) |
| GET | `/api/models/download/status` | Check download progress |
| DELETE | `/api/models/{name}` | Delete a model file |

### Separation Pipeline

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/separate` | Enqueue audio separation job |
| GET | `/api/queue/status` | Job queue status (waiting → processing → done/error) |
| GET | `/api/results` | List completed separation results grouped by song |
| GET | `/api/inputs` | List uploaded input files |

### File Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/upload` | Upload audio file (multipart form) or model file (`?type=model`) |
| GET | `/api/files/{song}/{file}` | Download/serve a separated stem file |
| DELETE | `/api/files/{song}` | Delete a song and all its stems |
| DELETE | `/api/delete` | Delete a specific stem (`?file=song/stem.wav`) |
| DELETE | `/api/inputs/{name}` | Delete an input file |

### Backend Control

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/backend/start` | Start inference container |
| POST | `/api/backend/stop` | Stop inference container |
| POST | `/api/backend/restart` | Restart inference container |

### Separation Request Body

```json
{
  "preset": "turbo",
  "input": "/input/song.mp3",
  "pitch": 2,
  "vocal_model": "BS_Roformer_Viperx",
  "stem_model": "htdemucs_ft",
  "viperx": true,
  "viperx_keep": "both",
  "demucs": true,
  "demucs_keep": ["drums", "bass", "other"],
  "output": "/output/song"
}
```

### Separation Response

```json
{
  "status": "queued",
  "song": "song"
}
```

### Queue Status Response

```json
{
  "jobs": [
    {
      "song": "song",
      "status": "processing",
      "progress": 50,
      "index": 1,
      "files": null
    }
  ]
}
```

## Project Structure

```
onda/
├── backend/                    # Go API server
│   ├── cmd/onda/               # Entry point
│   └── internal/
│       ├── api/                # HTTP handlers
│       │   ├── server.go       # Routes, queue, health, upload, separation
│       │   ├── models.go       # Model listing, download, catalog
│       │   └── gpu_info.go     # GPU monitoring (PyTorch + pynvml)
│       └── cli/                # Pipeline flags, presets, model resolution
├── frontend/                   # Svelte 5 UI
│   ├── index.html              # Entry HTML
│   ├── dist/                   # Built assets (gitignored)
│   └── src/
│       ├── App.svelte          # Main application shell
│       └── lib/
│           ├── api.ts          # TypeScript API client (all endpoints)
│           ├── PipelinePanel.svelte    # Pipeline editor + queue display
│           ├── ModelManager.svelte     # Per-model config sliders
│           ├── ModelDownloader.svelte  # UVR catalog browser & downloader
│           └── VramCalculator.svelte   # VRAM estimator UI
├── onda-gui/                   # GUI container build
│   ├── Dockerfile              # Multi-stage: Go builder + nginx
│   ├── nginx.conf              # nginx reverse proxy config
│   └── entrypoint.sh           # Container startup script
├── lib_v5/                     # Roformer inference scripts (Python)
│   ├── bs_roformer.py          # BS Roformer model architecture
│   ├── mel_band_roformer.py    # MelBand Roformer
│   └── attend.py               # Attention modules
├── demucs/                     # Demucs Python implementation
├── pipeline.sh                 # Pipeline orchestrator (bash, 436 lines)
├── uvr_models.json             # UVR model catalog (98 entries with URLs & sizes)
├── docker-compose.yml          # Service orchestration (onda + onda-gui)
├── Dockerfile                  # Inference container (onda: Python + CUDA)
├── VERSION                     # Version marker (v2.1.1)
├── CHANGELOG.md                # Full changelog
└── README.md                   # This file
```

## Presets

| Preset | Vocal Model | Stem Model | Pitch | VRAM |
|--------|-------------|------------|-------|------|
| Turbo | MelBand KJ | Demucs htdemucs_ft | Yes | ~8 GB |
| Balance | PolarFormer | Demucs htdemucs_ft | Yes | ~12 GB |
| Master | PolarFormer | Demucs + dedicated Bass | Yes | ~12 GB |
| Ultimate | 4 dedicated passes | Drums/Bass/Other/Vocals | Yes | ~12 GB |

## Configuration

Configuration is managed via a `.env` file at the project root:

| Variable | Default | Description |
|----------|---------|-------------|
| `MODEL_DIR` | `./models` | Path to model files (mounted as `/models` in both containers) |
| `HOST_UID` | `1000` | User ID for file ownership in the inference container |
| `HOST_GID` | `1000` | Group ID for file ownership |
| `ONDA_PORT` | `3000` | External port for the web UI |

## Performance

Benchmarks on NVIDIA RTX 5060 Ti 16 GB:

| Model | Speed | VRAM | Notes |
|-------|-------|------|-------|
| ViperX (BS Roformer) | ~0.5× realtime | ~616 MB | Highest quality vocal extraction |
| Demucs htdemucs_ft | ~60× realtime | ~2.8 GB | 4-stem split (drums, bass, other, vocals) |
| MDX-Net Kim Vocal 2 | ~5× realtime | ~64 MB | Fastest, lightweight |
| MelBand Roformer | ~0.4× realtime | ~900 MB | Alternative vocal model |

## License

MIT — see repository for details.
