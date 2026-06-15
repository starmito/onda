# 🎵 Onda — Separación de fuentes musicales con IA

Interfaz web para separar voces e instrumentos de cualquier canción usando modelos deep learning (Demucs, ViperX) con aceleración GPU (NVIDIA CUDA / AMD ROCm) o CPU.

![Version](https://img.shields.io/badge/version-v3.0.0-blue)
![License](https://img.shields.io/badge/license-MIT-green)

## ✨ Características

- 🎤 **Separación vocal/instrumental** con ViperX (Roformer)
- 🥁 **Separación multi-stem** con Demucs HT (drums, bass, other, vocals)
- 🎛️ **Pipeline configurable**: elige qué modelos usar y en qué orden
- 📋 **Presets directos**: 4 presets predefinidos (Voces Total, Eliminador de Voz, Separador Completo, Solo Instrumentos) como accesos directos en el sidebar
- 👤 **Presets personalizados**: guarda y carga tus propias configuraciones
- 🔒 **Presets bloqueados**: los 4 presets predefinidos no se pueden eliminar por accidente
- 🎹 **Pitch shift**: cambia el tono de stems generados sin re-procesar
- 📋 **Cola de procesamiento**: arrastra múltiples canciones, procesa en lote
- 📊 **Logs en tiempo real**: eventos del pipeline, logs de servicios, salida de inferencia
- 🎨 **Interfaz personalizable**: 8 colores de acento, tema claro/oscuro, escala 75-150%, tamaño de texto
- 🖥️ **WebUI responsive**: Svelte 5 + backend Go
- 🐳 **Dockerizado**: un solo `docker compose up -d`
- 🚀 **GPU NVIDIA/AMD + CPU**: aceleración CUDA, ROCm o CPU automática según `--build-arg DEVICE`

## 📋 Requisitos

- **Docker** y **docker compose** v2
- **GPU NVIDIA** con drivers + NVIDIA Container Toolkit para CUDA, **GPU AMD** con ROCm para ROCm, o **solo CPU** (sin GPU)
- **Modelos UVR**: descargar desde la interfaz web (aprox 2-4 GB cada uno)

## 🚀 Instalación

```bash
git clone https://github.com/starmito/onda.git
cd onda

# Configurar variables de entorno (opcional)
cp .env.example .env
# Editar .env: MODEL_DIR (ruta a modelos), ONDA_PORT, etc.

# Arrancar (CUDA por defecto)
docker compose up -d

# Para ROCm:
# DEVICE=rocm docker compose --profile rocm up -d

# Para CPU:
# DEVICE=cpu docker compose --profile cpu up -d
```

Abre http://localhost:3000

## 🎯 Uso rápido

1. **Elige un preset** en el sidebar: Voces Total ⭐, Eliminador de Voz 🎤, Separador Completo 〰️, Solo Instrumentos 🎸, o **Personalizado** 👤 para usar tu propia configuración
2. **Arrastra** uno o varios archivos de audio (WAV, MP3, FLAC, OGG, M4A)
3. **Pulsa Ejecutar** — el preset se aplica directamente (sin selector en presets directos)
4. **Descarga** los stems desde Results o aplica **pitch shift**
5. En **Cambiar Tono** 🎵: resultados existentes arriba + dropzone independiente para pitch

## 🏗️ Arquitectura

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│  Navegador  │────▶│   onda-gui   │────▶│     onda     │
│  (Svelte 5) │     │ (Go + Nginx) │     │ (Python+CUDA)│
└─────────────┘     └──────────────┘     └──────────────┘
                           │                      │
                     docker exec             PyTorch
                     pipeline.sh             Demucs/ViperX
                           │                      │
                    ┌──────┴──────┐        ┌──────┴──────┐
                    │   /input/   │        │   /output/  │
                    │  (bind mnt) │        │  (bind mnt) │
                    └─────────────┘        └─────────────┘
```

- **onda**: contenedor de inferencia (Python + PyTorch + CUDA)
- **onda-gui**: servidor único (Go backend + Nginx + Svelte frontend compilado)
- **/input/, /output/**: bind mounts al host, persistentes
- **/config/**: bind mount para presets y configuraciones

## 💻 GPU Compatibility

Onda soporta tres plataformas de inferencia mediante el argumento `--build-arg DEVICE` en el Dockerfile. La detección automática selecciona el device óptimo si no se especifica explícitamente.

### Build-arg DEVICE

| DEVICE  | Plataforma     | Base Image                          | GPU Support                |
|---------|----------------|-------------------------------------|----------------------------|
| `cuda`  | NVIDIA CUDA    | `nvidia/cuda:12.8.0-runtime-ubuntu22.04` | NVIDIA GPU + Container Toolkit |
| `rocm`  | AMD ROCm       | `rocm/dev-ubuntu-22.04:6.4.1-complete`   | AMD GPU + ROCm drivers    |
| `cpu`   | CPU-only       | `ubuntu:22.04`                      | Ninguna (solo CPU)         |

### Cómo construir para cada plataforma

```bash
# CUDA (NVIDIA) — por defecto
docker compose build --build-arg DEVICE=cuda
docker compose up -d

# ROCm (AMD)
DEVICE=rocm docker compose --profile rocm up -d

# CPU-only
DEVICE=cpu docker compose --profile cpu up -d
```

### Detección automática de GPU

El script `detect_gpu.sh` se ejecuta automáticamente al iniciar el contenedor. Detecta:

1. **GPU NVIDIA** mediante `nvidia-smi` → selecciona `cuda`
2. **GPU AMD** mediante `rocminfo` → selecciona `rocm`
3. **Ninguna GPU** → selecciona `cpu`

Si no se pasa `--device` explícitamente a `pipeline.sh`, el auto-detect selecciona el device automáticamente.

### Health endpoint

`GET /api/health` ahora reporta:

```json
{
  "device": "cuda",
  "gpu_type": "cuda",
  "gpu_warning": false
}
```

- `gpu_type`: `cuda` / `rocm` / `cpu`
- `gpu_warning`: `true` si se está ejecutando en CPU (el rendimiento es significativamente menor)

### Frontend

- **Header**: indicador visual del tipo de GPU activo (CUDA 🔵 / ROCm 🔴 / CPU 🟡)
- **CPU warning**: banner de aviso cuando se ejecuta en CPU, informando que el rendimiento puede ser limitado

### docker-compose alternativos

Además del `docker-compose.yml` por defecto (CUDA):

- **ROCm**: usar `--profile rocm` — configura el dispositivo AMD y monta los drivers ROCm
- **CPU**: usar `--profile cpu` — elimina dependencias de GPU, imagen minimalista

### Scripts de validación

`scripts/validate.sh` verifica el entorno completo de Onda:

- Docker y docker compose disponibles
- Estado de GPU (NVIDIA/AMD o CPU)
- Acceso a modelos y directorios
- Configuración de red

## 🧭 Navegación

| Pestaña | Descripción |
|---|---|
| ⭐ Separador Voces Total | 2 stems: voces + instrumental |
| 🎤 Eliminador de Voz | Solo instrumental (elimina voces) |
| 〰️ Separador Completo | 4 stems: drums, bass, other, vocals |
| 🎸 Solo Instrumentos | 4 stems, énfasis instrumental |
| 👤 Personalizado | Preset seleccionable (elige del desplegable) |
| 🎵 Cambiar Tono | Resultados + pitch shift + dropzone independiente |
| 🎵 Detectar velocidad | *Próximamente* |
| 🎛️ DAW | *Próximamente* |
| ❓ Ayuda | Estado de servicios, versión |
| ⚙️ Ajustes | Modelos, Descargas, Presets, Logs, Interfaz |

## 🎨 Personalización

Desde **Ajustes → Interfaz**:
- **Color de acento**: 8 colores (Púrpura, Azul, Verde, Naranja, Rojo, Rosa, Cian, Ámbar)
- **Tema**: Oscuro / Claro (persistente)
- **Tamaño texto**: Pequeño / Mediano / Grande
- **Escala UI**: 75% – 150%

## 🔧 Endpoints principales

| Endpoint | Descripción |
|---|---|
| `GET /api/health` | Estado del sistema (GPU, disco, versiones) |
| `POST /api/upload` | Subir archivo de audio |
| `POST /api/separate` | Lanzar pipeline de separación |
| `GET /api/queue/status` | Estado de la cola de procesamiento |
| `GET /api/results` | Stems generados |
| `POST /api/pitch` | Pitch shift sobre stems |
| `GET /api/logs` | Logs de eventos |
| `GET /api/logs/services` | Logs de servicios (docker + pipeline) |
| `GET /api/presets` | Presets guardados |

## 🗺️ Roadmap

### ✅ Completado (v2.6.2-alpha)
- Rediseño UI completo (sidebar tipo vocalremover.org)
- 4 presets directos + Personalizado
- Presets bloqueados contra eliminación
- PitchPage con dropzone independiente
- Sistema de temas (color acento, claro/oscuro, escala)
- Persistencia de configuración de interfaz
- 21 iconos SVG line-art
- Página de Ayuda con estado de servicios

### 🔜 Próximas fases

| Fase | Descripción | Estado |
|---|---|---|
| **Fase 9** | ROCm + CPU (soporte multi-GPU) | ✅ Completado |
| **Fase 10** | DAW ligero (waveform, cortes, fades) | Pendiente |
| **Fase 11** | App escritorio (empaquetado) | Pendiente |

## 🏷️ Versionado

Este proyecto sigue versionado semántico (MAJOR.MINOR.PATCH). Etiqueta `-alpha` indica desarrollo activo.

- **v3.0.0** (actual): Multi-platform CUDA, ROCm y CPU
- **v2.9.4**: Rubberband-cli restaurado, pipeline.sh auto-detect pasos
- **v2.9.3**: Pitch fix (paths contenedor)
- **v2.6.2-alpha**: Refactor tabs, 4 presets directos, Personalizado, PitchPage, presets bloqueados
- **v2.6.1-alpha**: Pulido UI — colores púrpura, iconos SVG, sidebar vertical, layout fluido
- **v2.6.0-alpha**: Rediseño UI — sidebar vertical colapsable, sistema de temas
- **v2.5.1**: Default preset persistente, selector unificado
- **v2.5.0-alpha**: Preparación UI — modales fullscreen, PresetsPanel

[CHANGELOG completo →](CHANGELOG.md)

## 📁 Estructura del proyecto

```
onda/
├── backend/           # Go backend (API REST + worker)
│   └── internal/api/
├── frontend/          # Svelte 5 frontend
│   └── src/
│       └── lib/       # Componentes (Sidebar, PipelineView, SettingsPanel, etc.)
├── inference/         # Python inference (Demucs, ViperX)
├── onda-gui/          # Dockerfile + entrypoint + nginx.conf
├── Dockerfile         # Contenedor de inferencia (onda)
├── docker-compose.yml # Orquestación
├── pipeline.sh        # Script de pipeline invocado por docker exec
├── VERSION            # Versión centralizada
└── CHANGELOG.md
```

## 📄 Licencia

MIT
