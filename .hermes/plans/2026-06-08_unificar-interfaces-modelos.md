# Unificar Interfaces de Modelos — Plan de Implementación

> **Para Hermes:** Usar `subagent-driven-development` para implementar tarea por tarea.

**Goal:** Unificar CatalogPanel en ModelDownloader, convertir ambos paneles (ModelDownloader + ModelManager) a pantalla completa con botón "Volver", y eliminar CatalogPanel.

**Arquitectura:** Tres componentes Svelte (ModelDownloader, ModelManager, CatalogPanel) + App.svelte como orquestador. ModelDownloader absorbe la sección HF de CatalogPanel y se rediseña a pantalla completa. ModelManager se rediseña a pantalla completa. CatalogPanel se elimina.

**Tech Stack:** Svelte 5 (runes: $state, $effect, $derived, $props), TypeScript, CSS nativo, fetch API REST.

---

## Tasks

### Task 1: Extender API de descarga para soportar filename

**Objective:** Que `downloadModel()` acepte un `filename` opcional para descargar archivos específicos dentro de un repo (necesario para los modelos HF de Politrees/UVR_resources).

**Files:**
- Modify: `frontend/src/lib/api.ts`

**Step 1: Actualizar interface y función**

Añadir `filename?` a `DownloadModelRequest` y actualizar `downloadModel()`:

```typescript
export interface DownloadModelRequest {
  source: 'huggingface';
  repo: string;
  filename?: string;  // NEW: optional specific file
}

export async function downloadModel(repo: string, filename?: string): Promise<DownloadModelResponse> {
  const body: DownloadModelRequest = { source: 'huggingface', repo };
  if (filename) body.filename = filename;
  const res = await fetch(`${API_BASE}/api/models/download`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`Model download failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as DownloadModelResponse;
}
```

**Step 2: Añadir función getHfCatalog()**

```typescript
export interface HFModelEntry {
  name: string;
  filename: string;
  hf_path: string;
  size_mb: number;
  category: string;
}

export interface HfCatalogResponse {
  categories: Record<string, { models: HFModelEntry[] }>;
}

export async function getHfCatalog(): Promise<HfCatalogResponse> {
  const res = await fetch(`${API_BASE}/api/models/catalog/hf`);
  if (!res.ok) {
    throw new Error(`HF catalog fetch failed with status ${res.status}: ${res.statusText}`);
  }
  return (await res.json()) as HfCatalogResponse;
}
```

**Step 3: Verificar compilación**

Run: `cd frontend && npm run build` (o `npx svelte-check`)
Expected: No type errors.

**Step 4: Commit**
```bash
git add frontend/src/lib/api.ts
git commit -m "feat(api): add filename param to downloadModel and getHfCatalog"
```

---

### Task 2: Rediseñar ModelDownloader a pantalla completa

**Objective:** Convertir ModelDownloader de panel lateral deslizante a pantalla completa con botón "← Volver" arriba a la izquierda. Preparar estructura para integrar HF catalog.

**Files:**
- Modify: `frontend/src/lib/ModelDownloader.svelte`

**Step 1: Cambiar estructura HTML**

Reemplazar el `div.backdrop` + `div.panel` (slide-in lateral) por una pantalla completa:

```svelte
{#if loading}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <button class="btn-back" onclick={onclose}>← Volver</button>
      <h2>📥 Gestor de Modelos</h2>
      <div><!-- spacer --></div>
    </div>
    <div class="fullscreen-body loading-text">Cargando...</div>
  </div>
{:else}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <button class="btn-back" onclick={onclose}>← Volver</button>
      <h2>📥 Gestor de Modelos</h2>
      <div><!-- spacer --></div>
    </div>
    <div class="fullscreen-body">
      <!-- Tab bar -->
      <div class="tab-bar">...</div>
      <!-- Search + source filters (placeholder for now) -->
      <div class="search-wrap">...</div>
      <!-- Rest of content stays the same -->
    </div>
  </div>
{/if}
```

**Step 2: Reemplazar estilos**

Eliminar todo el bloque `.backdrop` / `.panel` / `@keyframes slideIn`.

Añadir:

```css
.fullscreen {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: #0a0a14;
  z-index: 900;
  display: flex;
  flex-direction: column;
  animation: fadeIn 0.2s ease;
}

.fullscreen-header {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 0.75rem 1.25rem;
  border-bottom: 1px solid #2a2a4a;
  background: #1a1a2e;
}

.fullscreen-header h2 {
  margin: 0;
  font-size: 1.1rem;
  color: #e0e0e0;
  flex: 1;
  text-align: center;
}

.btn-back {
  background: none;
  border: 1px solid #2a2a4a;
  border-radius: 6px;
  color: #00d4ff;
  font-size: 0.85rem;
  padding: 0.3rem 0.8rem;
  cursor: pointer;
  transition: border-color 0.15s;
}
.btn-back:hover {
  border-color: #00d4ff;
}

.fullscreen-body {
  flex: 1;
  overflow-y: auto;
  padding: 1.25rem;
  max-width: 800px;
  margin: 0 auto;
  width: 100%;
  box-sizing: border-box;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}
```

**Paso 3: Ajustar tab-bar (centrado)**

```css
.tab-bar {
  display: flex;
  justify-content: center;
  gap: 0.5rem;
  margin-bottom: 1rem;
}
```

**Step 4: Verificar compilación**

Run: `cd frontend && npm run build`
Expected: Build OK

**Step 5: Commit**
```bash
git add frontend/src/lib/ModelDownloader.svelte
git commit -m "refactor(ModelDownloader): convert to fullscreen layout with back button"
```

---

### Task 3: Rediseñar ModelManager a pantalla completa

**Objective:** Convertir ModelManager de panel lateral deslizante a pantalla completa con botón "← Volver" arriba a la izquierda.

**Files:**
- Modify: `frontend/src/lib/ModelManager.svelte`

**Step 1: Cambiar estructura HTML**

Igual que en Task 2: reemplazar `div.backdrop` + `div.panel` por `.fullscreen`.

```svelte
{#if loading}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <button class="btn-back" onclick={onclose}>← Volver</button>
      <h2>⚙️ Configuración de Modelos</h2>
      <div><!-- spacer --></div>
    </div>
    <div class="fullscreen-body loading-text">Cargando...</div>
  </div>
{:else}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <button class="btn-back" onclick={onclose}>← Volver</button>
      <h2>⚙️ {selectedModelDisplayName || 'Configuración de Modelos'}</h2>
      <div><!-- spacer --></div>
    </div>
    <div class="fullscreen-body">
      <!-- Same content as before -->
    </div>
  </div>
{/if}
```

**Step 2: Reemplazar estilos**

Eliminar `.backdrop` / `.panel` / `@keyframes slideIn`.

Añadir mismos estilos que en Task 2 para `.fullscreen`, `.fullscreen-header`, `.btn-back`, `.fullscreen-body`, `@keyframes fadeIn`.

**Step 3: Ajustar ancho del panel-body (más ancho en fullscreen)**

```css
.fullscreen-body {
  flex: 1;
  overflow-y: auto;
  padding: 1.5rem;
  max-width: 600px;
  margin: 0 auto;
  width: 100%;
  box-sizing: border-box;
}
```

(600px porque los sliders son más compactos, pero necesita espacio para los labels)

**Step 4: Verificar compilación**

Run: `cd frontend && npm run build`
Expected: Build OK

**Step 5: Commit**
```bash
git add frontend/src/lib/ModelManager.svelte
git commit -m "refactor(ModelManager): convert to fullscreen layout with back button"
```

---

### Task 4: Integrar catálogo HF y filtros de fuente en ModelDownloader

**Objective:** Añadir los modelos de HuggingFace (Politrees/UVR_resources) a la pestaña Download de ModelDownloader, con 3 botones de filtro de fuente y etiqueta de fuente en cada modelo.

**Files:**
- Modify: `frontend/src/lib/ModelDownloader.svelte`

**Step 1: Añadir imports + nuevas variables de estado**

```typescript
import { getModelCatalog, getHfCatalog, downloadModel, uploadModel, deleteModel, getLocalModels, type UVRModelEntry, type HFModelEntry, type LocalModel } from './api';

// ---- Source filter state ----
type SourceFilter = 'all' | 'uvr' | 'hf';
let sourceFilter = $state<SourceFilter>('all');

// ---- HF catalog state ----
let hfCatalog = $state<HFModelEntry[]>([]);
let hfCatalogLoading = $state(false);
let hfCatalogError = $state(false);
```

**Step 2: Cargar HF catalog en $effect**

```typescript
$effect(() => {
  // Load HF catalog
  hfCatalogLoading = true;
  getHfCatalog()
    .then(data => {
      const all: HFModelEntry[] = [];
      for (const [cat, info] of Object.entries(data.categories)) {
        for (const m of info.models) {
          if (m.size_mb > 0) { // filter out YAML-only entries
            all.push({ ...m, category: cat });
          }
        }
      }
      hfCatalog = all;
      hfCatalogLoading = false;
    })
    .catch(() => {
      hfCatalogError = true;
      hfCatalogLoading = false;
    });
});
```

**Step 3: Añadir tipo combinado y combinación de fuentes**

```typescript
type SourceType = 'uvr' | 'hf';

interface CombinedModel {
  name: string;
  display_name?: string;
  category: string;
  size_mb: number;
  description?: string;
  downloaded: boolean;
  source: SourceType;
  // UVR-specific
  huggingface_repo?: string;
  filename?: string;
  // HF-specific
  hf_path?: string;
}

let combinedModels = $derived.by(() => {
  const uvrMapped: CombinedModel[] = (sourceFilter === 'hf' ? [] : catalog).map(m => ({
    name: m.name,
    display_name: m.display_name,
    category: m.category,
    size_mb: m.size_mb,
    description: m.description,
    downloaded: m.downloaded,
    source: 'uvr' as SourceType,
    huggingface_repo: m.huggingface_repo,
    filename: m.filename,
  }));

  const hfMapped: CombinedModel[] = (sourceFilter === 'uvr' ? [] : hfCatalog).map(m => ({
    name: m.name,
    category: m.category,
    size_mb: m.size_mb,
    downloaded: false, // HF models always start as not downloaded
    source: 'hf' as SourceType,
    hf_path: m.hf_path,
    filename: m.filename,
  }));

  return [...uvrMapped, ...hfMapped];
});
```

**Step 4: Actualizar filtrado para usar combinedModels**

El `filtered` derivado ya filtra por texto de búsqueda. Ahora filtra sobre `combinedModels`:

```typescript
let filtered = $derived.by(() => {
  if (!search) return combinedModels;
  const q = search.toLowerCase();
  return combinedModels.filter(
    (m) =>
      m.name.toLowerCase().includes(q) ||
      m.display_name?.toLowerCase().includes(q) ||
      m.description?.toLowerCase().includes(q),
  );
});
```

**Step 5: Añadir botones de filtro de fuente en HTML**

Debajo del search input y antes de la lista de catálogo:

```svelte
<div class="source-filters">
  <button
    class="source-btn"
    class:active={sourceFilter === 'all'}
    onclick={() => (sourceFilter = 'all')}
  >Todas las fuentes</button>
  <button
    class="source-btn"
    class:active={sourceFilter === 'uvr'}
    onclick={() => (sourceFilter = 'uvr')}
  >UVR</button>
  <button
    class="source-btn"
    class:active={sourceFilter === 'hf'}
    onclick={() => (sourceFilter = 'hf')}
  >Hugging Face</button>
</div>
```

**Step 6: Añadir etiqueta de fuente a cada modelo**

En la fila del modelo, al lado del botón de descargar:

```svelte
<span class="source-badge" class:uvr={model.source === 'uvr'} class:hf={model.source === 'hf'}>
  {model.source === 'uvr' ? 'UVR' : 'HF'}
</span>
```

**Step 7: Actualizar startDownload para HF models + descarga .yaml**

```typescript
async function startDownload(model: CombinedModel) {
  const set = new Set(downloading);
  set.add(model.filename || model.name);
  downloading = set;
  downloadErrors.delete(model.filename || model.name);
  downloadErrors = new Map(downloadErrors);

  try {
    if (model.source === 'uvr') {
      await downloadModel(model.huggingface_repo!);
    } else {
      await downloadModel('Politrees/UVR_resources', model.hf_path);
      // If checkpoint, also download .yaml
      if (model.filename?.match(/\.(ckpt|pth)$/i)) {
        const baseName = model.filename!.slice(0, model.filename!.lastIndexOf('.'));
        const yamlEntry = hfCatalog.find(m => m.filename === baseName + '.yaml');
        if (yamlEntry) {
          await downloadModel('Politrees/UVR_resources', yamlEntry.hf_path);
        }
      }
    }
    await refreshCatalog();
  } catch (err: any) {
    const errors = new Map(downloadErrors);
    errors.set(model.filename || model.name, err.message || 'Download failed');
    downloadErrors = errors;
  } finally {
    const set2 = new Set(downloading);
    set2.delete(model.filename || model.name);
    downloading = set2;
  }
}
```

**Step 8: Añadir estilos para filtros y badges**

```css
.source-filters {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 1rem;
  justify-content: center;
}

.source-btn {
  padding: 0.4rem 1rem;
  background: #1a1a2e;
  border: 1px solid #2a2a4a;
  border-radius: 20px;
  color: #888;
  font-size: 0.8rem;
  cursor: pointer;
  transition: all 0.15s;
}
.source-btn:hover {
  border-color: #555;
  color: #c0c0d0;
}
.source-btn.active {
  background: #00d4ff22;
  border-color: #00d4ff;
  color: #00d4ff;
}

.source-badge {
  font-size: 0.65rem;
  padding: 0.15rem 0.4rem;
  border-radius: 4px;
  font-weight: 600;
  flex-shrink: 0;
}
.source-badge.uvr {
  background: #1b3a2a;
  color: #81c784;
}
.source-badge.hf {
  background: #1b2a3a;
  color: #64b5f6;
}

.model-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  border-radius: 8px;
  transition: background 0.15s;
}
.model-row:hover {
  background: #1a1a2e;
}
```

**Step 9: Verificar compilación**

Run: `cd frontend && npm run build`
Expected: Build OK

**Step 10: Commit**
```bash
git add frontend/src/lib/ModelDownloader.svelte
git commit -m "feat(ModelDownloader): integrate HF catalog with source filters and badges"
```

---

### Task 5: Actualizar App.svelte — eliminar CatalogPanel, ajustar navegación

**Objective:** Eliminar la importación, estado, botón y renderizado de CatalogPanel. Ajustar los estilos de los botones de navegación ahora que solo hay 2.

**Files:**
- Modify: `frontend/src/App.svelte`

**Step 1: Eliminar import de CatalogPanel**

Eliminar línea: `import CatalogPanel from './lib/CatalogPanel.svelte';`

**Step 2: Eliminar estado showCatalog**

Eliminar línea: `let showCatalog = $state(false);`

**Step 3: Eliminar botón 📋 del header**

Eliminar:
```svelte
<button
  class="btn-gear"
  onclick={() => (showCatalog = !showCatalog)}
  title="Catálogo de modelos"
>📋</button>
```

**Step 4: Eliminar renderizado de CatalogPanel**

Eliminar:
```svelte
{#if showCatalog}
  <section class="catalog-section">
    <CatalogPanel />
  </section>
{/if}
```

**Step 5: Eliminar estilo catalog-section**

Eliminar:
```css
.catalog-section {
  width: 100%;
}
```

**Step 6: Verificar compilación**

Run: `cd frontend && npm run build`
Expected: Build OK, sin errores de import no usado.

**Step 7: Commit**
```bash
git add frontend/src/App.svelte
git commit -m "refactor(App): remove CatalogPanel, keep downloader and model manager buttons"
```

---

### Task 6: Eliminar CatalogPanel.svelte

**Objective:** Borrar el archivo del componente que ya no se usa.

**Files:**
- Delete: `frontend/src/lib/CatalogPanel.svelte`

**Step 1: Verificar que no hay referencias**

```bash
grep -r "CatalogPanel" frontend/src/
```
Expected: Solo resultados en git log, ningún import actual.

**Step 2: Eliminar archivo**

```bash
git rm frontend/src/lib/CatalogPanel.svelte
```

**Step 3: Verificar compilación**

`cd frontend && npm run build`
Expected: Build OK

**Step 4: Commit**
```bash
git commit -m "chore: remove CatalogPanel.svelte (folded into ModelDownloader)"
```

---

### Task 7: Verificación final y push

**Objective:** Build completo, smoke test visual, actualizar CHANGELOG.

**Files:**
- Modify: `CHANGELOG.md`

**Step 1: Build completo y verificación**

```bash
cd frontend
npm run build
```

**Step 2: CHANGELOG**

Añadir entrada:

```markdown
## [2.2.0] - 2026-06-08

### Changed
- ModelDownloader and ModelManager converted to fullscreen panels with back button
- HF catalog (Politrees/UVR_resources) integrated into ModelDownloader Download tab
- Added source filter buttons (Todas/UVR/HF) in ModelDownloader
- Added source badge (UVR/HF) to each model row

### Removed
- CatalogPanel component (functionality merged into ModelDownloader)
- Third catalog button from main navigation

### Added
- `getHfCatalog()` API function for HF model catalog
- `filename` parameter support in `downloadModel()` API function
```

**Step 3: Push**

```bash
git add CHANGELOG.md
git commit -m "chore: update CHANGELOG for v2.2.0"
git push
```

---
