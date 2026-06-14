# Changelog

## v2.8.0 (2026-06-14) — Presets reales + player unificado + pipeline chaining

### Added
- **Presets con routing de stems**: backend Go con struct `Preset` y `PipelineStep[]`, routing matrix (cada stem → resultado / siguiente paso / descartar)
- **PipelineEditor frontend**: matriz de routing visual con sidebar dinámico desde API, sin presets hardcodeados
- **4 presets bloqueados persistentes**: Separador Voces Total, Eliminador de Voz, Separador Completo, Solo Instrumentos (seedeados en backend, protegidos con DELETE 403)
- **Player unificado**: PitchPage y ResultsPanel usan el mismo componente completo — waveform, peak meters RMS en vivo, skip ±10s, mute/solo, volumen
- **Botón descargar todo** en subgrupos de cambio de tono
- **Pipeline chaining**: `pipeline.sh --steps JSON`, multi-step pipeline, flag `--no-clean`, `--input-from-step` para encadenar pasos

### Fixed
- **Pitch shift permissions**: directorio `_pitch+N` creado con `os.MkdirAll` con permisos correctos (no root)
- **Error fatal si falla drums**: pitch.sh no continúa silenciosamente si falla la copia del stem de batería
- **500 en eliminación de subgrupos**: URL encoding (`encodeURIComponent`) para nombres con paréntesis/espacios
- **AudioContext recreado en cada montaje**: evita abort en `onDestroy` que interrumpía el pitch shift al cambiar de pantalla
- **Conflicto tipo StemRoute/ActionRoute**: renombrado para evitar error de compilación

### Changed
- **Refactor frontend**: eliminado formato legacy de presets (`getHardcodedPreset()`, `lockedPresetNames`). Migración legacy→nuevo formato
- **PipelineEditor**: presets se cargan directamente del backend sin conversión legacy

### Removed
- Código legacy de presets hardcodeados en frontend

## v2.7.12 (2026-06-11)

### Fixed
- Barras de peak meter ahora muestran RMS en vivo (no el valor pico topeado)
- Línea blanca vertical (peak marker) sobre cada barra indicando el pico máximo
- Etiquetas dB superiores siguen mostrando el valor pico acumulado
- Ancho fijo de 100px en .stem-name para alineación vertical de todos los peak meters

### Changed
- VERSION actualizado a v2.7.12 (no reflejaba cambios previos)

## v2.7.4 — Cancel real + status correcto (2026-06-11)

### Fixed
- **Cancel ahora para el proceso real**: se ejecuta `pkill -f pipeline.sh` y `pkill -f python` dentro del contenedor `onda`, no solo el cliente docker local
- **Status de archivos tras cancelar**: ahora muestra 'waiting' (listo para re-ejecutar) en vez de 'uploading'

## v2.7.3 — Resultados navegables + progreso individual + PitchPage con outputs (2026-06-11)

### Added
- **Resultados navegables**: al hacer clic en un archivo completado de la cola, navega automáticamente a la pestaña Resultados
- **Progreso individual por pista**: cada fila de la cola muestra "Paso X/Y: StepName" con su propia barra de progreso y porcentaje
- **PitchPage: grupos de salida**: sección superior que lista los stems de output/ (excepto drums) con control de pitch shift por grupo

### Changed
- **PitchPage**: ahora también acepta `results` y `onResultsChange` props para mostrar y procesar stems existentes

## v2.7.2 — Clear queue on execute + Stop button (2026-06-11)

### Added
- **Backend: `DELETE /api/queue`**: limpia toda la cola de trabajos y cancela el proceso en ejecución
- **Backend: `POST /api/queue/cancel`**: cancela solo el trabajo actual sin limpiar la cola
- **Frontend: Clear automático**: al hacer clic en cualquier botón "Ejecutar", se limpia la cola primero
- **Frontend: Botón "⏹ Detener"**: aparece junto a cada botón de ejecutar cuando hay un proceso activo

### Fixed
- **Error 409 "song already queued"**: ya no se queda atascado al reintentar un trabajo fallido

## v2.7.1 — YAML config en Go puro, sin Python en onda-gui (2026-06-10)

### Changed
- **onda-gui/Dockerfile**: eliminado `COPY pipeline.sh` innecesario — el backend ejecuta pipeline.sh vía `docker exec onda bash /pipeline.sh`, no localmente
- **Go backend**: lectura/escritura de YAML con `gopkg.in/yaml.v3` en Go puro, elimina dependencia de Python en onda-gui
- **Dockerfile**: `go mod tidy` incluido en build para dependencias Go limpias

### Fixed
- **Parámetros de inferencia**: ahora se escriben como `!!int` en YAML (no strings), usando `strconv.Itoa` para `yaml.v3`
- **`!!python/tuple`**: el código Go ignora estos valores YAML específicos de Python sin errores

### Removed
- **Python + py3-yaml** de onda-gui Dockerfile — ya no se necesita, Go maneja YAML directamente
- **`COPY pipeline.sh /pipeline.sh`** de onda-gui — código muerto, pipeline.sh solo necesario en contenedor `onda`

## v2.6.4 — PitchPage: uploaded files with full player (2026-06-10)

### Added
- **Nginx location `/input_rubberband/`**: sirve archivos de audio subidos para pitch con CORS y MIME types
- **`pitchInputDownloadUrl()`** en api.ts: URL builder para servir archivos de pitch subidos
- **`deletePitchUpload()`** en api.ts: función para eliminar archivos de pitch subidos
- **`DELETE /api/uploads/pitch/{name}`** endpoint en Go backend
- **Full per-file player en PitchPage.svelte**: waveform, play/pause/stop, seek, volumen, descarga, eliminación

### Changed
- **VERSION** alineado a `v2.6.4` (consistente `v` prefix en backend, frontend, pipeline)

## v2.6.2-alpha — Refactor de pestañas: 4 presets directos, Personalizado, PitchPage (2026-06-10)

### Added
- **Nueva pestaña "Personalizado"** en el sidebar
- **PitchPage.svelte**: nueva página para Cambiar Tono
- **IconUser** en icons.ts

### Changed
- **Sidebar reorganizado**: 4 presets hardcodeados + Personalizado
- Cada preset se ejecuta directamente al pulsar ▶ Ejecutar

## v2.6.1-alpha — Pulido UI: colores púrpura, iconos SVG, sidebar vertical (2026-06-10)

### Fixed
- Reemplazado acento azul por púrpura (#6c5ce7) en toda la interfaz
- Reemplazados emojis por iconos SVG line-art al estilo vocalremover.org
- Sidebar vertical con items en layout vertical
- Layout fluido: interfaz ocupa todo el viewport

## v2.6.0-alpha — Rediseño UI con sidebar vertical (2026-06-10)

## v2.5.1 — Default preset persistente (2026-06-09)

### Added
- **Default preset persistente**: endpoint `GET/POST /api/presets/default`
- **Botón "Establecer como predeterminado"** en el Gestor de Presets

### Changed
- Selector de presets unificado en un solo dropdown

## v2.5.0-alpha — UI reorganizada (2026-06-09)

### Added
- Botón Gestor de Presets que abre el editor en modal fullscreen
- PresetsPanel con selector de preset, botón Ejecutar y barra de progreso integrada
- Cabecera de cola con checkbox maestro
- Sección de eliminación de presets con confirmación

## v2.4.4 — Progreso fiable + polling (2026-06-10)

### Fixed
- Barra de progreso individual: Python escribe pipeline_status.json directamente en cada chunk
- Barra de progreso total: solo cuenta jobs del batch actual
- Polling rápido: frontend consulta cada 500ms (eventos) y 200ms (pipeline_status.json)
- Timestamps reales con nano decreciente

## v2.4.3-alpha — VRAM + progreso (2026-06-10)

### Fixed
- VRAM: race condition corregida (loadConfig async)
- Progreso: jobs "done" ahora muestran 100%

## v2.4.2-alpha — VRAM calculator reactivo (2026-06-10)

### Fixed
- VRAM calculator: reactividad Svelte 5 corregida

## v2.4.1-alpha — VRAM calculator + progreso (2026-06-09)

### Fixed
- VRAM calculator: reconoce nombres completos de modelo
- VRAM calculator: incluye segment_size, overlap, batch_size
- Progreso: barra de progreso lee valor real del queue status

## v2.4.0-alpha — Fase 7: Limpieza de código (2026-06-09)

### Changed
- Eliminadas ~880 líneas de código muerto (Go, Svelte, Python)
- 3 duplicidades unificadas
- 4 requirements.txt consolidados en 2

## v2.3.8 — Timestamps reales (2026-06-09)

### Fixed
- Timestamps de docker logs ahora parsean timestamp real de nginx

## v2.3.7 — Timestamps únicos (2026-06-09)

### Fixed
- Timestamps únicos por línea en pestaña Servicios

## v2.3.6 — Upload + logs (2026-06-09)

### Fixed
- Upload no se quedaba en "uploading" (reactividad Svelte 5)
- Logs con nano decreciente

## v2.3.5 — Drag & drop persistente (2026-06-09)

### Fixed
- Archivos arrastrados ya no se pierden al refrescar
- Logs de pipeline visibles en ring buffer

## v2.3.4 — Presets completos + logs detallados (2026-06-10)

### Fixed
- Presets guardan estado COMPLETO del pipeline
- Errores de pipeline muestran stderr real

### Added
- Campo `service` en logs
- Panel de detalle al hacer clic en un log
- Pestaña "Servicios" con logs de docker

## v2.3.3 — Persistencia + logs (2026-06-10)

### Fixed
- Model configs persisten en /config/model_configs/
- Presets de usuario persisten en /config/presets_user.json
- Errores persistentes con botón copiar

### Added
- Sistema de logs con ring buffer (GET /api/logs)

## v2.3.2 — Regresiones corregidas (2026-06-09)

### Fixed
- Rubberband paths rotos en contenedor
- chmod 0755 impedía escritura uid 1000
- Stale download status
- Path traversal en handlePitchFileServe
- copyFile sin Sync()

## v2.3.1 — Bugfix masivo (2026-06-09)

### Fixed
- Pitch shift: paths del host vs contenedor
- Inyección de código Python en descarga HF
- Path traversal en upload
- Race conditions en presets y descargas

## v2.3.0 — Pitch shift + subgrupos (2026-06-09)

### Added
- Pitch shift post-procesamiento con subgrupos
- Múltiples subgrupos independientes por canción

### Removed
- PipelinePanel redundante
- ConfigPanel
- Selector de presets duplicado

## v2.2.0 — Interfaces unificadas (2026-06-08)

### Added
- ModelDownloader y ModelManager en pantalla completa
- Catálogo HF integrado en ModelDownloader
- Presets persistentes en backend (API REST)

### Fixed
- Versión centralizada desde VERSION file

## v2.1.1 — Catálogo UVR funcional (2026-05-31)

### Fixed
- 4 bugs críticos en ModelDownloader
- Tamaños de archivo reales vía HTTP HEAD
- GPU info vía PyTorch (no nvidia-smi)
