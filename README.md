# 🎵 Onda — Separación de fuentes musicales con IA

Interfaz web para separar voces e instrumentos de cualquier canción usando modelos deep learning (Demucs, ViperX) con aceleración NVIDIA GPU.

![Version](https://img.shields.io/badge/version-v2.8.0-blue)
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
- 🚀 **GPU NVIDIA**: aceleración CUDA para inferencia rápida

## 📋 Requisitos

- **Docker** y **docker compose** v2
- **NVIDIA GPU** con drivers instalados (opcional — fallback CPU automático)
- **NVIDIA Container Toolkit** (`nvidia-docker2`) para GPU
- **Modelos UVR**: descargar desde la interfaz web (aprox 2-4 GB cada uno)

## 🚀 Instalación

```bash
git clone https://github.com/starmito/onda.git
cd onda

# Configurar variables de entorno (opcional)
cp .env.example .env
# Editar .env: MODEL_DIR (ruta a modelos), ONDA_PORT, etc.

# Arrancar
docker compose up -d
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
| **Fase 9** | ROCm + CPU (soporte multi-GPU) | Pendiente |
| **Fase 10** | DAW ligero (waveform, cortes, fades) | Pendiente |
| **Fase 11** | App escritorio (empaquetado) | Pendiente |

## 🏷️ Versionado

Este proyecto sigue versionado semántico (MAJOR.MINOR.PATCH). Etiqueta `-alpha` indica desarrollo activo.

- **v2.6.2-alpha** (actual): Refactor tabs, 4 presets directos, Personalizado, PitchPage, presets bloqueados
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
