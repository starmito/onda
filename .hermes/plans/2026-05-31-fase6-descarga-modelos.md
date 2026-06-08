# Fase 6: Descarga de modelos + UX — Plan de Implementación

> **Para subagentes:** Usar subagent-driven-development. Un subagente por tarea.

**Objetivo:** Permitir descargar modelos UVR desde HuggingFace, subir modelos locales, y ver el catálogo completo con checks verdes. Medir VRAM real durante inferencia.

**Arquitectura:** Backend: nuevo endpoint de catálogo UVR (datos extraídos del repo oficial) + endpoints de descarga/subida existentes. Frontend: nuevo componente `ModelDownloader.svelte` (panel lateral como ModelManager). La medición de VRAM modifica `inference_universal.py`.

**Tech Stack:** Go 1.26 (backend), Svelte 5 + TypeScript (frontend), Python 3.11 (script UVR)

---

## 🔵 Fase 0 — Investigar UVR (datos)

### Task 0.1: Clonar UVR, extraer catálogo Y método de descarga

**Archivo:** nuevo `scripts/extract_uvr_models.py`

Clonar https://github.com/Anjok07/ultimatevocalremovergui y buscar:
1. **Catálogo de modelos**: nombres, categorías, tamaños, URLs de HuggingFace. Típicamente en `gui_data/constants.py` como `MODELS_DOWNLOAD` o `DOWNLOADABLES`.
2. **Método de descarga**: cómo descarga UVR realmente los modelos. Buscar en el código:
   - `snapshot_download` o `huggingface_hub` → usan la librería huggingface_hub
   - `wget` o `urllib` con URL directa → descarga directa
   - `git lfs pull` → via git

Script que:
1. Clona UVR en /tmp/
2. Busca constantes con: `grep -rn "huggingface\|download.*url\|bs_roformer\|melband\|htdemucs\|snapshot_download\|wget\|MODEL" gui_data/`
3. Extrae el catálogo completo a `uvr_models.json`
4. Documenta el método de descarga en `uvr_download_method.md`:
   - Qué librería usa (huggingface_hub / wget / git-lfs)
   - Cómo construye la URL o path de descarga
   - Ejemplo concreto para un modelo (ej: ViperX)

Genera:
- `uvr_models.json`: catálogo completo
- `uvr_download_method.md`: método de descarga documentado
```json
[
  {
    "name": "BS_Roformer_Viperx",
    "category": "VR_Arch",
    "subcategory": "Roformer",
    "huggingface_repo": "aufr33/BS-Roformer-ViperX-1297",
    "filename": "model_bs_roformer_ep_317_sdr_12.9755.ckpt",
    "size_mb": 609,
    "description": "High quality vocal separation, SDR 12.97"
  }
]
```

### Task 0.2: Crear endpoint de catálogo UVR

**Archivo:** `backend/internal/api/server.go`

Nuevo endpoint `GET /api/models/catalog` que lee `uvr_models.json` (empaquetado en la imagen Docker, copiado a `/app/uvr_models.json`) y lo devuelve. 

Añadir campo `downloaded: true/false` comparando con `listModels()` (si el modelo ya existe en /models/).

```go
type UVRModelEntry struct {
    Name             string `json:"name"`
    DisplayName      string `json:"display_name"`
    Category         string `json:"category"`
    HuggingfaceRepo  string `json:"huggingface_repo,omitempty"`
    Filename         string `json:"filename"`
    SizeMB           int64  `json:"size_mb"`
    Description      string `json:"description,omitempty"`
    Downloaded       bool   `json:"downloaded"`
}
```

### Task 0.3: Empaquetar catálogo en Dockerfile

**Archivo:** `onda-gui/Dockerfile`

Añadir línea: `COPY uvr_models.json /app/uvr_models.json`

---

## 🟢 Fase 1 — Pantalla de descarga (frontend)

### Task 1.1: Nuevo componente ModelDownloader.svelte (estructura)

**Archivo:** `frontend/src/lib/ModelDownloader.svelte`

Panel lateral (como ModelManager) con 3 pestañas:
- **📥 Descargar**: lista por categorías con barra de búsqueda
- **📤 Subir**: dropzone para archivos locales
- **✅ Instalados**: lista de modelos ya descargados

Props: `onclose: () => void`

Estado:
```typescript
let catalog: UVRModelEntry[] = [];
let tab = $state<'download' | 'upload' | 'installed'>('download');
let search = $state('');
let downloading: Set<string> = new Set();
```

### Task 1.2: Carga del catálogo + filtro por búsqueda

```typescript
$effect(() => {
  fetch('/api/models/catalog')
    .then(r => r.json())
    .then(data => catalog = data);
});

let filtered = $derived.by(() => {
  if (!search) return catalog;
  const q = search.toLowerCase();
  return catalog.filter(m => 
    m.name.toLowerCase().includes(q) || 
    m.description?.toLowerCase().includes(q)
  );
});
```

### Task 1.3: Vista "Descargar" — lista por categorías con botón

Agrupar `filtered` por categoría. Para cada modelo:
- Nombre + descripción + tamaño
- Si `downloaded`: check ✅ verde (sin botón)
- Si no: botón "Descargar" que llama a `POST /api/models/download`
- Mientras descarga: spinner + progreso (polling `GET /api/models/download/status?repo=...`)

```svelte
{#each groupedCatalog as group}
  <h3>{group.category}</h3>
  {#each group.models as model}
    <div class="model-row">
      <span>{model.display_name || model.name}</span>
      <span>{model.size_mb} MB</span>
      {#if model.downloaded}
        <span class="check">✅</span>
      {:else if downloading.has(model.name)}
        <span class="spinner">⏳</span>
      {:else}
        <button onclick={() => startDownload(model)}>Descargar</button>
      {/if}
    </div>
  {/each}
{/each}
```

### Task 1.4: Vista "Subir" — dropzone para archivos locales

Reutilizar lógica de DropZone existente en App.svelte. Subir archivos .ckpt/.pth/.onnx a `/api/upload?type=model`.

```svelte
<div class="dropzone" ondragover={...} ondrop={...}>
  Arrastra archivos de modelo aquí (.ckpt, .pth, .onnx)
</div>
<input type="file" accept=".ckpt,.pth,.onnx,.safetensors" multiple />
```

### Task 1.5: Vista "Instalados" — modelos ya descargados

Usar `getLocalModels()` existente. Mostrar lista con nombre, categoría, tamaño. Botón para borrar (llama a `DELETE /api/models/{name}` si existiera, o avisa que se borre manualmente).

### Task 1.6: Botón de acceso en App.svelte

Añadir botón "📥 Modelos" en la barra superior (junto a ⚙️ Modelos). Al pulsar, abre `ModelDownloader`.

```svelte
<button onclick={() => showDownloader = true}>📥 Modelos</button>
{#if showDownloader}
  <ModelDownloader onclose={() => showDownloader = false} />
{/if}
```

### Task 1.7: api.ts — nuevos tipos y funciones

```typescript
export interface UVRModelEntry {
  name: string;
  display_name?: string;
  category: string;
  huggingface_repo?: string;
  filename: string;
  size_mb: number;
  description?: string;
  downloaded: boolean;
}

export async function getModelCatalog(): Promise<UVRModelEntry[]> {
  const res = await fetch(`${API_BASE}/api/models/catalog`);
  return (await res.json()) as UVRModelEntry[];
}
```

---

## 🟡 Fase 2 — Medir pico VRAM real

### Task 2.1: Instrumentar inference_universal.py

**Archivo:** `inference_universal.py` (dentro del contenedor onda)

Añadir al final del script (o en un bloque `finally`):
```python
if torch.cuda.is_available():
    peak_mb = torch.cuda.max_memory_allocated() // (1024 * 1024)
    print(f"VRAM_PEAK_MB={peak_mb}", file=sys.stderr)
```

O mejor: escribir en un archivo JSON junto con los parámetros usados:
```python
vram_stats = {
    "model": model_name,
    "segment_size": args.segment_size,
    "overlap": args.overlap,
    "batch_size": args.batch_size,
    "vram_peak_mb": peak_mb,
    "vram_model_mb": model_size_mb
}
with open(f"{output_dir}/vram_stats.json", "w") as f:
    json.dump(vram_stats, f)
```

### Task 2.2: Modificar pipeline.sh para capturar VRAM

Añadir flag `--vram-stats` a pipeline.sh. Si está presente, buscar `vram_stats.json` en el output y copiarlo a `/output/{song}/vram_stats.json`.

---

## ✅ Verificación

- `GET /api/models/catalog` → devuelve lista UVR con `downloaded: true/false`
- Abrir ModelDownloader → ver pestañas, buscar "viperx" → filtrar
- Botón "Descargar" en un modelo no instalado → spinner → ✅ verde al terminar
- Subir un .ckpt desde local → aparece en "Instalados"
- Ejecutar pipeline con `--vram-stats` → `vram_stats.json` en output con pico real
