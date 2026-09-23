# 🎵 Onda — Separación de fuentes musicales con IA

Interfaz web para separar voces e instrumentos de cualquier canción usando modelos deep learning (Demucs, ViperX, MDX/SCNet/ONNX) con aceleración GPU (NVIDIA CUDA) o CPU.

![Version](https://img.shields.io/badge/version-desde%20tags%20git-blue)
![License](https://img.shields.io/badge/license-MIT-green)

---

## ✨ Características

- 🎤 **Separación vocal/instrumental** con ViperX (BS-Roformer)
- 🥁 **Separación multi-stem** con Demucs HT (drums, bass, other, vocals)
- 🎛️ **Pipeline configurable**: encadena modelos en el orden que quieras
- 📋 **4 presets directos**: Voces Total ⭐, Eliminador de Voz 🎤, Separador Completo 〰️, Solo Instrumentos 🎸
- 👤 **Presets personalizados**: guarda y carga tus propias configuraciones de routing
- 🔒 **Presets bloqueados**: los predefinidos no se pueden eliminar por accidente
- 🔀 **Routing de stems**: elige qué stems guardar, descartar o encadenar al siguiente paso
- 🎹 **Pitch shift**: cambia el tono de stems generados sin reprocesar
- 📊 **Peak meters RMS** en tiempo real durante la reproducción
- 📋 **Cola de procesamiento**: arrastra múltiples canciones, procesa en lote
- 📊 **Logs en tiempo real**: eventos del pipeline, logs de servicios, salida de inferencia
- 🎨 **Interfaz personalizable**: 8 colores de acento, tema claro/oscuro, escala 75-150%
- 🖥️ **WebUI responsive**: Svelte 5 + backend Go
- 🐳 **Un solo contenedor**: Python + Go + Nginx + Svelte en una misma imagen
- 🚀 **Auto-detección GPU**: detecta CUDA → CPU automáticamente en runtime

---

## 📋 Requisitos

- **Docker** y **docker compose** v2
- **GPU NVIDIA**: drivers NVIDIA + [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
- **CPU**: sin requisitos adicionales
- **16 GB RAM** recomendados (8 GB mínimo)
- **Modelos**: descarga automática desde la interfaz web (~2-4 GB)

---

## 🚀 Instalación

```bash
git clone https://github.com/starmito/onda.git
cd onda
```

### CPU (por defecto)

```bash
docker compose up -d --build
```

### CUDA (NVIDIA)

```bash
docker compose -f docker-compose.yml -f docker-compose.cuda.yml up -d --build
```

Abre **http://localhost:3000** en tu navegador.

---

## 🎯 Uso rápido

1. **Elige un preset** en el sidebar: Voces Total, Eliminador de Voz, Separador Completo, Solo Instrumentos o **Personalizado**
2. **Arrastra** uno o varios archivos de audio (WAV, MP3, FLAC, OGG, M4A)
3. **Pulsa Ejecutar** — el preset se aplica automáticamente (sin selector extra en presets directos)
4. **Descarga** los stems desde la página de resultados o aplica **pitch shift**
5. En la pestaña **Cambiar Tono**: resultados existentes arriba + dropzone independiente para subir archivos nuevos

---

## 🏗️ Arquitectura

```
┌──────────────────────────────────────────┐
│          Contenedor onda (single)         │
│                                          │
│  ┌──────────────┐   ┌─────────────────┐  │
│  │ Go API       │──▶│ Python inference│  │
│  │ + frontend   │   │ (Demucs/ViperX/ │  │
│  │   estático   │   │  MDX/SCNet/ONNX)│  │
│  └──────┬───────┘   └────────┬────────┘  │
│         │                    │           │
│  ┌──────┴────────────────────┴───────┐   │
│  │         PyTorch / CUDA / CPU       │   │
│  └────────────────────────────────────┘   │
│                                          │
└──────────────────────────────────────────┘
         │
    ┌────┴────┐
    │/app/data│  input/ output/ daw-data/ config/
    │ (bind)  │  models/ logs/ .cache/
    └─────────┘
```

El contenedor unificado incluye:
- **Go backend**: sirve el frontend estático, expone la API REST, gestiona la cola de procesamiento, presets, modelos y almacenamiento
- **Python inference**: Demucs, ViperX, MDX/SCNet/ONNX, pitch shift
- **Svelte 5 frontend**: interfaz de usuario compilada

Toda la persistencia vive bajo la **raíz de datos única** (`ONDA_DATA_DIR`, por defecto `/app/data` en el contenedor): `input/`, `output/`, `daw-data/`, `input_rubberband/`, `config/`, `logs/`, `models/` y `.cache/`. Las rutas fijas `/app/input/`, `/app/output/` y `/app/config/` son **obsoletas**.

---

## 💻 Despliegue en CUDA

### Requisitos específicos

- GPU NVIDIA con arquitectura Kepler o superior
- [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) instalado
- `nvidia-smi` funcionando en el host

### Pasos

```bash
# Construir y arrancar con soporte CUDA
docker compose -f docker-compose.yml -f docker-compose.cuda.yml up -d --build

# Verificar que la GPU está activa
curl http://localhost:3000/api/health
# → "gpu": { "type": "cuda", ... }
```

---

## 🧭 Navegación

| Pestaña | Descripción |
|---------|-------------|
| ⭐ Separador Voces Total | 2 stems: voces + instrumental |
| 🎤 Eliminador de Voz | Solo instrumental (elimina voces) |
| 〰️ Separador Completo | 4 stems: drums, bass, other, vocals |
| 🎸 Solo Instrumentos | 4 stems, énfasis instrumental |
| 👤 Personalizado | Preset seleccionable (elige del desplegable) |
| 🎵 Cambiar Tono | Resultados + pitch shift + dropzone independiente |
| 🎵 Detectar velocidad | *Próximamente* |
| 🎛️ DAW | Editor con waveform, tempo, effects, piano roll MIDI y mixer |
| ❓ Ayuda | Estado de servicios, versión |
| ⚙️ Ajustes | Modelos, Descargas, Presets, Logs, Interfaz |

---

## 🔧 Endpoints de la API (selección)

| Endpoint | Método | Descripción |
|----------|--------|-------------|
| `/api/health` | GET | Estado del sistema (GPU, disco, versiones) |
| `/api/upload` | POST | Subir archivo de audio |
| `/api/upload/pitch` | POST | Subir archivo para pitch shift |
| `/api/separate` | POST | Lanzar pipeline de separación |
| `/api/queue/status` | GET | Estado de la cola de procesamiento |
| `/api/processes/status` | GET | Cola, proceso activo y métricas de GPU |
| `/api/queue/cancel` | POST | Cancelar el trabajo actual |
| `/api/results` | GET | Stems generados |
| `/api/inputs` | GET | Archivos subidos disponibles |
| `/api/pitch` | POST | Pitch shift sobre stems |
| `/api/pitch/file` | POST | Pitch shift de archivo suelto |
| `/api/audio/tempo` | GET/POST | Detección y cambio de tempo |
| `/api/key` | POST | Detección de tonalidad |
| `/api/stems/merge` | POST | Mezclar stems (mixdown) |
| `/api/export/profiles` | GET/POST | Perfiles de exportación |
| `/api/export/files/{file}` | GET | Descargar exportación |
| `/api/storage/config` | GET/POST | Raíz de datos, carpeta de config y exportación |
| `/api/storage/usage` | GET | Uso de disco por carpeta |
| `/api/models` | GET/POST | Catálogo y gestión de modelos |
| `/api/models/list` | GET | Modelos instalados con peso real |
| `/api/models/download-hf` | POST | Descargar modelo desde HuggingFace |
| `/api/logs` | GET | Logs de eventos |
| `/api/logs/services` | GET | Logs de servicios (docker + pipeline) |
| `/api/presets` | GET/POST | Presets guardados |
| `/api/presets/default` | GET/POST | Preset predeterminado |
| `/api/settings/ui` | GET/POST | Configuración de interfaz de usuario |
| `/api/gpu/info` | GET | Información de GPU y VRAM |
| `/api/gpu/vram-calculator` | GET | Estimador de VRAM por modelo |

Ver `backend/internal/api/server.go` para el listado completo.

---

## ⚙️ Variables de entorno

| Variable | Descripción | Default en contenedor |
|----------|-------------|----------------------|
| `ONDA_DATA_DIR` | Raíz única de datos (`input/`, `output/`, `daw-data/`, `config/`, `logs/`, `models/`, `.cache/`) | `/app/data` |
| `ONDA_SETTINGS_FILE` | Fichero persistente de ajustes (vive fuera de `ONDA_DATA_DIR` para sobrevivir a cambios de raíz) | `/app/data/config/.onda-settings.json` |
| `ONDA_CONFIG_DIR` | Carpeta de configuración (debe estar dentro de `ONDA_DATA_DIR`) | `${ONDA_DATA_DIR}/config` |
| `ONDA_EXPORT_DIR` | Carpeta destino de exportaciones del DAW | vacío (usa ubicación legada) |
| `ONDA_APP_DIR` | Directorio de la aplicación | `/app` |
| `ONDA_PORT` | Puerto expuesto por docker-compose | `3000` |
| `ONDA_ALLOW_UNTAGGED=1` | Permite builds de desarrollo sin tag de release | `0` |
| `HF_HOME` | Caché de HuggingFace Hub | `${ONDA_DATA_DIR}/.cache/huggingface` |
| `TORCH_HOME` | Caché de PyTorch/torch hub | `${ONDA_DATA_DIR}/.cache/torch` |
| `NUMBA_CACHE_DIR` | Caché de Numba | `${ONDA_DATA_DIR}/.cache/numba` |
| `XDG_CACHE_HOME` | Caché XDG | `${ONDA_DATA_DIR}/.cache/xdg` |

Orden de precedencia para raíz, config y exportación: **env > ajuste persistido > defecto**.

---

## 🧠 Flags de modelo

Las flags `shifts`, `segment`, `batch`, `jobs` y `device` se gestionan **solo** en **Ajustes → Modelos**:
- Los presets ya no las mandan.
- La petición de separación no puede sobreescribirlas.
- El rango declarado de cada slider siempre puede representar el valor guardado (p. ej. `shifts` de Demucs usa el rango real del manifiesto).

---

## 🎮 GPU y VRAM

Onda detecta la GPU en runtime y lee la **VRAM real** del dispositivo vía `nvidia-smi`. Si no puede leer la VRAM disponible, el trabajo se bloquea con estado `blocked_no_gpu`. El usuario puede forzar el lanzamiento con `force_vram`, pero el bloqueo por defecto es seguro.

---

## 🎨 Personalización

Desde **Ajustes → Interfaz**:

- **Color de acento**: 8 colores (Púrpura, Azul, Verde, Naranja, Rojo, Rosa, Cian, Ámbar)
- **Tema**: Oscuro / Claro (persistente entre sesiones)
- **Tamaño texto**: Pequeño / Mediano / Grande
- **Escala UI**: 75% – 150%

---

## 🗺️ Roadmap

### ✅ Completado

| Versión | Hitos |
|---------|-------|
| **v3.2.x** | DAW ligero integrado (waveform, BPM, tempo, effects, piano roll MIDI, mixer), seguridad API (CORS, sanitización, path traversal), versionado desde VERSION |
| **v3.1.x** | Fixes ROCm (retirado en v3.5.0), auto-detección GPU dinámica (HelpPage), single-container unificado, PYTHONPATH fix, GPU type-aware, Removed ResultsPanel |
| **v3.0.0** | Multi-platform: CUDA, ROCm (retirado en v3.5.0) y CPU en un solo contenedor |
| **v2.9.x** | Pitch shift con rubberband, persistencia UI settings, routing de stems por preset |
| **v2.8.x** | Pipeline chaining, presets reales con routing, matriz de stems en PipelineEditor |
| **v2.7.x** | Peak meters RMS en vivo, YAML config con Go puro, cancel real |
| **v2.6.x** | Rediseño UI completo, 4 presets directos, PitchPage, sidebar tipo vocalremover.org |
| **v2.5.x** | Default preset persistente, selector unificado de presets |
| **v2.4.x** | Limpieza de código (-880 líneas), barra de progreso individual por chunk |

### 🔜 Próximas fases

| Fase | Versión | Descripción | Estado |
|------|:-------:|-------------|:------:|
| **Fase 11** | v3.x | Empaquetado desktop: Tauri, .deb/.AppImage/.msi, Flatpak, auto-updater | Pendiente |
| **Fase 12** | v3.x | Plugin VST3/AU: investigación frameworks (JUCE, DPF, iPlug2) | Pendiente |

---

## 🏷️ Versionado

Versionado semántico (MAJOR.MINOR.PATCH). Prefijo `v` consistente.

**El fichero `VERSION` es la única fuente de verdad.** Todos los consumidores deben coincidir con él:
- `onda/_version.py` → igual que `VERSION` (con `v`)
- `pyproject.toml`   → igual que `VERSION` **sin** la `v` inicial (PEP 440)
- `frontend/package.json` → igual que `VERSION` (con `v`)

`build.sh` y `deploy.sh` **leen** `VERSION`; nunca la deducen de los tags de git. El tag `onda-vX.Y.Z` es una **etiqueta de release** creada a partir de `VERSION`, no su origen.

### Orden de release

```
1. Actualiza VERSION (y los ficheros derivados)  →  vX.Y.Z
2. Commitea y mergea el cambio de versión
3. Crea y empuja el tag:  git tag -a onda-vX.Y.Z -m "Release vX.Y.Z"
                           git push origin onda-vX.Y.Z
4. Despliega:             bash deploy.sh
```

`deploy.sh` se niega a desplegar si:
- los cuatro ficheros de versión no coinciden con `VERSION`,
- no existe el tag `onda-<VERSION>`, o
- el tag no es ancestro de `HEAD`.

Para builds de desarrollo sin tag, usa `ONDA_ALLOW_UNTAGGED=1`; la imagen se etiquetará como `onda:<VERSION>-dev`.

[CHANGELOG completo →](CHANGELOG.md)

---

## 📁 Estructura del proyecto

```
onda/
├── backend/            # Go backend (API REST + worker)
│   └── internal/api/
├── frontend/           # Svelte 5 + TypeScript 6 frontend
│   └── src/
│       └── lib/        # Componentes (Sidebar, PipelineView, SettingsPanel, etc.)
├── onda/               # Python inference (Demucs, ViperX, MDX/SCNet/ONNX) + CLI
├── tests/              # Tests Python, Go, API, e2e e integración
├── tools/              # Guardianes, trackers y helpers del repo
├── Dockerfile          # Imagen unificada
├── docker-compose.yml  # Orquestación (CPU)
├── docker-compose.cuda.yml    # Override CUDA
├── pipeline.sh         # Script de pipeline de separación
├── build.sh            # Build nativo/Docker; VERSION es la fuente de verdad
├── deploy.sh           # Script de despliegue con auto-detección
├── VERSION             # Versión centralizada
└── CHANGELOG.md        # Historial de cambios
```

---

## 📄 Licencia

MIT
