# Onda Architecture

> Versión actual: resuelta en tiempo de build desde los tags de git (`onda-vX.Y.Z`, `gui-vX.Y.Z`).

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
├── frontend/                   # Svelte 5 + TypeScript frontend
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
├── config/                     # Local runtime config (not in git)
├── config.example/             # Example config templates
├── output/                     # Generated stems (not in git)
├── input/                      # User upload source (not in git)
├── daw-data/                   # DAW project data (not in git)
├── tests/                      # Python / Go / API / e2e tests
├── VERSION
├── CHANGELOG.md
├── ARCHITECTURE.md
└── README.md
```

## Container (single)

- **Name**: `onda`
- **Go backend**: serves the compiled Svelte frontend and the REST API on `:3000`
- **Python inference**: Demucs, ViperX, pitch shift, DAW audio effects
- **Bind mounts**:
  - `./input/`      → `/app/input/`
  - `./output/`     → `/app/output/`
  - `./config/`     → `/app/config/`
  - `./daw-data/`   → `/app/daw-data/`
  - `./models/`     → `/app/models/`

## Pipeline Flow

```
Frontend upload → POST /api/upload → /app/input/<file>
POST /api/separate → Job queue → Worker
  → pipeline.sh --steps JSON /app/input/<file>
  → vocal/stem separation (Python inference)
  → optional pitch shift (rubberband CLI)
  → writes stems to /app/output/<song>/
  → status JSON updated for polling
```

## DAW Flow (v3.2.x)

```
Frontend DAWWorkspace → /api/daw/* endpoints
  → audio serving, spectrogram, key detection
  → MIDI parse/export via gomidi
  → trim/fade/export via SoX / Go audio libs
  → tempo detection via aubio
  → effects (EQ, compressor, reverb, ...) via SoX
```

## Versioning

Versions are read from git tags at build time:

- `onda-vX.Y.Z` is injected into the Go binary and the Python package.
- `gui-vX.Y.Z` is injected into the Svelte build via `VITE_ONDA_VERSION`.

`build.sh` and `deploy.sh` resolve these tags; the container image receives them as Docker `ARG`s because `.dockerignore` excludes `.git`.
