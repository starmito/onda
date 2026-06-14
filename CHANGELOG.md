# Changelog
## v2.9.1 (2026-06-14) — Recovery + permisos no-root + pitch fix + stem display

### Added
- **Format友好的 nombres de stems pitch**: los stems procesados ahora se muestran como `vocals (+1)` o `drums (-2)` en vez del nombre raw `vocals_pitch+1.wav` (ResultsPanel, PitchPage)

### Fixed
- **Pitch shift permissions (recuperado del stash)**: el directorio de salida se crea dentro del contenedor `onda` con `docker exec mkdir -p` en vez de `os.MkdirAll` en el host. Rubberband (uid 1000) ahora puede escribir correctamente resolviendo el bug de "solo drums se reproduce"
- **Logging detallado de pitch shift**: añadidos logs info/debug/error en cada paso del pitch shift para facilitar debugging

### Changed
- **Todos los servicios corren como user no-root (UID 1000)**:
  - Contenedor `onda`: ahora ejecuta Python/PyTorch como usuario `starmito` (no root)
  - Contenedor `onda-gui`: nginx ahora corre como `starmito` (su-exec 1000:1000), Go backend ya corría como 1000:983
  - nginx.conf: directiva `user starmito;` añadida
  - entrypoint.sh: `exec su-exec 1000:1000 nginx -g "daemon off;"` en vez de root
  - Dockerfiles: creación de usuario `starmito` en ambos contenedores

### Docs
- **CHANGELOG recuperado**: historial completo del proyecto (v2.1.1→v2.7.12) restaurado desde git, limpiado de prefijos de línea corruptos


## v2.7.12 (2026-06-11)

### Fixed
- Barras de peak meter ahora muestran RMS en vivo (no el valor pico topeado)
- Línea blanca vertical (peak marker) sobre cada barra indicando el pico máximo
- Etiquetas dB superiores siguen mostrando el valor pico acumulado
- Ancho fijo de 100px en .stem-name para alineación vertical de todos los peak meters

### Changed
- VERSION actualizado a v2.7.12 (no reflejaba cambios previos)
## v2.9.1 (2026-06-14) — Recovery + permisos no-root + pitch fix + stem display

### Added
- **Format友好的 nombres de stems pitch**: los stems procesados ahora se muestran como `vocals (+1)` o `drums (-2)` en vez del nombre raw `vocals_pitch+1.wav` (ResultsPanel, PitchPage)

### Fixed
- **Pitch shift permissions (recuperado del stash)**: el directorio de salida se crea dentro del contenedor `onda` con `docker exec mkdir -p` en vez de `os.MkdirAll` en el host. Rubberband (uid 1000) ahora puede escribir correctamente resolviendo el bug de "solo drums se reproduce"
- **Logging detallado de pitch shift**: añadidos logs info/debug/error en cada paso del pitch shift para facilitar debugging

### Changed
- **Todos los servicios corren como user no-root (UID 1000)**:
  - Contenedor `onda`: ahora ejecuta Python/PyTorch como usuario `starmito` (no root)
  - Contenedor `onda-gui`: nginx ahora corre como `starmito` (su-exec 1000:1000), Go backend ya corría como 1000:983
  - nginx.conf: directiva `user starmito;` añadida
  - entrypoint.sh: `exec su-exec 1000:1000 nginx -g "daemon off;"` en vez de root
  - Dockerfiles: creación de usuario `starmito` en ambos contenedores

### Docs
- **CHANGELOG recuperado**: historial completo del proyecto (v2.1.1→v2.7.12) restaurado desde git, limpiado de prefijos de línea corruptos

## v2.8.0 (2026-06-14) — Presets reales + player unificado + pipeline chaining

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
- **Nginx location `/input_rubberband/`**: serves uploaded pitch audio files with CORS and proper MIME types
- **`pitchInputDownloadUrl()`** in api.ts: URL builder for pitch upload serving
- **`deletePitchUpload()`** in api.ts: client-side function for deleting uploaded pitch files
- **`DELETE /api/uploads/pitch/{name}`** endpoint in Go backend: deletes files from `input_rubberband/` with path traversal protection
- **Full per-file player in PitchPage.svelte**: each uploaded audio file now shows an independent player with:
  - Waveform visualization (real audio data on first play, deterministic fallback)
  - Play / Pause / Stop transport controls
  - Seek slider with time display (current / duration)
  - Volume slider with percentage label
  - Download button (⬇)
  - Delete button (🗑) with confirmation and server-side cleanup
  - Upload status (uploading/ready/error) with toast notifications

### Fixed
- **Entrypoint.sh**: `mkdir -p /config/model_configs` now fails gracefully when /config is root-owned

### Changed
- **VERSION** aligned to `v2.6.4` (consistent `v` prefix across backend, frontend, pipeline)

### Added
- **Endpoint `POST /api/upload/pitch`**: guarda archivos en `input_rubberband/` independiente
- **`uploadPitchAudio()`** en api.ts para el frontend
- **Volumen `input_rubberband`** en docker-compose (persistente)
- **Prop `onError`** en PipelineView para errores visibles (toast)

### Fixed
- **PipelineView**: si el preset no existe en el servidor, muestra error en vez de fallar silenciosamente
- **Personalizado**: cambiar preset en el desplegable ahora actualiza `selectedPresetName` correctamente (prop `onPresetChange`)
- **PitchPage**: ahora sube a `input_rubberband/` en vez de a la cola general

## v2.6.2-alpha — Refactor de pestañas: 4 presets directos, Personalizado, PitchPage (2026-06-10)

### Added
- **Nueva pestaña "Personalizado"** en el sidebar — con selector de presets para elegir qué preset ejecutar
- **PitchPage.svelte** — nueva página para Cambiar Tono con resultados existentes arriba y dropzone independiente abajo
- **IconUser** en icons.ts

### Changed
- **Sidebar reorganizado**: 4 presets hardcodeados (Separador Voces Total ⭐, Eliminador de Voz 🎤, Separador Completo 〰️, Solo Instrumentos 🎸) + Personalizado 👤
- Cada preset se ejecuta directamente al pulsar ▶ Ejecutar, sin selector de presets
- Sidebar ya no carga presets dinámicamente de la API
- **PipelineView**: nueva prop `hidePresetSelector` — cuando true, oculta el selector de presets y muestra botón ejecutar directo con barra de progreso

### Fixed
- **PipelineEditor**: los 4 presets predefinidos están bloqueados para eliminación (muestran 🔒)
- **Persistencia**: font-size y scale ahora se cargan al iniciar la app (no solo al entrar a Ajustes → Interfaz)
- **Sidebar texto**: nombres ahora envuelven a 2 líneas correctamente (white-space: normal)

## v2.6.1-alpha — Pulido UI: colores púrpura, iconos SVG, sidebar vertical, layout fluido (2026-06-10)

### Fixed
- **Títulos de preset**: Ahora muestran el nombre original (ej. "Separador Voces Total") en vez del ID con guiones ("separador-voces-total")
- **Paleta de colores**: Reemplazado acento azul (#00d4ff) por púrpura (#6c5ce7) en toda la interfaz — checkboxes, botones, gradientes, bordes, barras de progreso
- **Iconos SVG**: Reemplazados emojis por iconos SVG line-art al estilo vocalremover.org (18 iconos: menú, estrella, música, tono, tempo, DAW, ayuda, ajustes, subida, modelo, descarga, presets, logs, onda, cerrar, refrescar)
- **Sidebar vertical**: Items cambian a layout vertical (icono arriba, texto debajo) con borde activo inferior púrpura
- **Layout fluido**: Interfaz ocupa todo el viewport y escala con la ventana. Eliminado `max-width: 800px` — contenido principal limitado a 900px para legibilidad

## v2.6.0-alpha — Rediseño UI con sidebar vertical al estilo vocalremover.org

## v2.5.1 (2026-06-09)

### Added
- **Default preset persistente**: endpoint `GET/POST /api/presets/default` que guarda el preset predeterminado en `/config/default_preset.json`. El frontend lo carga automáticamente al iniciar.
- **Botón "Establecer como predeterminado"** en el Gestor de Presets, con confirmación visual verde.
- **Reorganización del Gestor de Presets** en dos categorías: "Crear Presets" (configuración + guardar) y "Editor de Presets" (un solo selector para establecer predeterminado y eliminar).

### Changed
- **Selector de presets unificado**: un solo dropdown en "Editor de Presets" sirve para ambas acciones (predeterminado y eliminar). Eliminado el selector duplicado de la sección de eliminar.
- Botón "Ejecutar" en PresetsPanel se deshabilita si no hay preset seleccionado.

## v2.5.0-alpha (2026-06-09)

### Added
- **Botón 🎛 Gestor de Presets** en la UI principal que abre el editor en modal fullscreen.
- **PresetsPanel**: nuevo componente con selector de preset, botón Ejecutar y barra de progreso integrada.
- **Cabecera de cola**: fila de encabezado con checkbox maestro, columnas "Título", "Progreso" y "Estado".
- **Banner de confirmación**: tras guardar un preset, banner verde "✅ Preset guardado correctamente" durante 5 segundos.
- **Sección "🗑 Eliminar Presets"** con confirmación explícita antes de borrar.

### Changed
- **PipelineEditor renombrado** a "Gestor de Presets" con etiquetas descriptivas: "Modelo separador de Voces/Stems", "Separación de Voces/Stems".
- **Botón Guardar Preset** grande y centrado (💾), con estilo verde prominente.
- **Ventana de Logs**: convertida a modal fullscreen unificado (mismo patrón que ModelManager/ModelDownloader).
- **✕ reemplaza "← Volver"** en ModelManager y ModelDownloader para consistencia visual.

### Fixed
- **Ventanas fullscreen unificadas**: PresetEditor y Logs usan `.fullscreen` (pantalla completa), mismo patrón que ModelManager. Eliminado `modal-overlay`/`modal-panel`.
- **Barra de progreso en idle**: ya no muestra 100% falso — solo se activa con `status === 'running'`.
- **PresetsPanel estrecho**: anchura ampliada para mejor legibilidad.
- **Botón ✕ alineado**: header flex con ✕ a la izquierda y título centrado en todas las ventanas.
- **Refresco de presets**: al cerrar el Editor, la lista de presets se recarga automáticamente.
- **Tamaños de ventana inconsistentes**: todas las modales usan las mismas dimensiones y estilos.

## v2.4.4 (2026-06-10)

### Fixed
- **Barra de progreso individual**: Python escribe `pipeline_status.json` directamente en cada chunk (~1% por actualización). Eliminado el frágil `report_progress` en bash.
- **Barra de progreso total**: solo cuenta jobs del batch actual (ignora jobs históricos). Peso igual por paso: 1 canción × 2 pasos = 50% cada paso, 2 canciones × 1 paso = 50% cada una.
- **Polling rápido**: frontend consulta `/api/queue/status` cada 500ms (eventos) y 200ms (pipeline_status.json).
- **Pestaña Servicios**: sin auto-refresh (solo botón manual) para permitir revisar logs antiguos.
- **`set -u` crash**: variables en heredocs de `report_progress` usan `${VAR:-default}` para evitar crash por unbound variable.
- **Timestamps**: `LogWithNano()` con nano decreciente por línea para orden correcto en logs.
- **Filtro "Todos"**: `<select>` usa `onchange` + `parseInt()` en vez de `bind:value` para evitar coerción string↔number.
- **Versión frontend**: `ONDA_VERSION` se inyecta desde entorno; Dockerfile multi-stage sin valor fijo.
- **pipeline_status.json**: se limpia al iniciar pipeline (ya no muestra estado "done" residual).
- Reporte de progreso inicial en ViperX y Demucs (barra arranca en 0%, no vacío).
- Progreso intermedio para Demucs (conteo de stems generados vs esperados).

## v2.4.3-alpha (2026-06-10)

### Fixed
- VRAM: race condition corregida (loadConfig async causaba primera llamada con defaults)
- Progreso: jobs "done" ahora muestran 100% en vez de 0%
- Progreso: barra total calculada ponderando todos los jobs en cola

## v2.4.2-alpha (2026-06-10)

### Fixed
- VRAM calculator: reactividad Svelte 5 corregida (sliders ahora disparan recálculo)
- Device indicator: muestra "Ejecutando en GPU/CPU" durante inferencia activa
- Eliminado banner GPU estático del header (no aportaba información)

## v2.4.1-alpha (2026-06-09)

### Fixed
- VRAM calculator: reconoce nombres completos de modelo (BS_Roformer_Viperx, no solo "viperx")
- VRAM calculator: incluye segment_size, overlap, batch_size en la fórmula
- Progreso: barra de progreso lee valor real del queue status (ya no se queda en 0%)
- Device indicator: muestra "Ejecutando en GPU/CPU" durante inferencia activa

## v2.4.0-alpha (2026-06-09)

### v2.4.0-alpha.2 (2026-06-09)

- Fix: VRAM calculator UI ahora usa endpoint del backend (chunk_size y shifts afectan)
- Feat: indicador GPU/CPU en la interfaz
- Fix: progreso de inferencia usa set_progress_bar nativo de UVR (por chunk, no cada 10)
- Feat: auto-refresh de logs (3s Eventos, 5s Servicios) + botón refrescar manual

- Fix: VRAM calculator ahora incluye chunk_size en ViperX/Roformer
- Fix: VRAM calculator ahora incluye shifts en modelos Demucs
- Feat: progreso por paso en la cola (Paso 1/2 ViperX 65%)
- Fix: timestamps de pipeline verificados (LogWithNano intacto)

### v2.4.0-alpha.1 (2026-06-09)

- Fix: presets API restaurada (regresión por limpieza agresiva en 4ab3cc1)

### Fase 7 — Optimización y limpieza

- Eliminadas ~880 líneas de código muerto (Go, Svelte, Python)
- 3 duplicidades unificadas (copyFile, loaders catálogo, groupBySong)
- 4 requirements.txt consolidados en 2 (NVIDIA + AMD)
- 5 funciones API + 4 interfaces huérfanas eliminadas del frontend
- Componente PipelinePanel.svelte (441 líneas) eliminado
- Dependencia @tauri-apps/api no usada eliminada
- Presets legacy + tests rotos eliminados
- onda.sh reparado (referencias rotas a compose files)
- validate.sh reparado (bug en chequeo git)
- CSS huérfano eliminado

## v2.3.8 (2026-06-09)

### Fixed
- Timestamps de docker logs mostraban hora actual — ahora se parsea el timestamp real de nginx
- Filtro "Todos" en Servicios no funcionaba por coerción string↔number

## v2.3.7 (2026-06-09)

### Fixed
- Timestamps de docker logs idénticos en pestaña Servicios — ahora cada línea tiene nano decreciente

## v2.3.6 (2026-06-09)

### Fixed
- Upload de archivos se quedaba en "uploading" hasta refrescar — reactividad Svelte 5 corregida
- Timestamps de logs de pipeline idénticos — ahora cada línea tiene nano decreciente
- Eventos mostraba líneas detalladas de pipeline — ahora solo resúmenes
- Servicios no tenía filtro — ahora dropdown: Últimos 50/100/500/Todos

## v2.3.5 (2026-06-09)

### Fixed
- Archivos arrastrados se pierden al refrescar — ahora se suben al servidor inmediatamente al arrastrar
- Logs de pipeline/inferencia no visibles — ahora cada línea de stdout/stderr de pipeline.sh se guarda en el ring buffer

## v2.3.4 (2026-06-10)

### Fixed
- Presets guardan y restauran el estado COMPLETO del pipeline (pasos activos, stems, modelos)
- Errores de pipeline muestran la salida real de stderr en los logs (antes solo "exit status 1")

### Added
- Campo `service` en logs (backend, pipeline, inference)
- Panel de detalle al hacer click en un log (mensaje completo + metadata + botón copiar)
- Pestaña "Servicios" con logs reales de docker (onda, onda-gui)
- Colores por servicio en el visor de logs

## v2.3.3 (2026-06-10)

### Fixed
- Model configs se pierden al redeploy — ahora persisten en /config/model_configs/
- Presets de usuario no persisten — ahora guardados en /config/presets_user.json
- Errores desaparecen muy rápido — ahora banner persistente con botón copiar

### Added
- Sistema de logs con ring buffer en memoria (GET /api/logs)
- Panel de logs en la UI con colores y detalle expandible

## v2.3.2 — Regresiones corregidas + bajos + UI 🧹

### 🔴 Regresiones de v2.3.1 corregidas

- **Rubberband paths rotos en contenedor**: `findProjectRoot()` devolvía `/` dentro del contenedor, causando que `strings.Replace` eliminara la barra inicial. Ahora se pasan paths absolutos del contenedor directamente.
- **chmod 0755 impedía escritura uid 1000**: rubberband corre como uid 1000 en el contenedor `onda`. Restaurado `0777` en el directorio de salida del pitch.

### 🟠 Medios corregidos

- **Stale download status**: dos modelos con misma URL de dependencia se sobrescribían. Key compuesta `filename@URL`.
- **pitchStr sin sanitizar** en `handlePitchFileServe`. Añadido `filepath.Clean()`.
- **Check `_pitch` demasiado amplio**: bloqueaba archivos legítimos con `_pitch` en el nombre. Ahora verifica el directorio padre.

### 🟢 Bajos backend corregidos

- **copyFile sin Sync()** → añadido `out.Sync()` antes de `Close()`
- **404 vs 200 vacío**: cuando un directorio no existe, devuelve 404 en vez de 200 con array vacío
- **pip install silencioso**: ahora loggea errores de instalación
- **Verificación de pip**: comprueba que pip existe antes de usarlo
- **Upload sin check de disco**: verifica espacio libre antes de parsear 500MB en RAM
- **Catálogo HF con sync.Once**: ahora se recarga en cada request

### 🔵 UI frontend corregidos

- **Progreso de cola no se actualizaba**: polling ahora actualiza pipelineStep, pipelineModel, pipelineEta
- **AudioContext por waveform**: compartido un solo OfflineAudioContext para decodificar waveforms
- **$inspect residual en producción**: eliminado
- **onDestroy sin cancelación**: añadido AbortController para requests de pitch
- **Errores red silenciados**: ahora muestra toast al fallar carga de subgrupos
- **syncSubgroupGains ignoraba pausa**: ahora aplica cambios de volumen incluso si el player está pausado

## v2.3.1 — Bugfix masivo: pitch shift, seguridad, player 🔧

### 🐛 Bugs críticos corregidos

- **Pitch shift no funcionaba**: `docker exec rubberband` recibía paths del host en vez de paths del contenedor (`/output/...`). Corregido convirtiendo paths automáticamente. Añadido timeout de 60s.
- **PitchResponse devolvía paths absolutos del servidor**: el frontend recibía `/home/starmito/.../output/...` en vez de URLs HTTP. Nuevo endpoint `GET /api/pitch/files/{song}/{pitch}/{file}` para servir archivos de subgrupos.
- **Reproductor de subgrupos no cargaba audio**: las URLs de descarga no coincidían con la estructura de directorios del servidor. Nuevo helper `pitchDownloadUrl()` en el frontend.
- **Waveform y enlaces de descarga rotos**: usaban `stem.path` (ruta absoluta del servidor) como href. Corregido usando URLs HTTP.

### 🟠 Bugs graves corregidos

- **Inyección de código Python en descarga HF**: el parámetro `repo` se interpolaba directamente en un script Python sin sanitizar. Corregido escapando comillas simples.
- **Path traversal en upload**: `header.Filename` iba directo a `filepath.Join` sin sanitizar. Corregido con `filepath.Base()`.
- **handleDeleteFile**: permitía borrar archivos en subdirectorios arbitrarios de `output/`. Corregido rechazando paths que contengan `_pitch`.
- **stopSubgroup()**: no reseteaba `duration`, `buffers`, `gainNodes`. El slider de seek mostraba tiempo incorrecto tras stop.
- **toggleSubgroupSolo()**: no silenciaba los demás stems cuando se activaba Solo. Nueva función `syncSubgroupGains()`.
- **handleSubgroupSeekChange()**: no reiniciaba el timer de reproducción tras hacer seek. Corregido cancelando y reiniciando `startSubgroupTimer()`.

### 🟡 Bugs medios corregidos

- **Chmod 0777** en directorios de pitch → 0755
- **Race condition en presets**: `saveUserPresets()` fuera del write lock. Movido dentro.
- **handleDeleteModel**: error de limpieza de config silencioso → incluye warning en respuesta
- **Prefix check sin trailing separator**: `HasPrefix(absPath, outputPrefix)` podía dar falso positivo
- **loadModelConfig**: errores de parseo silenciosos → ahora logea warning
- **Colisión en tracker de descargas**: dos modelos con misma dependencia se sobrescribían. Key compuesta `filename@URL`.
- **handleSeekInput**: no actualizaba `pauseOffset` → al pausar tras arrastrar slider, la reanudación iba a posición incorrecta
- **$effect loadPitchSubgroups**: race conditions con respuestas fuera de orden. Añadido contador de versión.
- **waveformDrawn**: memory leak (Set nunca se limpiaba). Limpiado en `onDestroy`.
- **Último stem de subgrupo**: al borrarlo, el subgrupo quedaba vacío. Ahora se elimina automáticamente.
- **buildConfig()**: no incluía `preset` → siempre usaba default. Corregido.
- **demucsVocalsAutoDisabled**: no chequeaba `demucsEnabled` → se autodesactivaba innecesariamente.

## v2.3.0 — Pitch shift + subgrupos + limpieza de pipeline 🎛️

### 🎵 Nuevas funcionalidades

- **Pitch shift post-procesamiento**: nuevo endpoint `POST /api/pitch`. Slider de tono debajo de cada grupo de stems en ResultsPanel con botón "🎵 Cambiar tono".
- **Subgrupos con pitch**: al cambiar el tono, se genera un subgrupo anidado con reproducción independiente (▶⏸⏹ seek). Cada stem tiene Mute, Solo, Volumen, Descargar y Eliminar.
- **Múltiples subgrupos**: se pueden tener varios subgrupos por canción (+2, -5, +12...) independientes entre sí.
- **Persistencia**: los subgrupos se guardan en el servidor y sobreviven a recargas del navegador.
- **Drums sin procesar**: los stems de drums se copian sin aplicar pitch shift.

### 🗑️ Limpieza

- **PipelinePanel eliminado**: la sección redundante con ViperX/Demucs/Pitch ya no se muestra. PipelineEditor es la única interfaz de configuración del pipeline.
- **ConfigPanel eliminado**: el desplegable "Configuración avanzada" no estaba conectado al pipeline real.
- **Selector de presets duplicado eliminado**: PipelinePanel ya no tenía su propio selector — todo se gestiona desde PipelineEditor.

### 🐛 Bugs corregidos

- **Rubberband en contenedor equivocado**: el backend ejecutaba `rubberband` como comando local en `onda-gui` donde no estaba instalado. Corregido ejecutando dentro del contenedor `onda` via `docker exec`.
- **Permisos de escritura**: el directorio de salida del pitch se creaba como root pero rubberband corre como uid 1000. Corregido con `os.Chmod(outDir, 0777)`.
- **Download URL de subgrupos**: usaba la API incorrecta (404). Corregido usando la ruta directa servida por nginx.
- **Delete stem individual**: daba 405 por usar DELETE en estáticos de nginx. Nuevo endpoint `DELETE /api/pitch/{song}/{pitch}/{file}`.
- **Waveform de subgrupos**: no se dibujaba por usar URL incorrecta. Nueva función `waveformFromUrl` con ruta directa.
- **Path traversal en POST /api/pitch**: no tenía guard de seguridad a diferencia de GET y DELETE.

### 🎨 UI

- **Reproductores de stems responsive**: los botones ya no se salen del cuadro al hacer zoom en el navegador (flex-wrap, tamaños reducidos).
- **SVG del editor corregido**: altura dinámica para que los 3 stems de Demucs se vean completos (ya no se corta el tercero).

## v2.2.0 — Interfaces unificadas + pantalla completa 🖥️

### 🎨 UI (08-jun-2026)

- **ModelDownloader y ModelManager convertidos a pantalla completa** con botón "← Volver" en la cabecera (antes eran paneles laterales deslizantes).
- **Catálogo HF integrado** en ModelDownloader — los modelos de Politrees/UVR_resources ahora aparecen en la misma pantalla que los modelos UVR.
- **Filtros de fuente**: 3 botones tipo pill debajo del buscador — "Todas las fuentes", "UVR", "Hugging Face" — para mostrar solo los modelos de una fuente.
- **Badge de fuente** en cada modelo: etiqueta "UVR" (verde) o "HF" (azul) al lado del botón de descargar.
- **Descarga .yaml automática**: al descargar un checkpoint (.ckpt/.pth) de HF, también se descarga su archivo .yaml asociado.
- **API extendida**: `downloadModel()` acepta `filename` opcional; nueva función `getHfCatalog()`.
- **CatalogPanel eliminado**: toda la funcionalidad absorbida en ModelDownloader.

### 🐛 Bugs corregidos (08-jun-2026)

- **Versión centralizada**: ahora se lee del archivo `VERSION` en la raíz del proyecto. El backend usa `api.Version` (leído vía `init()`), el frontend lo obtiene del health endpoint. Ya no hay texto hardcodeado en `App.svelte`, `server.go` ni `main.go`.
- **Build frontend en Docker**: el Dockerfile ahora es multi-stage con un `frontend-builder` que compila el Svelte dentro del Docker build. Ya no necesita rsync ni build manual.
- **htdemucs_ft con 0MB corregido**: ahora muestra correctamente 2800 MB en la lista de modelos instalados (su VRAM real).
- **Skill de despliegue actualizada**: documenta el workflow correcto con build multi-stage y versión centralizada.
- **Presets persistentes en backend**: nueva API REST (`GET/POST/DELETE /api/presets`) con persistencia en archivo JSON. PipelineEditor guarda/carga presets desde el servidor. Se unifican presets built-in (turbo, balance, master, ultimate) con los del usuario.
- **Selector de presets duplicado eliminado**: PipelinePanel ya no tiene su propio selector de presets — todo se gestiona desde PipelineEditor.
- **ConfigPanel eliminado**: el desplegable "Configuración avanzada" no estaba conectado al pipeline real.
- **Reproductores de stems responsive**: los botones ya no se salen del cuadro al hacer zoom en el navegador (flex-wrap, tamaños reducidos).
- **SVG del editor corregido**: altura dinámica para que los 3 stems de Demucs se vean completos (ya no se corta el tercero).

## v2.1.1 — Catálogo de modelos UVR funcional + fixes de UI ✅

### 🐛 Catálogo de modelos — 4 bugs críticos arreglados (31-may-2026)

El catálogo de descarga de modelos (ModelDownloader) no funcionaba por 4 bugs encadenados:

- **Fix (crítico):** `each_key_duplicate` — 10 modelos del catálogo UVR tenían nombres/filenames duplicados. El `{#each}` de Svelte 5 craseaba el componente entero. Solución: eliminar la key del each + deduplicación inteligente.
- **Fix (crítico):** `state_unsafe_mutation` — la función `groupedCatalog` mutaba `display_name` dentro de `$derived`. Svelte 5 prohíbe mutar `$state` en derivados. Solución: `flatMap` + spread operator para crear copias.
- **Fix:** Catálogo mostraba "Cargando..." infinito — el `$effect` de Svelte 5 no disparaba reactividad con `catalog = data`. Solución: `catalog = [...data]` (spread assignment).
- **Fix:** Botón "Descargar" siempre deshabilitado — backend envía `download_url`, frontend esperaba `huggingface_repo`. Solución: mapeo en `getModelCatalog()`.

### 📏 Tamaños de archivo reales (31-may-2026)

- **Fix:** Los 98 modelos del catálogo mostraban 0 MB. Script Python que obtiene tamaños vía HTTP HEAD (GitHub Releases, HuggingFace, Facebook CDN) + filesystem para modelos ya descargados.
- **Fix:** Modelos built-in de Demucs (`htdemucs_ft`, `htdemucs`, `htdemucs_6s`, `hdemucs_mmi`) ahora muestran su tamaño VRAM real (1400–3200 MB).
- **Fix:** URL rota de `deverb_bs_roformer` (typo en repo name + path incorrecto).

### 🧹 Limpieza del catálogo (31-may-2026)

- **Fix:** 31 sub-componentes UUID de Demucs (`.th` internos) ocultos del catálogo. Son archivos que Demucs descarga automáticamente.
- **Fix:** Deduplicación por `display_name` — los archivos `.yaml` (0 MB) ya no aparecen junto a los `.ckpt` (X MB) del mismo modelo.
- **Fix:** Versiones v2/v3 de Demucs renombradas: `demucs (v2)` vs `demucs (v3)` para evitar confusión.

### 🎨 UI (31-may-2026)

- **Fix:** Panel de ModelDownloader ampliado de 340px → 440px (+30%) para mejor visibilidad de nombres largos.
- **Fix:** Icono favicon añadido (`public/icon.png`).

### 🔧 Eliminación de modelos (31-may-2026)

- **Fix:** El botón de papelera ahora borra el archivo físico real (antes solo lo quitaba de la lista en memoria).
- **Fix:** Volumen `/models` cambiado de `:ro` a lectura-escritura para permitir borrar.

### Commits (10 fixes)

`c262734`, `ac6361a`, `615bab7`, `bcc5628`, `f62498f`, `edbebd7`, `b042382`, `005c43b`, `185d765`, `37e8645`

### 🐛 Bug fixes en GPU info y frontend (1-jun-2026)

- **Fix:** `vram_used_mb` desaparecía del JSON cuando valía 0 (GPU idle). Quitado `omitempty` del struct Go.
- **Fix:** VRAM calculator (`/api/gpu/vram-calculator`) siempre devolvía 0. Ahora busca en catálogo UVR + fallback 2000 MB.
- **Fix:** Header mostrando `v2.0.0-alpha` hardcodeado → `v2.1.1`.
- **Fix:** `API_BASE` hardcodeada a `192.168.1.87` → URLs relativas (funciona desde cualquier IP).

### 📦 Catálogo y descargas (1-jun-2026)

- **Feat:** Filtrado de modelos `size_mb=0` (config files) del catálogo visible. De 98 → 72 modelos.
- **Feat:** Descarga de dependencias: al bajar un modelo (.ckpt/.pth) se descargan automáticamente sus archivos .yaml asociados.
- **Feat:** Añadido `hf_models.json` con 380 modelos del repo HuggingFace Politrees/UVR_resources organizados en 11 categorías.

### 📦 Catálogo HF — Tamaños reales y normalización de nombres (8-jun-2026)

- **Feat:** Tamaños reales de los 380 modelos HF obtenidos vía API de HuggingFace (300 checkpoints con tamaño, 80 YAML de configuración). Todos resueltos correctamente sin errores.
- **Feat:** Normalización de nombres de modelos stem — 8 modelos `kuielab_*` ahora muestran su fuente (ej: `kuielab_a (bass stem)`).
- **Fix:** `ModelManager.svelte` ahora muestra `display_name || name` en selectores y cabeceras.

---

## v2.1.0-alpha — Fase 5: Modelos configurables + Editor visual de pipeline ✅

### Fixes recuperados del 28-may + modelsBasePath (31-may-2026, sesión mañana)

Los commits originales de estos fixes (46898d0-c8c52fd) se perdieron en un git reset. Reimplementados hoy.

- **Fix (crítico):** `handleSeparate` ahora ejecuta pipeline.sh dentro del contenedor `onda` (`docker exec onda bash /pipeline.sh`) en vez de en `onda-gui`. Esto resuelve "demucs: command not found" y "inference_universal.py not found".
- **Fix:** `Dockerfile` de onda copia `pipeline.sh` → `/pipeline.sh` en la imagen.
- **Fix:** `resolveModelDir()` — traduce nombres de modelo a rutas de directorio en el contenedor (`model_bs_roformer...` → `/app/models/VR_Models/BS_Roformer_Viperx`). Resuelve "ViperX model not found".
- **Fix:** Dual-config loading — `handleSeparate` carga configs de ViperX y Demucs por separado (antes solo cargaba una). Demucs ya no ignora `shifts`/`segment`/`jobs` guardados.
- **Fix:** `PipelineStatus` +8 campos (`segment_size`, `overlap`, `chunk_size`, `batch_size`, `device`, `shifts`, `demucs_segment`, `jobs`) — el frontend ahora recibe los flags reales.
- **Fix:** Error handler preserva flags — al fallar el pipeline, lee el JSON existente (escrito por pipeline.sh vía trap) y solo actualiza `status` + `error`.
- **Fix:** `modelsBasePath` ahora es dinámico — detecta `/models` en Docker, usa `ONDA_MODEL_DIR`/`MODEL_DIR` si existen, fallback al path legacy. Resuelve que `listModels()` solo devolvía htdemucs_ft.
- **Fix:** `isDemucs` scope en frontend — separado en `isDemucs` (solo htdemucs_ft) e `isDemucsFamily` (todos). Demucs ONNX ya no muestra sliders inaplicables.
- **Fix:** VRAM base sin ajustes — `estimateVRAM()` devuelve valores raw (sin aplicar config guardada). El frontend aplica sliders → sin doble multiplicación.
- **Commits:** effd554, 858557f, abc7257, 32580b8

### StatusBar versionado + CORS fix + GPU check (31-may-2026)

- **Fix:** CORS duplicado — nginx ya no añade `Access-Control-Allow-Origin` (solo el backend Go), resolviendo indicadores rojos en navegador
- **Feat:** `StatusBar.svelte` reescrito: muestra Backend, Frontend, Pipeline (apps primero) + GPU, Disco, Docker (infra) con versiones
- **Feat:** `version_mismatch` en `/api/health` — detecta y reporta divergencias entre backend, frontend y pipeline
- **Fix:** `handleHealth` en server.go completo: frontend version (lee `/usr/share/nginx/html/VERSION`), pipeline version (lee `/VERSION`), version_mismatch con detalle de componente conflictivo
- **Fix:** `checkGPU()` usa PyTorch en vez de `nvidia-smi` (el contenedor `onda` no lo tiene)
- **Fix:** `main.go` — flag `--addr` con default `:3001` (antes hardcodeado `:3000`, rompía nginx)
- **Fix:** versiones unificadas: `const version = "v2.1.0-alpha"` en server.go + `ONDA_VERSION=v2.1.0-alpha` en Dockerfile
- **Fix:** `entrypoint.sh` arranca backend Go (`/usr/bin/onda-backend serve --addr :3001`) en vez de Python
- **Fix:** Dockerfile multi-stage: `golang:1.26-alpine` (go.mod requiere >=1.26)
- **Fix:** Despliegue con `docker compose -f docker-compose.yml -f docker-compose.nvidia.yml` para acceso GPU
- **Refactor:** `docker-compose.yml` unificado — GPU integrada, un solo `docker compose up -d --build` levanta todo. Eliminados `docker-compose.nvidia.yml` y `.amd.yml`.
- **Feat:** Health check en `onda` (verifica CUDA con PyTorch). `onda-gui` espera con `condition: service_healthy`.
- **Chore:** Limpiado `.env` — eliminadas variables obsoletas (`GPU_TYPE`, `GPU_DOCKERFILE`). Solo queda `MODEL_DIR`.
- **Build:** `frontend/dist/` gitignored — construir con `npm run build` antes de `docker compose build`
- **Commits:** 6bc3c3e, af1d6c7, 39fdc39, ad78c1b, 99c9edd, 3bcfec0, 97a52e0

### Infraestructura unificada — paths, usuarios, permisos (31-may-2026, sesión tarde)

- **Fix:** Paths unificados — ambos contenedores (`onda`, `onda-gui`) mapean el mismo volumen `/models`. Sin paths divergentes ni mounts separados.
- **Fix:** Usuario `1000:1000` en `onda` — el pipeline se ejecuta como usuario del host, sin `--user` forzado en `docker exec` (hereda el `user:` del compose).
- **Fix:** `rm -rf` del output previo recuperado — seguro porque el pipeline corre como uid 1000 (mismo owner que los archivos).
- **Fix:** `onda-gui` usa root (necesario para entrypoint nginx/gestión de usuarios).
- **Fix:** `STATUS_FILE` unificado a `/output/pipeline_status.json` — antes `pipeline.sh` escribía en `/tmp/pipeline_status.json` pero `server.go` leía de `/output/pipeline_status.json` (mismatch de paths).
- **Refactor:** Eliminados `docker-compose.nvidia.yml` y `docker-compose.amd.yml` — un solo compose con GPU integrada vía `deploy.resources.reservations.devices`.
- **Refactor:** Sin `chmod 777`, sin `--user 0:0`, sin `docker cp` — todo resuelto mediante código y docker compose.
- **Commits:** 1d5c7d5, f9492be, 3fb1c69, 118b830

### Fixes VRAM — fórmula realista + NaN (31-may-2026, sesión tarde)

- **Fix (crítico):** Fórmula de VRAM estimada en ModelManager — cambiada de cadena multiplicativa a modelo aditivo. Antes: `base × (seg/256) × (1+overlap) × batch × (chunk/1024)` → ViperX con valores máximos daba 76.8 GB (factor 24×). Ahora: `(base + activationMemory) × batch` donde `activationMemory = base × 0.25 × (seg/256) × (1+overlap)`. ViperX máx: 7.8 GB, MelBand máx: 10.3 GB. El chunk_size ya no escala la VRAM del modelo (nunca debió — solo afecta al throughput de audio).
- **Fix:** NaN en barra de VRAM — cuando la GPU no estaba disponible, el backend Go omitía `vram_total_mb` del JSON (`omitempty` en struct tag). El frontend recibía `undefined`, las guardas `!== null` no protegían, y `undefined/undefined` → NaN → "NaN%". Solución: quitar `omitempty`, retornar HTTP 503 en vez de 200 cuando GPU no disponible, validar `gpu.ok` + `isFinite()` en frontend, y cambiar guardas a `== null`.
- **Commits:** 313fa20, 53ce03a
### GPU info via PyTorch + VRAM Demucs (31-may-2026, sesión tarde)

- **Fix:** GPU info ahora usa PyTorch (`torch.cuda`) vía `docker exec onda python3`. El contenedor `onda` (python:slim) no tiene `nvidia-smi`, lo que causaba `ok:false` y ocultaba la barra de VRAM en el frontend. Ahora obtiene VRAM total/usada/libre, nombre, uso% y temperatura desde PyTorch + pynvml.
- **Feat:** Fórmula VRAM para Demucs (htdemucs_ft) — considera `segment` (escala lineal vs default 7.8s) y `jobs` (escala sub-lineal: `1 + (n-1) × 0.3`). Shifts se ignora (procesamiento secuencial, no escala VRAM).
- **Fix:** `estimateVRAM()` en backend — eliminados todos los hardcodes por modelo (ViperX=3200, MelBand=4200, Polarformer=4800, etc.). Mediciones reales en RTX 5060 Ti muestran que los pesos en fp16 cargan 1:1 en VRAM vs disco (ViperX 609 MB disco → 616 MB VRAM). Nueva lógica: sizeMB para .ckpt/.pth, sizeMB×2 para ONNX, 2800 MB para htdemucs_ft.
- **Commits:** 9ea7793, f9a1149, f2b2d17

### Cola secuencial + Resultados acumulados (31-may-2026, sesión noche)

- **Feat:** Cola secuencial FIFO en backend — worker único consume del channel `jobQueue`. Cada `POST /api/separate` encola en vez de lanzar goroutine. Solo 1 pipeline ejecutándose a la vez → GPU sin saturar.
- **Feat:** `GET /api/queue/status` — estado de toda la cola (waiting/processing/done/error), ordenado por prioridad.
- **Feat:** Cola visible en frontend (PipelinePanel) — emojis de estado por canción, mensaje de error si falla.
- **Feat:** Resultados acumulados (ResultsPanel) — stems de cada canción aparecen como grupos independientes, no se reemplazan. Controles de reproducción/borrado por grupo.
- **Refactor:** Eliminado código obsoleto — `/api/status`, `/api/events` (SSE), `pipeline_status.json` único.
- **Commits:** e896323, 18b3335
+### Fixes cola — orden FIFO + persistencia (31-may-2026, sesión noche)
+
