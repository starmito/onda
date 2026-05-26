# Changelog

## v2.0.0-alpha

### v2.0.0-alpha.7 — UI overhaul + GPU fix + JSON tags

#### Fixed
- **Bug #3 — GPU no detectada**: contenedor `onda` recreado con `docker-compose.nvidia.yml` (runtime nvidia). Health check ahora devuelve `"gpu":true`
- **Bug #1 — Presets sin nombre**: añadidos `json` tags al struct `Preset` en Go backend. API responde `"name"`/`"description"` en camelCase

#### Added
- **Bug #2 — UI overhaul** (paridad con v1.4.4):
  - `FileQueue.svelte`: cola multi-archivo con checkboxes y progreso por archivo
  - `PipelineConfig.svelte`: checkboxes ViperX/HTDemucs con sub-opciones de stems
  - `PitchControl.svelte`: slider -12 a +12 semitonos
  - `ResultsPanel.svelte`: grupos por canción, mute/solo/volumen por stem, waveform, descarga
  - `AudioControls.svelte`: Web Audio API playback, seek slider
  - `HealthBar.svelte`: indicadores BE/GPU/Disk/Docker con polling 15s
  - `App.svelte`: integración de todos los componentes + flujo multi-archivo

### v2.0.0-alpha.6 — Fix permisos output + documentación de testing

#### Fixed
- Docker: añadido `user: "1000:1000"` en `docker-compose.yml` para que archivos de output no se creen como root
- Build: añadido `bin/` a `.gitignore` (binario compilado Go no debe committearse)

#### Added
- `docs/plans/`: planes de Fase 5 — testing integral, rebuild CUDA 12.8, demucs ONNX, MDX-Net, contenedor v2
- `tests/integration/benchmark_results.json`: resultados comparativos de benchmark

### Rollback Demucs ONNX → Demucs PyTorch

#### Removed
- `inference/inference_demucs_onnx.py` — script ONNX eliminado (bug #1: GPU hang en RTX 5060 Ti)
- `tests/integration/test_demucs_onnx.py` — tests del script eliminado
- `demucs-onnx` de `requirements-docker.txt` y `requirements-docker-v2.txt`

#### Changed
- `tests/integration/test_edge_cases.py` — reemplazado Demucs ONNX con Demucs PyTorch (`demucs` CLI)
- `tests/integration/benchmark.py` — eliminada entrada `demucs-onnx-cpu`
- `Dockerfile` y `Dockerfile.v2` — eliminado cleanup de onnxruntime CPU (ya no necesario sin `demucs-onnx`)

#### Rationale
`demucs-onnx==0.3.4` + `onnxruntime-gpu` en RTX 5060 Ti se cuelga (GPU al 100%, no genera output). Los modelos ONNX de StemSplitio son incompatibles con CUDAExecutionProvider en esta configuración. Demucs PyTorch funciona correctamente (~60x realtime). MDX-Net ONNX se mantiene (funciona en GPU sin problemas).

### Rebuild CUDA 12.8 — onnxruntime-gpu con GPU real

#### Fixed
- Inferencia: `onnxruntime-gpu` ahora detecta CUDA 12.8 (antes solo CPU fallback)
- Docker: añadidas librerías runtime CUDA 12.8 (cudnn9, cublas, cufft, curand, cusolver, cusparse)
- Docker: `LD_LIBRARY_PATH` configurado para que ONNX Runtime encuentre CUDA y libcudart
- Docker: `demucs-onnx==0.3.4` instalado (faltaba en requirements anteriores)
- Docker: `onnxruntime` CPU ya no sobrescribe `onnxruntime-gpu` (eliminado post-install)

#### Changed
- Contenedor `onda:nvidia`: `python:3.12-slim` + CUDA 12.8 runtime (~500 MB extra)
- Paquetes NVIDIA desde repositorio oficial (developer.download.nvidia.com)
- `.dockerignore`: excluye `frontend/node_modules/` (reduce tamaño de imagen)

#### Verification
- `onnxruntime.get_available_providers()` → `['TensorrtExecutionProvider', 'CUDAExecutionProvider', 'CPUExecutionProvider']` ✅
- `demucs-onnx` 0.3.4 funcional ✅
- PyTorch CUDA RTX 5060 Ti sin regresión ✅

> **Nota**: El historial de git anterior a este commit se perdió por un bug en la herramienta de desarrollo (subagente C creó un commit huérfano). El contenido del código está intacto. Este commit representa el estado completo de Fase 0 + Fase 1.

#### Known Issues
- **Demucs ONNX GPU se cuelga**: la inferencia con `demucs-onnx` + `onnxruntime-gpu` en RTX 5060 Ti consume VRAM (~3.2 GB) y GPU al 100% pero no genera output. El modelo ONNX (htdemucs_ft, StemSplitio) parece incompatible con TensorRT/CUDAExecutionProvider en esta configuración. La separación funciona en CPU. Issue: #1

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

### Fase 3 — Frontend Alpha MVP

#### Changed
- Frontend: actualizar dependencias a últimas versiones estables (vite 8.0.14, @sveltejs/vite-plugin-svelte 7.1.2, typescript 6.0.3, @tauri-apps/api 2.11.0, @tauri-apps/cli 2.11.2)

#### Fixed
- Frontend: usar `mount()` de Svelte 5 en lugar de `new App()` (API legacy de Svelte 4 causaba pantalla en blanco)
- Frontend: mostrar mensaje de error cuando la API de presets no responde, en lugar de "Cargando presets..." infinito

### Fase 4 — Contenedor de Inferencia v2

#### Changed
- Inferencia: nuevo contenedor `onda-v2` con CUDA 13.2, PyTorch 2.12.0, Demucs 4.0.1
- Inferencia: todas las dependencias Python fijadas con versiones exactas (requirements-docker-v2.txt)
- Inferencia: wheel unificado de PyTorch (sin sufijo cuXXX), incluye runtime CUDA 13.0

#### Added
- `Dockerfile.v2`: multi-stage con python:3.12-slim, PyTorch 2.12.0, Demucs 4.0.1
- `requirements-docker-v2.txt`: 27+ dependencias con versiones exactas

#### Fixed
- Inferencia: añadidas dependencias faltantes openunmix y lameenc para Demucs 4.0.1
- Infraestructura: liberados ~57 GB en .87 (build cache, imágenes sin uso, volúmenes huérfanos)

#### Performance
- Separación: ~60x realtime en RTX 5060 Ti (canción de 3:54 en ~4s)

### Roformer — Scripts de inferencia

#### Added
- `inference/inference_universal.py`: script standalone para todos los modelos Roformer (ViperX, MelBand, PolarFormer)
- `inference/lib_v5/`: librerías core (bs_roformer.py, mel_band_roformer.py, attend.py, etc.)
- Integración en Dockerfile.v2: scripts Roformer copiados a `/app/`

#### Performance
- ViperX: GPU 100%, ~2977 MiB VRAM, 59s para 30s de audio

### MDX-Net — Modelos ONNX

#### Added
- Modelos MDX-Net ONNX: Kim_Vocal_2 (64 MB), Kim_Vocal_1 (64 MB), UVR_MDXNET_Main (64 MB)
- `inference/inference_mdx.py`: script standalone con STFT manual, chunks fijos de 256 frames
- Integración en ambos contenedores (onda y onda-v2)

#### Performance
- MDX-Net Kim Vocal 2: 5.8s para 30s de audio, salida bit-identical entre contenedores
- El más rápido de los 3 métodos de separación

### Demucs ONNX — Migración PyTorch → ONNX

#### Added
- `inference/inference_demucs_onnx.py` — wrapper CLI para Demucs ONNX vía `demucs-onnx` (StemSplitio)
- Dependencia `demucs-onnx==0.3.4` (0.1 MB, sin PyTorch para inferencia)
- Modelos ONNX: 4 especialistas de htdemucs_ft (vocals, drums, bass, other) — 302 MB c/u, ~1.2 GB total
- Fuente: StemSplitio/htdemucs-ft-onnx (calidad idéntica a PyTorch, SDR 9.19 dB vocals)
- `.gitignore`: excluye `models/Demucs_ONNX/`

#### Changed
- Inferencia Demucs ahora puede usar ONNX en lugar de PyTorch (instalación 40× más ligera)
- Bind mount `models/Demucs_ONNX/` accesible desde el contenedor vía `./models:/app/models`

#### Performance
- Demucs ONNX vocals: 19.4s para 30s de audio (1.5x realtime), ~270 MB VRAM pico

### Fase 5 — Testing Integral

#### Added
- `tests/integration/`: suite completa E2E con audio sintético
- `generate_test_audio.py`: generador de fixtures (sine 440Hz, chirp, silence, short 0.5s)
- Tests por método: `test_demucs_onnx.py`, `test_mdx_onnx.py`, `test_pipeline_api.py`
- `benchmark.py`: script comparativo RTF de todos los métodos
- `test_edge_cases.py`: tests de silencio, audio corto, archivo inexistente
- `run_all.sh`: ejecución completa de la suite

#### Coverage
- 4 fixtures de audio sintético (FLAC 44100 Hz)
- End-to-end: Demucs ONNX, MDX-Net ONNX, Pipeline HTTP API
- Edge cases: silencio, audio 0.5s, archivo inexistente
- Benchmark: PyTorch vs ONNX vs MDX-Net

## v1.4.4

Última versión estable. Inamovible en rama `main`.
