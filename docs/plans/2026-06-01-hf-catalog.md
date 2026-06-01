# Catálogo HF — Plan de Implementación

> **Para Hermes:** Usar subagent-driven-development, delegando cada fase como una unidad.

**Goal:** Añadir catálogo de modelos de Politrees/UVR_resources junto al catálogo UVR nativo, con UI de dos secciones desplegables.

**Architecture:**
- `hf_models.json` — scrape estático del repo HF (YA GENERADO, 380 modelos, 11 categorías)
- `GET /api/models/catalog` → añade `source: "uvr"` a cada entry existente
- `GET /api/models/catalog/hf` → nuevo endpoint que sirve `hf_models.json`
- Frontend: `CatalogPanel.svelte` con dos `<details>` desplegables (UVR Nativa + Repo HF)
- La sección HF muestra categorías como sub-secciones, cada modelo con botón de descarga

**Tech Stack:** Go 1.26 backend, Svelte 5 frontend, Nginx reverse proxy

---

## Fase 1: Backend — nuevo endpoint HF + modificar UVR catalog

### Task 1.1: Añadir handler para GET /api/models/catalog/hf

**Files:**
- Create: `backend/internal/api/catalog_hf.go`
- Modify: `backend/internal/api/server.go` (registrar ruta)

**Step 1: Crear handler**

```go
// backend/internal/api/catalog_hf.go
package api

import (
    "encoding/json"
    "net/http"
    "os"
)

var hfCatalogCache []byte

func loadHFCatalog() ([]byte, error) {
    if hfCatalogCache != nil {
        return hfCatalogCache, nil
    }
    data, err := os.ReadFile("hf_models.json")
    if err != nil {
        return nil, err
    }
    hfCatalogCache = data
    return data, nil
}

func (s *Server) handleModelsCatalogHF(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusMethodNotAllowed)
        json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
        return
    }

    data, err := loadHFCatalog()
    if err != nil {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusNotFound)
        json.NewEncoder(w).Encode(map[string]string{"error": "HF catalog not available"})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write(data)
}
```

**Step 2: Registrar ruta en server.go** — añadir después de la línea 83:

```go
s.mux.HandleFunc("GET /api/models/catalog/hf", s.handleModelsCatalogHF)
```

**Verificación:**
```bash
cd backend && go build ./...
curl http://localhost:3000/api/models/catalog/hf | jq '.categories | keys'
```

### Task 1.2: Añadir `source: "uvr"` al catálogo existente

**Files:** `backend/internal/api/server.go` — handler `handleModelsCatalog`

Añadir `Source string \`json:"source"\`` al struct `UVRModelEntry` (línea 28-38) y setearlo a `"uvr"` en el handler.

```go
// En UVRModelEntry, añadir:
Source string `json:"source"`

// En handleModelsCatalog, al construir cada entry:
entry.Source = "uvr"
```

---

## Fase 2: Frontend — UI del catálogo con 2 secciones desplegables

### Task 2.1: Crear CatalogPanel.svelte

**Files:** `frontend/src/lib/CatalogPanel.svelte`

El componente:
- Dos `<details>` desplegables: "📦 UVR Nativa" y "🤗 Repo HF"
- UVR Nativa → carga `GET /api/models/catalog`, agrupa por `category`, muestra modelos con nombre + size_mb + botón descargar
- Repo HF → carga `GET /api/models/catalog/hf`, muestra `categories` como sub-secciones, cada modelo con nombre + botón descargar
- Cada modelo descargable muestra: nombre, filename, y botón ⬇ que llama a `POST /api/models/download`

```svelte
<script lang="ts">
  import { getModelCatalog, type UVRModelEntry } from './api';

  let uvrModels = $state<UVRModelEntry[]>([]);
  let hfData = $state<any>(null);
  let expandedUVR = $state(false);
  let expandedHF = $state(false);

  $effect(() => {
    getModelCatalog().then(m => uvrModels = m).catch(() => {});
    fetch('/api/models/catalog/hf')
      .then(r => r.json())
      .then(d => hfData = d)
      .catch(() => {});
  });

  function groupByCategory(models: UVRModelEntry[]): Record<string, UVRModelEntry[]> {
    const groups: Record<string, UVRModelEntry[]> = {};
    for (const m of models) {
      const cat = m.category || 'Other';
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(m);
    }
    return groups;
  }
</script>

<div class="catalog-panel">
  <!-- UVR Nativa -->
  <details bind:open={expandedUVR}>
    <summary>📦 UVR Nativa ({uvrModels.length} modelos)</summary>
    {#if expandedUVR}
      {#each Object.entries(groupByCategory(uvrModels)) as [cat, models]}
        <h4>{cat} ({models.length})</h4>
        <div class="model-list">
          {#each models as model}
            <div class="model-row">
              <span class="model-name">{model.display_name || model.name}</span>
              <span class="model-size">{model.size_mb} MB</span>
              {#if model.download_url}
                <button class="dl-btn" onclick={() => downloadModel(model.download_url!)}>⬇</button>
              {/if}
            </div>
          {/each}
        </div>
      {/each}
    {/if}
  </details>

  <!-- Repo HF -->
  <details bind:open={expandedHF}>
    <summary>🤗 Repo HF — Politrees/UVR_resources</summary>
    {#if expandedHF && hfData?.categories}
      {#each Object.entries(hfData.categories) as [cat, info]}
        <h4>{cat} ({info.models.length})</h4>
        <div class="model-list">
          {#each info.models as model}
            <div class="model-row">
              <span class="model-name">{model.name}</span>
              <span class="model-filename">{model.filename}</span>
              <button class="dl-btn">⬇</button>
            </div>
          {/each}
        </div>
      {/each}
    {/if}
  </details>
</div>

<style>
  /* Dark theme matching Onda */
  .catalog-panel { width: 100%; display: flex; flex-direction: column; gap: 0.75rem; }
  details { background: #1a1a2e; border: 1px solid #2a2a4a; border-radius: 8px; padding: 0.75rem 1rem; }
  summary { cursor: pointer; font-weight: 600; color: #e0e0e0; font-size: 1rem; }
  h4 { color: #a0a0c0; margin: 0.5rem 0 0.25rem; font-size: 0.85rem; text-transform: uppercase; }
  .model-list { display: flex; flex-direction: column; gap: 0.25rem; }
  .model-row { display: flex; align-items: center; gap: 0.5rem; padding: 0.25rem 0.5rem; border-radius: 4px; font-size: 0.8rem; }
  .model-row:hover { background: #0a0a14; }
  .model-name { flex: 1; color: #e0e0e0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .model-size { color: #606080; flex-shrink: 0; }
  .model-filename { color: #555; font-size: 0.7rem; }
  .dl-btn { background: #2a2a4a; border: 1px solid #3a3a5a; color: #00d4ff; border-radius: 4px; cursor: pointer; padding: 0.15rem 0.4rem; font-size: 0.7rem; }
  .dl-btn:hover { background: #3a3a5a; }
</style>
```

### Task 2.2: Integrar CatalogPanel en App.svelte

**Files:** `frontend/src/App.svelte`

Añadir import y botón toggle junto a ModelDownloader/ModelManager:

```svelte
import CatalogPanel from './lib/CatalogPanel.svelte';
let showCatalog = $state(false);
```

Añadir botón en el header (junto a 📥 y ⚙️):
```svelte
<button class="btn-gear" onclick={() => (showCatalog = !showCatalog)} title="Catálogo de modelos">📋</button>
```

Añadir al final del template:
```svelte
{#if showCatalog}
  <section class="catalog-section">
    <CatalogPanel />
  </section>
{/if}
```

---

## Fase 3: Despliegue y verificación

### Task 3.1: Build y deploy en .87

```bash
# Local: build frontend
cd frontend && npm run build

# Sync a .87
rsync -avz --delete frontend/dist/ starmito@192.168.1.87:~/projects/onda/frontend/dist/
rsync -avz hf_models.json starmito@192.168.1.87:~/projects/onda/

# Rebuild y restart en .87
ssh starmito@192.168.1.87 'cd ~/projects/onda && docker compose up -d --build onda-gui'
```

### Task 3.2: Verificar endpoints

```bash
# UVR catalog con source
curl -s http://192.168.1.87:3000/api/models/catalog | jq '.[0].source'  # debe ser "uvr"

# HF catalog
curl -s http://192.168.1.87:3000/api/models/catalog/hf | jq '.categories | keys'
```

---

## Notas

- El archivo `hf_models.json` YA EXISTE en el proyecto (380 modelos, 11 categorías)
- Los tamaños del repo HF son 0 (HF API no expone tamaños LFS) — se pueden obtener luego con HEAD requests individuales
- Las categorías `impulse/` (VS8F-1, VS8F-2, VS8F-3) son archivos de audio IR, no modelos de separación — se muestran igual en el catálogo
