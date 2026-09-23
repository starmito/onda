# Onda Architecture

> Versión actual: `VERSION` en la raíz del repo es la única fuente de verdad. `build.sh` y `deploy.sh` validan que `onda/_version.py`, `pyproject.toml` y `frontend/package.json` coincidan.

## Project Structure

```
onda/
├── backend/                    # Go backend
│   ├── cmd/onda/main.go        # Entry point
│   ├── internal/
│   │   ├── api/                # HTTP API, static files, DAW endpoints
│   │   ├── audio/              # Audio utilities (FFmpeg, SoX, Rubberband)
│   │   ├── pipeline/           # Pipeline orchestrator
│   │   └── daw/                # DAW helpers (MIDI, tempo, effects)
│   └── go.mod
├── frontend/                   # Svelte 5 + TypeScript 6 frontend
│   ├── src/
│   │   ├── lib/                # Components and API client
│   │   └── App.svelte
│   ├── package.json
│   └── vite.config.ts
├── onda/                       # Python inference + CLI
│   ├── detect_gpu.sh
│   ├── cli.py
│   └── ...
├── models/                     # Model checkpoints (not in git)
├── data/                       # Single data root (not in git)
│   ├── input/                  # User upload source
│   ├── output/                 # Generated stems
│   ├── daw-data/               # DAW project data
│   ├── input_rubberband/       # Pitch-shift uploads
│   ├── config/                 # Local runtime config
│   ├── logs/                   # Service and event logs
│   ├── models/                 # Model cache (alternative to ./models)
│   └── .cache/                 # HF, torch, numba, xdg caches
├── tests/                      # Python / Go / API / e2e tests
├── tools/                      # Repo guards and pipeline helpers
├── VERSION
├── CHANGELOG.md
├── ARCHITECTURE.md
└── README.md
```

## Container (single)

- **Name**: `onda`
- **Go backend**: serves the compiled Svelte frontend and the REST API on `:3000`
- **Python inference**: Demucs, ViperX, MDX/SCNet/ONNX, pitch shift, DAW audio effects
- **Bind mounts**:
  - `./data/`       → `/app/data` (single data root: `input/`, `output/`, `daw-data/`, `config/`, `logs/`, `models/`, `.cache/`)
  - `./models/`     → `/app/models` (model repository, optional if stored under `./data/models`)
  - `pytorch-cache` → `/opt/pytorch-backends` (CUDA/CPU torch backend cache)

All runtime paths are resolved relative to `ONDA_DATA_DIR` (default `/app/data`). The legacy fixed paths `/app/input/`, `/app/output/` and bare `/input/` are obsolete.

## Pipeline Flow

```
Frontend upload → POST /api/upload → ${ONDA_DATA_DIR}/input/<file>
POST /api/separate → Job queue → Worker
  → pipeline.sh --steps JSON ${ONDA_DATA_DIR}/input/<file>
  → vocal/stem separation (Python inference)
  → optional pitch shift (rubberband CLI)
  → writes stems to ${ONDA_DATA_DIR}/output/<song>/
  → status JSON updated for polling
```

## DAW Flow

```
Frontend DAWWorkspace → /api/daw/* endpoints
  → audio serving, spectrogram, key detection
  → MIDI parse/export via gomidi
  → trim/fade/export via SoX / Go audio libs
  → tempo detection via aubio
  → effects (EQ, compressor, reverb, ...) via SoX
```

## Versioning

Versions are read from the `VERSION` file at build time:

- `ONDAP_VERSION` is injected into the Go binary (`-ldflags`) and the Python package (`onda/_version.py`).
- `GUI_VERSION` (same value) is injected into the Svelte build via `VITE_ONDA_VERSION`.
- `pyproject.toml` stores the version without the leading `v` (PEP 440).

`build.sh` and `deploy.sh` read `VERSION` and validate all consumers. Release tags (`onda-vX.Y.Z`) are labels created from `VERSION`, not its source.
