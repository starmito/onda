# 🎵 Onda — Separación de fuentes musicales con IA

Interfaz web para separar voces e instrumentos de cualquier canción usando modelos deep learning (Demucs, ViperX) con aceleración GPU (NVIDIA CUDA / AMD ROCm) o CPU.

![Version](https://img.shields.io/badge/version-v3.1.2-blue)
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
- 🚀 **Auto-detección GPU**: detecta CUDA → ROCm → CPU automáticamente en runtime

---

## 📋 Requisitos

- **Docker** y **docker compose** v2
- **GPU NVIDIA**: drivers NVIDIA + [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
- **GPU AMD ROCm**: drivers ROCm instalados en el host, acceso a `/dev/kfd` y `/dev/dri`
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

### ROCm (AMD)

```bash
docker compose -f docker-compose.yml -f docker-compose.rocm.yml up -d --build
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
│  ┌─────────┐   ┌──────────┐   ┌──────┐  │
│  │  Nginx   │──▶│ Go API   │──▶│Python│  │
│  │ (:3000)  │   │ backend  │   │ infer│  │
│  └────┬────┘   └──────────┘   └──┬───┘  │
│       │                          │      │
│  ┌────┴────┐              ┌──────┴───┐  │
│  │ Svelte  │              │ PyTorch  │  │
│  │frontend │              │CUDA/ROCm │  │
│  └─────────┘              └──────────┘  │
│                                          │
└──────────────────────────────────────────┘
         │           │            │
    ┌────┴───┐ ┌────┴───┐  ┌────┴────┐
    │ /input/ │ │ /output│  │ /config/ │
    │  (bind) │ │ (bind) │  │  (bind)  │
    └────────┘ └────────┘  └─────────┘
```

El contenedor unificado incluye:
- **Nginx**: sirve el frontend y hace proxy inverso a la API
- **Go backend**: API REST, cola de procesamiento, gestión de presets
- **Python inference**: Demucs, ViperX, pitch shift
- **Svelte frontend**: interfaz de usuario compilada

Los directorios `/input/`, `/output/` y `/config/` son bind mounts al host para persistencia.

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

## 💻 Despliegue en ROCm

### Requisitos específicos

- GPU AMD compatible con ROCm (ver [lista oficial](https://rocm.docs.amd.com/en/latest/release/gpu_os_support.html))
- Drivers ROCm instalados en el host
- El contenedor necesita acceso a `/dev/kfd` y `/dev/dri`
- Grupo `render` (GID 991) para acceso a GPU

### Pasos

```bash
# Construir y arrancar con soporte ROCm
docker compose -f docker-compose.yml -f docker-compose.rocm.yml up -d --build

# Verificar que la GPU está activa
curl http://localhost:3000/api/health
# → "gpu": { "type": "rocm", ... }
```

### HSA_OVERRIDE_GFX_VERSION (APUs únicamente)

Las AMD APU (Radeon 780M/760M/740M, arquitectura gfx1103) **no están soportadas oficialmente** por ROCm. Para forzar su detección, se necesita la variable de entorno `HSA_OVERRIDE_GFX_VERSION`.

Crea un archivo `.env` en la raíz del proyecto:

```bash
echo 'HSA_OVERRIDE_GFX_VERSION=11.0.2' > .env
```

Valores posibles:

| Valor | Objetivo | Notas |
|-------|----------|-------|
| `11.0.0` | gfx1100 | Funcionalidad básica, kernels limitados |
| `11.0.2` | gfx1102 | Mayor compatibilidad, estable para carga ligera |

### ⚠️ ADVERTENCIA — HSA_OVERRIDE NO ES SOPORTADO OFICIALMENTE

**`HSA_OVERRIDE_GFX_VERSION` es una variable de depuración (DEBUG) no soportada por AMD.**

Obliga a ROCm a cargar kernels de una arquitectura diferente a la real de tu GPU. Esto **no es seguro** y puede causar:

- **GPU Hang** que congela el sistema completo y requiere reinicio forzado
- **Pérdida de datos de VRAM** durante el GPU reset
- **CRTC flip_done timeout** — congelación de la pantalla por caída del controlador de display
- **MES scheduler en estado irrecuperable**
- **Resultados incorrectos** en operaciones edge-case (inferencia silenciosamente errónea)

**NO usar en entornos de producción. Usar bajo tu propio riesgo.**

### amdgpu.dcdebugmask=0x10

Si experimentas congelaciones de pantalla (error `[CRTC:*] flip_done timed out` en los logs del kernel), el parámetro de kernel `amdgpu.dcdebugmask=0x10` desactiva **Panel Self Refresh (PSR)**, una función de ahorro de energía que interfiere con ROCm en APUs.

**⚠️ Tampoco es un parámetro soportado oficialmente.** Uso:

```bash
# Añadir a GRUB
sudo sed -i 's/GRUB_CMDLINE_LINUX_DEFAULT="/GRUB_CMDLINE_LINUX_DEFAULT="amdgpu.dcdebugmask=0x10 /' /etc/default/grub
sudo update-grub
# Reiniciar
```

### Limitaciones conocidas de ROCm en APUs

| Limitación | Descripción |
|------------|-------------|
| **iGPU compartida** | Display y compute compiten por el mismo hardware |
| **Resolución alta** | 4K agrava la inestabilidad (más ancho de banda consumido por display) |
| **Carga sostenida** | Audios >10s pueden causar GPU Hang (~6-7 chunks es el límite típico) |
| **Sin soporte oficial** | AMD no ofrece soporte para iGPUs en workloads AI/ML |
| **Mitigación** | Cerrar apps que usen GPU durante inferencia mejora la estabilidad |

### Recomendaciones para ROCm en APUs

1. Usar `--batch-size 1` y `--dim-t 256` para inferencias
2. Cerrar aplicaciones que consuman GPU (navegador, juegos, etc.)
3. Para cargas largas, dividir el audio en fragmentos de 5-10s
4. Alternativamente, usar el servidor `.87` (NVIDIA CUDA) para cargas pesadas y `.21` para pruebas rápidas

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
| 🎛️ DAW | *Próximamente* |
| ❓ Ayuda | Estado de servicios, versión |
| ⚙️ Ajustes | Modelos, Descargas, Presets, Logs, Interfaz |

---

## 🔧 Endpoints de la API

| Endpoint | Método | Descripción |
|----------|--------|-------------|
| `/api/health` | GET | Estado del sistema (GPU, disco, versiones) |
| `/api/upload` | POST | Subir archivo de audio |
| `/api/separate` | POST | Lanzar pipeline de separación |
| `/api/queue/status` | GET | Estado de la cola de procesamiento |
| `/api/results` | GET | Stems generados |
| `/api/pitch` | POST | Pitch shift sobre stems |
| `/api/logs` | GET | Logs de eventos |
| `/api/logs/services` | GET | Logs de servicios (docker + pipeline) |
| `/api/presets` | GET | Presets guardados |
| `/api/settings/ui` | GET/POST | Configuración de interfaz de usuario |

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
| **v3.1.x** | Fixes ROCm, auto-detección GPU dinámica (HelpPage), single-container unificado, PYTHONPATH fix, GPU type-aware, Removed ResultsPanel |
| **v3.0.0** | Multi-platform: CUDA, ROCm y CPU en un solo contenedor |
| **v2.9.x** | Pitch shift con rubberband, persistencia UI settings, routing de stems por preset |
| **v2.8.x** | Pipeline chaining, presets reales con routing, matriz de stems en PipelineEditor |
| **v2.7.x** | Peak meters RMS en vivo, YAML config con Go puro, cancel real |
| **v2.6.x** | Rediseño UI completo, 4 presets directos, PitchPage, sidebar tipo vocalremover.org |
| **v2.5.x** | Default preset persistente, selector unificado de presets |
| **v2.4.x** | Limpieza de código (-880 líneas), barra de progreso individual por chunk |

### 🔜 Próximas fases

| Fase | Descripción | Estado |
|------|-------------|--------|
| **Fase 10** | DAW ligero: waveform, selección de rango, cortes, fades, exportar, undo/redo | Pendiente |
| **Fase 11** | Empaquetado desktop: Tauri, instaladores .deb/.AppImage/.msi, Flatpak, auto-updater | Pendiente |
| **Fase 12** | Plugin VST3/AU: investigación frameworks (JUCE, DPF, iPlug2), test en DAWs | Pendiente |

---

## 🏷️ Versionado

Versionado semántico (MAJOR.MINOR.PATCH). Prefijo `v` consistente, etiqueta `-alpha` para desarrollo activo.

[CHANGELOG completo →](CHANGELOG.md)

---

## 📁 Estructura del proyecto

```
onda/
├── backend/            # Go backend (API REST + worker)
│   └── internal/api/
├── frontend/           # Svelte 5 frontend
│   └── src/
│       └── lib/        # Componentes (Sidebar, PipelineView, SettingsPanel, etc.)
├── inference/          # Python inference (Demucs, ViperX)
├── scripts/            # Scripts auxiliares (test, deploy, validación)
├── Dockerfile          # Imagen unificada
├── docker-compose.yml  # Orquestación (CPU)
├── docker-compose.cuda.yml    # Override CUDA
├── docker-compose.rocm.yml    # Override ROCm
├── pipeline.sh         # Script de pipeline de separación
├── deploy.sh           # Script de despliegue con auto-detección
├── VERSION             # Versión centralizada
└── CHANGELOG.md        # Historial de cambios
```

---

## 📄 Licencia

MIT
