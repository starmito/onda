# Onda Architecture v2.0.0-alpha

## Project Structure

```
onda/
├── backend/                    # Go backend
│   ├── cmd/onda/main.go       # Entry point
│   ├── internal/
│   │   ├── cli/               # CLI flags, presets, parsing
│   │   │   ├── flags.go
│   │   │   └── flags_test.go
│   │   ├── audio/             # Audio utilities (FFmpeg, Rubberband)
│   │   │   ├── audio.go
│   │   │   ├── ffmpeg.go
│   │   │   ├── rubberband.go
│   │   │   └── audio_test.go
│   │   ├── pipeline/          # Pipeline orchestrator
│   │   │   ├── pipeline.go
│   │   │   └── pipeline_test.go
│   │   └── api/               # HTTP API (future)
│   └── go.mod
├── frontend/                   # Tauri + Svelte + TypeScript
│   ├── src/                   # Svelte app
│   │   ├── App.svelte
│   │   └── main.ts
│   ├── src-tauri/             # Tauri Rust backend
│   │   ├── src/
│   │   ├── Cargo.toml
│   │   └── tauri.conf.json
│   ├── package.json
│   ├── vite.config.ts
│   └── svelte.config.js
├── models/                     # Model checkpoints (not in git)
├── VERSION
├── CHANGELOG.md
├── ARCHITECTURE.md
└── README.md
```

## Pipeline Flow

```
CLI flags → Parse → Pipeline.New()
  → vocal separation (docker exec inference_universal.py)
  → stem separation (docker exec demucs)
  → dedicated stems (docker exec inference_demucs_single.py)
  → pitch shift (rubberband CLI)
  → write status JSON
```

## Container

- Name: `onda`
- Host: .87 (GPU Nvidia)
- Models: mounted as `:ro` from `~/models/`
- Output: mounted `/home/starmito/projects/onda/output/` → `/output/`
- Input: mounted `/home/starmito/projects/onda/input/` → `/input/`
