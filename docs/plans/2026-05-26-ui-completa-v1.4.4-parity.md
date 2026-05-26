# UI Completa — Paridad con v1.4.4 + Features Planeadas

> **Para Hermes:** Usar subagent-driven-development. Backend primero, luego frontend.

**Goal:** Cerrar todos los gaps entre la UI v2.0.0-alpha y la v1.4.4, incluyendo features planeadas (model loader, GPU monitor, VRAM calculator).

**Architecture:** Go backend (nuevos endpoints) → Svelte 5 frontend (nuevos componentes).

---

## FASE A — Backend: Nuevos endpoints Go

### A1. Enhanced Health Check (`/api/health`)
Estructura actual:
```json
{"status":"ok","container":"running","gpu":true,"gpu_info":"...","version":"v2..."}
```
Debe devolver:
```json
{
  "status": "ok",
  "version": "v2.0.0-alpha",
  "backend": {"ok": true, "detail": "onda container running"},
  "gpu": {"ok": true, "code": "OK", "detail": "RTX 5060 Ti, 270/16311 MiB"},
  "disk": {"ok": true, "code": "OK", "detail": "234 GB free on /output"},
  "docker": {"ok": true, "code": "OK", "detail": "docker.sock accessible"}
}
```
Reglas:
- `gpu.ok = true` solo si GPU detectada + runtime nvidia OK + `nvidia-smi` funciona
- `disk.ok = true` si > 10 GB libres en /output
- `docker.ok = true` si `/var/run/docker.sock` existe y responde
- Si algo falla: `ok: false, code: "E1"|"E2"|..."`, `detail: mensaje`

### A2. Backend Control (`/api/backend/start`, `/api/backend/stop`, `/api/backend/restart`)
- `POST /api/backend/start` → `docker start onda`
- `POST /api/backend/stop` → `docker stop onda`
- `POST /api/backend/restart` → `docker restart onda`
- Response: `{"success": true, "detail": "Backend started"}` o `{"success": false, "detail": "..."}`

### A3. GPU Info (`/api/gpu/info`)
```json
{
  "name": "NVIDIA GeForce RTX 5060 Ti",
  "vram_total_mb": 16311,
  "vram_used_mb": 270,
  "vram_free_mb": 16041,
  "utilization_gpu": 2,
  "temperature": 38,
  "runtime": "nvidia",
  "ok": true
}
```
- Ejecuta `nvidia-smi --query-gpu=... --format=csv,noheader` en el host
- Si falla, devuelve `{"ok": false, "error": "..."}`

### A4. VRAM Calculator (`/api/gpu/vram-calculator`)
- Recibe `?models=vocal=melband_kj,stems=htdemucs_ft`
- Devuelve consumo estimado por modelo + total + disponible
```json
{
  "models": [
    {"name": "melband_kj", "vram_mb": 3200},
    {"name": "htdemucs_ft", "vram_mb": 2800}
  ],
  "total_vram_mb": 6000,
  "available_vram_mb": 16311,
  "free_vram_mb": 10311,
  "fits": true
}
```

### A5. Model Management (`/api/models/list`, `/api/models/download`)
- `GET /api/models/list` → lista modelos disponibles en `/mnt/almacen/onda/models/`
- `POST /api/models/download` {source: "huggingface", repo: "StemSplitio/htdemucs-ft-onnx"} → descarga con huggingface_hub

---

## FASE B — Frontend: Nuevos componentes Svelte

### B1. BackendControls.svelte
- Botones ▶ Start, 🔄 Restart, ⏹ Stop
- Estado: "Running" (verde), "Stopped" (rojo), "..." (amarillo)
- Llama a `/api/backend/start|stop|restart`

### B2. HealthBar.svelte (rewrite)
- 4 dots: BE, GPU, Disk, Docker
- Cada dot: verde (ok) / rojo (err) / amarillo (unknown)
- Tooltip con detail al hover
- Polling 15s a `/api/health`
- Usa la nueva estructura `{backend: {ok, code, detail}, ...}`

### B3. PresetSelector.svelte (re-add)
- Dropdown con presets de `/api/models`: turbo, balance, master, ultimate
- Cada preset muestra nombre + descripción + VRAM estimado
- Botón "▶ START" — ejecuta el preset seleccionado

### B4. ModelConfig.svelte
- Desplegable "Configuración avanzada"
- Dropdowns por stem: Vocal Model, Stem Model, Drums Model, Bass Model, Other Model
- Cada dropdown se llena con modelos disponibles de `/api/models/list`
- Overlap slider (2-8) para vocal model

### B5. GpuMonitor.svelte
- Barra de VRAM: usado / total (con gradiente)
- Utilización GPU %
- Temperatura °C
- Runtime (nvidia/runc)
- Polling 5s a `/api/gpu/info`

### B6. VramCalculator.svelte
- Muestra: "Este preset consumirá ~X MB de VRAM"
- "VRAM disponible: Y MB" → "Cabe ✅" o "No cabe ❌"
- Se actualiza al cambiar de preset o al abrir ModelConfig

### B7. ModelLoader.svelte
- Pestaña/pantalla "📦 Models"
- Lista de modelos locales (de `/api/models/list`)
- Botón "📥 Download from HuggingFace" → input para repo ID
- Progreso de descarga
- Botón "📂 Load from PC" → upload de archivo .pth/.onnx

---

## FASE C — Integración en App.svelte

- Layout: header con HealthBar + BackendControls
- Panel Pipeline: PresetSelector + ModelConfig (plegable) + VramCalculator
- Panel GPU: GpuMonitor (widget lateral o inferior)
- Panel Models: ModelLoader (nueva pestaña/sección)
- Mantener: FileQueue, PipelineConfig, PitchControl, ResultsPanel, AudioControls

---

## Orden de ejecución

1. **FASE A** (backend): subagentes A1+A2, A3+A4, A5 (3 tareas en paralelo)
2. Desplegar backend en .87
3. **FASE B** (frontend): subagentes B1+B2, B3+B4+B5, B6+B7 (3 tareas en paralelo)
4. **FASE C** (integración): 1 subagente
5. Verificar todo en .87
