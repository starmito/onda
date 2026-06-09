# 🎵 Onda — Separación de fuentes musicales con IA

Interfaz web para separar voces e instrumentos de cualquier canción usando modelos deep learning (Demucs, ViperX) con aceleración NVIDIA GPU.

![Version](https://img.shields.io/badge/version-v2.3.8-blue)
![License](https://img.shields.io/badge/license-MIT-green)

## ✨ Características

- 🎤 **Separación vocal/instrumental** con ViperX (Roformer)
- 🥁 **Separación multi-stem** con Demucs HT (drums, bass, other, vocals)
- 🎛️ **Pipeline configurable**: elige qué modelos usar y en qué orden
- 💾 **Presets**: guarda y carga configuraciones del pipeline
- 🎹 **Pitch shift**: cambia el tono de stems generados sin re-procesar
- 📋 **Cola de procesamiento**: arrastra múltiples canciones, procesa en lote
- 📊 **Logs en tiempo real**: eventos del pipeline, logs de servicios, salida de inferencia
- 🖥️ **WebUI responsive**: Svelte 5 + backend Go
- 🐳 **Dockerizado**: un solo `docker compose up -d`
- 🚀 **GPU NVIDIA**: aceleración CUDA para inferencia rápida

## 📋 Requisitos

- **Docker** y **docker compose** v2
- **NVIDIA GPU** con drivers instalados
- **NVIDIA Container Toolkit** (`nvidia-docker2`)
- **Modelos UVR**: descargar desde la interfaz web (aprox 2-4 GB cada uno)

## 🚀 Instalación

```bash
git clone https://github.com/starmito/onda.git
cd onda

# Configurar variables de entorno (opcional)
cp .env.example .env
# Editar .env: MODEL_DIR (ruta a modelos), ONDA_PORT, etc.

# Descargar modelos desde la WebUI (http://localhost:3000 → Modelos)
# o colocar manualmente en ./models/: Demucs, ViperX, Roformer...

# Arrancar
docker compose up -d
```

Abre http://localhost:3000

## 🎯 Uso rápido

1. **Arrastra** uno o varios archivos de audio (WAV, MP3, FLAC, OGG, M4A)
2. **Configura el pipeline**: elige ViperX (vocal/inst) y/o Demucs (4 stems)
3. **(Opcional) Guarda un preset** con tu configuración
4. **Marca** las canciones a procesar y pulsa **Ejecutar**
5. **Descarga** los stems desde Results o aplica **pitch shift**

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

## 🏷️ Versionado

Este proyecto sigue versionado semántico (MAJOR.MINOR.PATCH).

- **v2.3.8** (actual): timestamps reales en logs, filtro de logs funcional
- **v2.3.7**: timestamps distintos en docker logs
- **v2.3.6**: reactividad upload, separación Eventos/Servicios, filtro logs
- **v2.3.5**: upload on drag, pipeline logs en ring buffer
- **v2.3.4**: presets con pipeline completo, logs de servicios docker

[CHANGELOG completo →](CHANGELOG.md)

## 📁 Estructura del proyecto

```
onda/
├── backend/           # Go backend (API REST + worker)
│   └── internal/api/
├── frontend/          # Svelte 5 frontend
│   └── src/
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
