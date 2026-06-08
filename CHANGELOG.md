# Changelog

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
- **PipelinePanel eliminado**: la sección redundante con ViperX/Demucs/Pitch ya no se muestra. PipelineEditor es la única interfaz de configuración del pipeline.
- **Pitch shift post-procesamiento**: nuevo endpoint `POST /api/pitch`. Slider de tono debajo de cada grupo de stems en ResultsPanel. Al cambiar el tono, se genera un subgrupo anidado con los stems procesados (+ drums sin procesar).

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
+- **Fix:** Orden FIFO estable en la cola — añadido `index` secuencial a cada job. Las canciones en waiting ya no cambian de posición aleatoriamente (el map de Go itera en orden aleatorio).
+- **Feat:** `GET /api/results` — lista stems en `/output/` agrupados por canción. `GET /api/inputs` — lista archivos en `/input/`.
+- **Fix:** Inputs y resultados persisten al recargar la página (F5). El frontend carga desde el filesystem al montar, no solo desde memoria.
+- **Commits:** 225247a

### Fixes finales — limpieza + límites (31-may-2026, sesión noche)

- **Fix:** Slider de segment para htdemucs_ft limitado a 7.8s (step 0.1). Antes permitía 60s, el modelo solo soporta 7.8s.
- **Fix:** `GET /api/results` devuelve `[]` en vez de `null` cuando no hay stems.
- **Fix:** `DELETE /api/inputs/{name}` — borrado físico de archivos de input desde la UI. Antes solo los quitaba de la lista visual.
- **Chore:** Limpiados archivos huérfanos de root en `/input/` y `/output/` (herencia del pipeline antiguo con `--user 0:0`).
- **Commits:** e10ac87, 29c4bd5, cb7d118, 2d7f4c9, 4ccf810

### Fase 6 — Catálogo UVR + Pantalla de descarga (31-may-2026, sesión noche)

- **Feat:** Catálogo UVR completo — 98 modelos (26 Roformer, 56 Demucs, 8 MDX, 8 SCnet) con URLs de descarga extraídas del repo oficial. Método: descarga directa (wget) desde GitHub Releases y HuggingFace.
- **Feat:** `GET /api/models/catalog` — devuelve el catálogo UVR con campo `downloaded: true/false` comparando con modelos instalados.
- **Feat:** `POST /api/models/download` con `source: "direct"` — descarga modelos desde URLs directas (wget) a la categoría correcta según el nombre del archivo.
- **Feat:** `ModelDownloader.svelte` — panel lateral con 3 pestañas: 📥 Descargar (catálogo UVR con checks ✅), 📤 Subir (dropzone para .ckpt/.pth/.onnx), ✅ Instalados (modelos locales).
- **Commits:** ab5dfb6, 23e5885, 26959c7

### Deploy-ready + defaults locales (30-may-2026)

- **docker-compose.yml:** paths configurables via `.env` (`MODEL_DIR`, `HOST_UID`, `HOST_GID`, `ONDA_PORT`), defaults locales (`./models`)
- **.env.example:** template documentado con todos los valores
- **Makefile raiz:** `make setup` (detecta GPU, crea .env, directorios), `make build`, `make up`, `make test`, `make validate`, `make clean`
- **scripts/download-models.sh:** guia de descarga de modelos ViperX, Demucs, ONNX desde HuggingFace
- **onda-gui/Makefile:** sin paths hardcodeados, usa `PROJECT_DIR` relativo
- **Limpieza:** `Dockerfile.v2` unificado como `Dockerfile`, eliminado `pipeline.sh.bak`, `.gitignore` mejorado

### Robustez del pipeline + Build validation (30-may-2026)

- **Fix:** Overlap float->int usa python3 (locale-independent), no awk — evita `ValueError: int('0.25')`
- **Fix:** `json.load()` de archivos de progreso parciales protegidos con `|| echo 0` — evita crash por race condition
- **Fix:** `ls *.wav` final protegido con `|| true` — evita falsos errores con pipefail
- **Fix:** Pre-flight validation antes de ViperX: verifica modelo e inference_universal.py existen
- **Feat:** `scripts/validate.sh` — validacion pre-build (sintaxis, archivos, anti-patrones, modelos)
- **Commits:** fe5d254, 8080bdc en v2.1.0-alpha

### Fixes post-Fase 5 (27-may-2026, sesión tarde) — 10 fixes
- **ModelManager → per-model config:** `model_configs/{model_name}.json` (14 archivos), no global. Endpoints: `GET/POST /api/models/{name}/config`
- **PipelineEditor interactividad:** Grafo SVG muestra nombres de modelo. Prop `hasFiles`. Toast en vez de alert.
- **Default YAML UVR:** 14 modelos con valores reales de dim_t, num_overlap, batch_size importados desde YAML en .87
- **display_name API:** Nombres amigables (BS_Roformer_Viperx, no model_bs_roformer_ep_317_sdr_12.9755)
- **Demucs reorganizado:** htdemucs_ft (PyTorch) en categoría "Demucs", ONNX stems en "Demucs ONNX"
- **Sliders min/max:** Valores numéricos + etiquetas Fast/-VRAM ↔ Quality/+VRAM en extremos
- **VRAM realista:** `vram_estimate_mb`: htdemucs_ft=2800, Kim_Vocal=800, ViperX=3200 MB
- **Parámetros Demucs PyTorch:** shifts (0-20), segment (0-60s), jobs (0-8) — solo visibles para htdemucs_ft
- **Chunk/Batch docs:** Añadido "No afecta a la calidad del resultado" en descripciones
- **ModelManager UX:** Selector de modelo con optgroups, sliders con descripciones, barra VRAM estimada

### 5.1 — Cablear presets → pipeline
- `pipeline.sh`: flags `--viperx-model PATH` (default: BS_Roformer_Viperx), `--demucs-model NAME` (default: htdemucs_ft), `--segment-size`, `--overlap`, `--batch-size`, `--device`
- `server.go`: `SeparateRequest.StemModel`, pasa modelos como flags al pipeline. Endpoints `POST/GET /api/models/config`
- `api.ts`: campos `vocal_model`, `stem_model`, `viperx_model`, `demucs_model`, `viperx_stems`, `demucs_stems`

### 5.2 — Editor visual de pipeline (`PipelineEditor.svelte`, 746 líneas)
- Selectores dropdown: ViperX (Roformer/VR_Arch) y Demucs (Demucs/MDX) con optgroups
- Checkboxes de stems por paso (vocals, instrumental, drums, bass, other)
- Auto-detección: ViperX activo + vocals → deshabilita vocals en Demucs con tooltip
- Grafo SVG inline del flujo con nodos activos (cyan) / inactivos (gris)
- Guardar/cargar/eliminar presets en localStorage con nombre personalizado
- Botón "Ejecutar" que construye config y lanza separación

### 5.3 — Gestor de modelos (`ModelManager.svelte`, 318 líneas)
- Panel lateral con sliders: segment size (64-1024), overlap (0-0.5), chunk size (0-4096), batch size (0-32)
- Dropdown device (cpu/cuda)
- `POST/GET /api/models/config` — persiste configuración en `model_config.json`
- Botón "Aplicar" con feedback visual de éxito/error

### 5.4 — Verificación
- Endpoints funcionales: `GET /api/models/config` (defaults), `POST /api/models/config` (guarda), `GET` (recupera)
- Go compila, TypeScript compila, Vite build exitoso
- 12 commits en `v2.1.0-alpha`, 233 commits totales, working tree limpio

## v2.0.0-alpha

### v2.0.0-alpha.9 — Simplificación + Pipeline inteligente + Bug fixes

#### Changed (Simplificación — 27-may-2026)
- **Fase 1:** Eliminado `inference/` (duplicado de `lib_v5/`), `frontend/src-tauri/`, `pipeline.go` (1.074 líneas), 15 componentes frontend obsoletos. Total: 10.955 líneas eliminadas
- **Fase 2:** Frontend reducido a 5 componentes Svelte: `App.svelte`, `PipelinePanel.svelte`, `ConfigPanel.svelte`, `ResultsPanel.svelte`, `StatusBar.svelte`
- **Fase 3:** Pipeline migrado de Go a Bash (`pipeline.sh`, 268 líneas). Backend Go ejecuta `bash pipeline.sh` vía `exec.Command`

#### Added
- Pipeline inteligente: cuando ViperX está activo, Demucs excluye automáticamente `vocals.wav` (duplicado) y `instrumental_viperx.wav` (intermedio). Pipeline completo = 4 stems finales
- Nuevo flujo: `original → ViperX → {vocals_viperx, instrumental_viperx → Demucs → {drums, bass, other}}`

#### Fixed
- **Bug:** Dead import `groupBySong` en ResultsPanel
- **Bug:** Memory leak en AudioContext de `drawRealWaveform()` — ahora usa `finally` para `.close()`
- **Bug:** Sin `onDestroy` en ResultsPanel — AudioContexts y animation frames ahora se limpian al desmontar
- **Bug:** `API_BASE` duplicado en ResultsPanel — centralizado en `api.ts`
- **Bug:** `fmtTime(0)` con lógica confusa (`!0 === true` por coincidencia)
- **Bug:** Canvas waveform con dimensiones hardcodeadas 200×32 — ahora usa `devicePixelRatio`
- **Bug:** Eliminar un stem individual borraba todo el grupo de la UI — ahora filtra localmente sin reemplazar resultados completos
- **Tests:** Aserciones actualizadas al nuevo naming (`vocals_viperx` / `instrumental_viperx`). 29/30 pasan, 1 xfail conocido

### v2.0.0-alpha.8 — Backend controls + GPU monitor + Model loader

#### Added (Backend — Go)
- `/api/health`: respuesta estructurada `{backend, gpu, disk, docker}` con `{ok, code, detail}`
- `/api/backend/start|stop|restart` (POST): control del contenedor de inferencia
- `/api/gpu/info` (GET): VRAM, temperatura, utilización, runtime vía nvidia-smi
- `/api/gpu/vram-calculator` (GET): estimación de VRAM por modelo + disponible
- `/api/models/list` (GET): escaneo de modelos locales por categoría
- `/api/models/download` (POST): descarga desde HuggingFace

#### Added (Frontend — Svelte)
- `BackendControls.svelte`: botones Start/Restart/Stop con estado
- `HealthBar.svelte` (rewrite): dots BE/GPU/Disk/Docker con structured health
- `PresetSelector.svelte`: dropdown de presets + botón START
- `ModelConfig.svelte`: configuración por modelo (Vocal/Stems/Drums/Bass/Other)
- `GpuMonitor.svelte`: barra VRAM + temp + utilización (polling 5s)
- `VramCalculator.svelte`: consumo estimado por preset
- `ModelLoader.svelte`: lista de modelos locales + descarga HuggingFace

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
