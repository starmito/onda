# Changelog

## v2.0.0-alpha

> **Nota**: El historial de git anterior a este commit se perdió por un bug en la herramienta de desarrollo (subagente C creó un commit huérfano). El contenido del código está intacto. Este commit representa el estado completo de Fase 0 + Fase 1.

### Fase 0 — Estructura del proyecto
- Rama `v2.0.0-alpha` creada a partir de `main` (v1.4.4 inamovible)
- Estructura multi-lenguaje: `backend/` (Go), `frontend/` (Tauri + Svelte + TS)
- Go module + CLI skeleton
- Tauri + Svelte + TypeScript skeleton
- ARCHITECTURE.md con diseño del sistema

### Fase 1 — Core Go pipeline
- `backend/cmd/onda/main.go`: entry point, dispatch de comandos
- `backend/internal/cli/flags.go`: 298 líneas, flags, presets (turbo/balance/master/ultimate), compat legacy
- `backend/internal/cli/flags_test.go`: 15 tests
- `backend/internal/audio/`: audio.go, rubberband.go, ffmpeg.go + tests (11)
- `backend/internal/pipeline/pipeline.go`: 464 líneas, orquestador 2-stage con 4 presets
- `backend/internal/pipeline/pipeline_test.go`: 14 tests
- Tests unitarios: 39/39 pass

### Correcciones (Ronda 1)
- **Bug #1**: Output path mapping — `--output` del usuario se traduce a ruta dentro del contenedor
- **Bug #2**: PolarFormer YAML — `use_pope` y otras keys inválidas eliminadas. `attend.py` actualizado con soporte para parámetro `scale`

### Correcciones (Ronda 2)
- **Bug #3**: Demucs sin `-n` — ahora pasa `-n htdemucs_ft` al comando
- **Bug #4**: Demucs estructura output — corrige track subdirectory y stems esperados (`vocals.wav`/`no_vocals.wav`)

## v1.4.4

Última versión estable. Inamovible en rama `main`.
