# 🎵 Onda — Audio Stem Separation

> Fork de Ultimate Vocal Remover GUI v5.6 con pipeline multi-modelo, pitch shifting, y empaquetado multiplataforma.

**Estado:** Backend funcional · Pipeline modular · GUI en desarrollo

---

## 🚀 Quick Start

```bash
git clone https://github.com/starmito/onda.git
cd onda

# Build (elige NVIDIA o AMD — auto-detecta)
docker compose build

# Separar una canción (pipeline completo)
./onda.sh pipeline cancion.mp3

# Solo quitar la voz
./onda.sh pipeline --viperx --viperx-keep instrumental cancion.mp3

# Solo separar batería y bajo
./onda.sh pipeline --demucs --demucs-keep drums,bass cancion.mp3

# Cambiar tono ±2 semitonos
./onda.sh pipeline --rubberband --pitch 2 cancion.wav
```

---

## 🎛️ Pipeline

```
cancion.mp3
     │
     ├─ 🔪 Viperx ──────────→ vocals + instrumental
     │
     ├─ 🥁 HTDemucs_ft ─────→ drums · bass · other · vocals
     │
     └─ 🎛️ Rubberband ──────→ pitch shift (-drums)
```

Cada paso es independiente. Actívalos con flags:

| Flag | Descripción |
|------|-------------|
| `--viperx` | BS-Roformer-Viperx → vocal + instrumental |
| `--viperx-keep WHAT` | `instrumental` · `vocals` · `both` (default) |
| `--demucs` | HTDemucs_ft → drums, bass, other, vocals |
| `--demucs-keep LIST` | `drums,bass,other,vocals` · `all` (default) |
| `--rubberband` | Pitch shift a todos los stems menos drums |
| `--pitch N` | Semitonos (-12 a +12, default 0) |
| `--output DIR` | Directorio de salida |

**Sin flags** = pipeline completo (viperx + demucs + rubberband).

---

## 🖥️ GPU Support

| GPU | Dockerfile | Imagen | Runtime |
|-----|-----------|--------|---------|
| **NVIDIA** | `Dockerfile` | `onda:nvidia` (~14 GB) | CUDA 12.8 |
| **AMD** | `Dockerfile.amd` | `onda:amd` (~18 GB) | ROCm 6.2 |

Auto-detección vía `lspci`. El wrapper `onda.sh` elige la imagen correcta.

Forzar manualmente:
```bash
GPU_TYPE=amd docker compose build
GPU_TYPE=amd ./onda.sh pipeline cancion.mp3
```

---

## 📦 Estructura

```
onda/
├── Dockerfile              # NVIDIA CUDA 12.8 multi-stage
├── Dockerfile.amd          # AMD ROCm 6.2
├── docker-compose.yml      # Backend + GUI, auto-detecta GPU
├── pipeline.sh             # Pipeline modular (CLI)
├── onda.sh                 # Wrapper con auto-detección GPU
├── requirements-docker.txt # Dependencias Python
├── inference_universal.py  # Inferencia Viperx/RoFormer
├── separate.py             # Librería de separación
├── lib_v5/                 # Modelos UVR v5
├── models/                 # Pesos (.ckpt, .pth) — montados como volumen
├── input/                  # Audio de entrada
├── output/                 # Stems generados
└── gui_data/               # Recursos GUI (fuera del contenedor)
```

---

## 🧪 Test

```bash
# Pipeline completo con tono de 10s
docker exec onda bash /app/pipeline.sh /tmp/test.wav

# Solo instrumental
docker exec onda bash /app/pipeline.sh \
  --viperx --viperx-keep instrumental /tmp/test.wav

# Solo drums + bass
docker exec onda bash /app/pipeline.sh \
  --demucs --demucs-keep drums,bass /tmp/test.wav
```

---

## 🗺️ Roadmap

- [x] Pipeline modular (Viperx → HTDemucs_ft → Rubberband)
- [x] Docker multi-GPU (NVIDIA + AMD)
- [x] CLI wrapper con auto-detección
- [ ] GUI completa (cola, waveform, mute/solo)
- [ ] Empaquetado .deb / AppImage / .msi
- [ ] BSRoformer.cpp (inferencia GGML)

---

## ⚠️ Requisitos

- Docker + nvidia-container-toolkit (NVIDIA) o ROCm (AMD)
- ~16 GB VRAM recomendados (funciona con 8 GB)
- ~25 GB disco (imagen Docker + modelos)
- Modelos descargables aparte (no incluidos en el repo)

---

## 🔗 Créditos

Fork de [Anjok07/ultimatevocalremovergui](https://github.com/Anjok07/ultimatevocalremovergui) v5.6.0 (MIT).
Modelos: BS-Roformer-Viperx (TRvlvr), MelBand RoFormer (pcunwa), HTDemucs (Facebook Research).
