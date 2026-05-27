# Pantalla de Configuración Avanzada — Plan de Implementación

> **Para Hermes:** Delegar vía subagent-driven-development. 1 subagente para el componente completo.

**Goal:** Reemplazar el collapsible `ModelConfig.svelte` actual por una pantalla completa de configuración con: model loader, lista de modelos interactiva, configurador por modelo con VRAM reactivo.

---

## Arquitectura

Nuevo componente: `ModelConfigScreen.svelte` — pantalla completa que reemplaza al actual `ModelConfig`.

Layout:
```
┌─────────────────────────────────────────────────────┐
│ 📦 Model Configuration                              │
├─────────────────────────────────────────────────────┤
│ 🟢 Back ⬇ Descargar  📂 Cargar                      │
├──────────────────────┬──────────────────────────────┤
│ Model List (left)    │ Model Config (right)          │
│                      │                              │
│ ☐ polarformer        │ Model: polarformer            │
│ ☐ melband_kj     ←──│ Type: RoFormer                │
│ ☐ htdemucs_ft        │ Path: /models/.../model.ckpt │
│ ☐ Kim_Vocal_2        │                              │
│ ☐ ...                │ ⚙️ Parameters:               │
│                      │                              │
│                      │ Segment Size: [256]  ▾       │
│                      │   ↳ Samples per chunk.        │
│                      │   Más = +VRAM, -calidad       │
│                      │                              │
│                      │ Overlap:     [0.50] ▾        │
│                      │   ↳ Solapamiento entre         │
│                      │   chunks. 0.25=menos VRAM     │
│                      │                              │
│                      │ Batch Size:  [1]    ▾        │
│                      │   ↳ Paralelismo. GPU x2 VRAM  │
│                      │                              │
│                      │ ┌────────────────────────┐   │
│                      │ │ 💾 VRAM: ~3,200 / 16,311│   │
│                      │ │ ████████░░░░░░ 19%     │   │
│                      │ │ (con batch=1, seg=256)  │   │
│                      │ └────────────────────────┘   │
│                      │                              │
│                      │ [💾 Save] [🔄 Reset Defaults] │
└──────────────────────┴──────────────────────────────┘
```

---

## Tareas

### 1. Crear `ModelConfigScreen.svelte`

Props:
- `modelInfos: ModelInfo[]` — de `/api/models/list`
- `onclose: () => void` — volver a pantalla principal

Estado interno:
- `selectedModel: ModelInfo | null`
- `config: {segmentSize, overlap, batchSize}` — valores actuales del modelo seleccionado
- `defaults: {...}` — valores por defecto del modelo seleccionado

### 2. Model List (panel izquierdo)

- Lista scrollable de modelos de `/api/models/list`
- Agrupados por categoría (VR, MDX-Net, RoFormer, Demucs)
- Cada fila: nombre + tamaño (MB) + categoría
- Click → selecciona modelo, muestra config a la derecha
- Fila seleccionada resaltada (bg #00d4ff con opacidad baja)

### 3. HuggingFace Downloader (barra superior)

- Input text "Repo ID (ej: StemSplitio/htdemucs-ft-onnx)"
- Botón "⬇ Descargar"
- Estado: downloading / done / error
- Al completar: refrescar modelList

### 4. PC Model Loader (barra superior)

- File input accept=".pth,.onnx,.ckpt,.th,.safetensors"
- Al seleccionar archivo: mostrar nombre + tamaño
- Botón "📂 Subir modelo" → POST /api/upload?type=model

### 5. Model Configurator (panel derecho)

Solo visible cuando `selectedModel !== null`.

Campos de configuración (dropdowns con valores predefinidos):

| Campo | Valores | Default | Descripción |
|---|---|---|---|
| Segment Size | 128, 256, 512, 1024, 2048 | 256 | Samples per chunk. Más = más VRAM, más calidad |
| Overlap | 0.25, 0.50, 0.75 | 0.50 | Chunk overlap. Menos = menos VRAM, posible artefactos |
| Batch Size | 1, 2, 4, 8 | 1 | Parallel batch. ×2 batch = ~×1.8 VRAM |

Cada campo: label + dropdown + breve descripción debajo.

### 6. VRAM Estimator (debajo de los campos)

Barra horizontal:
- VRAM usada estimada / VRAM total
- Color: verde < 50%, amarillo < 80%, rojo ≥ 80%
- Texto: "~3,200 / 16,311 MB (19%)"

Cálculo reactivo:
- `baseVram` = VRAM del modelo (de la tabla hardcodeada)
- `adjustedVram = baseVram * (segmentSize / 256) * overlapFactor * batchFactor`
- `overlapFactor`: 0.25→0.7, 0.50→1.0, 0.75→1.3
- `batchFactor`: 1→1.0, 2→1.8, 4→3.2, 8→5.8

Llama a `/api/gpu/info` para obtener `vram_total_mb`.

### 7. Save / Reset buttons

- **💾 Save**: guarda config en localStorage (`onda_model_config_{modelName}`)
- **🔄 Reset Defaults**: carga los defaults del modelo

### 8. Integración en App.svelte

- Añadir estado `showModelConfig = false`
- El botón "⚙️ Configuración avanzada" (en ModelConfig actual) → `showModelConfig = true`
- Cuando `showModelConfig`: mostrar `<ModelConfigScreen>` en vez del contenido principal
- Botón "🟢 Back" en ModelConfigScreen → `showModelConfig = false`

---

## Svelte 5 patterns
- `$state()` para reactividad
- `$props()` para props
- `$effect()` para side effects (llamar API cuando selectedModel cambia)
- `$derived()` para cálculos reactivos (VRAM)

## Estilo
- Tema oscuro (#0a0a14 bg, #1a1a2e surface, #00d4ff accent)
- Consistente con la UI existente
- Responsive (en móvil: lista arriba, config abajo)

## API endpoints usados
- `GET /api/models/list` — lista de modelos
- `POST /api/models/download` — descargar de HuggingFace
- `POST /api/upload?type=model` — subir modelo
- `GET /api/gpu/info` — VRAM total
