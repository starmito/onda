# Eliminar PipelinePanel + Pitch post-procesamiento con subgrupos

> **Para Hermes:** Usar `subagent-driven-development` para implementar tarea por tarea.

**Goal:** Simplificar la UI eliminando PipelinePanel (redundante con PipelineEditor) y añadir pitch shift post-procesamiento por grupo de stems con subgrupos anidados.

**Arquitectura:** 
- Backend: Nuevo endpoint `POST /api/pitch` que aplica rubberband a stems existentes y guarda resultados en subdirectorio
- Frontend: ResultsPanel con slider de pitch por grupo + subgrupos anidados
- PipelinePanel eliminado, App.svelte simplificado

**Tech Stack:** Go (backend), Svelte 5 (frontend), rubberband CLI (procesamiento audio)

---

## Tasks

### Task 1: Endpoint backend POST /api/pitch

**Objective:** Crear endpoint que reciba `{song, pitch}` y procese los stems de una canción con rubberband, excepto drums. Guarde resultados en `{song}/{song}_pitch{+N}/`.

**Files:**
- Create: `backend/internal/api/pitch.go`
- Modify: `backend/internal/api/server.go` — registrar ruta

**Lógica del endpoint:**
1. Recibir `{song: string, pitch: int}` (POST)
2. Leer los stems de `/output/{song}/` (los archivos .wav)
3. Para cada stem excepto "drums" (y cualquier variante):
   - Ejecutar `audio.RubberbandPitch(pitch, inputPath, outputPath)`
4. Para drums: copiar tal cual
5. Guardar resultados en `/output/{song}/{song}_pitch{+N}/` (ej: `MiCancion_pitch+2/`)
6. Devolver `{song, pitch, files: [{name, path}]}` con los nuevos stems

```go
type PitchRequest struct {
    Song  string `json:"song"`
    Pitch int    `json:"pitch"`
}

type PitchResponse struct {
    Song   string          `json:"song"`
    Pitch  int             `json:"pitch"`
    Files  []FileEntry     `json:"files"`
}

// En server.go, línea ~455 (después de handleSeparate), añadir ruta:
s.mux.HandleFunc("POST /api/pitch", s.handlePitchShift)
```

**Registrar ruta en server.go** en `NewServer()`.

**Verificación:** `cd backend && go vet ./...`

---

### Task 2: Eliminar PipelinePanel de App.svelte

**Objective:** Quitar PipelinePanel del template (ya no se renderiza). Ya no se necesita el import ni sus props.

**Files:**
- Modify: `frontend/src/App.svelte`

**Cambios:**
1. Eliminar `import PipelinePanel from './lib/PipelinePanel.svelte';`
2. Eliminar del template el bloque `<PipelinePanel>` (actual líneas ~585-595)
3. Eliminar el estilo `.pipeline-section` si ya está vacío

**Verificación:** `cd frontend && npm run build`

---

### Task 3: Añadir pitch shift + subgrupos en ResultsPanel

**Objective:** Añadir slider de pitch debajo de cada grupo de stems. Al cambiar, llamar a POST /api/pitch y mostrar resultado como subgrupo anidado.

**Files:**
- Modify: `frontend/src/lib/ResultsPanel.svelte`
- Modify: `frontend/src/lib/api.ts` — añadir `pitchStems()` función

**api.ts** — Añadir:
```typescript
export interface PitchResponse {
  song: string;
  pitch: number;
  files: Array<{ name: string; path: string }>;
}

export async function pitchStems(song: string, pitch: number): Promise<PitchResponse> {
  const res = await fetch(`${API_BASE}/api/pitch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ song, pitch }),
  });
  if (!res.ok) throw new Error(`Pitch failed: ${res.status}`);
  return res.json();
}
```

**ResultsPanel.svelte** — Cambios:

1. **Añadir estado de pitch por grupo**:
```typescript
let pitchValues = $state<Record<string, number>>({}); // song → pitch value
let pitchLoading = $state<Record<string, boolean>>({}); // song → loading
```

2. **Añadir estado para subgrupos** (stems con pitch aplicado):
```typescript
interface PitchedSubgroup {
  pitch: number;
  stems: Array<{ name: string; path: string; stemType: string }>;
}
let pitchedGroups = $state<Record<string, PitchedSubgroup[]>>({}); // song → pitched subgrupos
```

3. **Añadir función handlePitchChange**:
```typescript
async function handlePitchChange(song: string, value: number) {
  pitchValues[song] = value;
  pitchValues = { ...pitchValues }; // trigger reactivity
  
  if (value === 0) {
    // Remove subgrupo for this pitch
    pitchedGroups[song] = (pitchedGroups[song] || []).filter(g => g.pitch !== 0);
    pitchedGroups = { ...pitchedGroups };
    return;
  }
  
  pitchLoading[song] = true;
  pitchLoading = { ...pitchLoading };
  
  try {
    const result = await pitchStems(song, value);
    const stems = result.files.map(f => ({
      name: f.name,
      path: f.path,
      stemType: detectStemType(f.name),
    }));
    
    // Replace or add subgrupo for this pitch value
    const existing = pitchedGroups[song] || [];
    const filtered = existing.filter(g => g.pitch !== value);
    pitchedGroups[song] = [...filtered, { pitch: value, stems }];
    pitchedGroups = { ...pitchedGroups };
  } catch {
    // Show error
  } finally {
    pitchLoading[song] = false;
    pitchLoading = { ...pitchLoading };
  }
}
```

4. **Añadir slider de pitch en el HTML**, después del song-header y antes de stems-list:
```svelte
<!-- Pitch slider -->
<div class="pitch-section">
  <label class="pitch-label">
    Tono: <strong>{pitchValues[group.song] || 0}</strong>
  </label>
  <input
    type="range"
    min="-12"
    max="12"
    step="1"
    value={pitchValues[group.song] || 0}
    oninput={(e) => handlePitchChange(group.song, parseInt((e.target as HTMLInputElement).value))}
    class="pitch-slider"
    disabled={pitchLoading[group.song]}
  />
  {#if pitchLoading[group.song]}
    <span class="pitch-spinner">⏳</span>
  {/if}
</div>
```

5. **Añadir subgrupos después de stems-list**:
```svelte
{#if pitchedGroups[group.song]?.length}
  {#each pitchedGroups[group.song] as pg (pg.pitch)}
    <div class="pitched-group">
      <h4 class="pitched-title">
        {group.song} ({pg.pitch > 0 ? '+' : ''}{pg.pitch})
      </h4>
      {#each pg.stems as stem}
        <div class="stem-row pitched-stem">
          <span class="stem-emoji">{stemEmoji(stem.stemType)}</span>
          <span class="stem-name">{stem.name}</span>
          <a class="stem-btn dl-btn" href={downloadUrl(group.song + '_pitch' + (pg.pitch > 0 ? '+' : '') + pg.pitch, stem.name)} download={stem.name} title="Download">⬇</a>
        </div>
      {/each}
    </div>
  {/each}
{/if}
```

6. **Añadir estilos**:
```css
.pitch-section {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0;
  border-top: 1px solid #2a2a3e;
  margin-top: 0.5rem;
}
.pitch-label {
  font-size: 0.8rem;
  color: #c0c0d0;
  white-space: nowrap;
}
.pitch-slider {
  flex: 1;
  max-width: 200px;
  accent-color: #b388ff;
}
.pitch-spinner {
  font-size: 0.9rem;
}
.pitched-group {
  margin-left: 1.5rem;
  border-left: 2px solid #b388ff44;
  padding-left: 0.75rem;
  margin-top: 0.5rem;
}
.pitched-title {
  margin: 0 0 0.3rem;
  font-size: 0.85rem;
  color: #b388ff;
  font-weight: 600;
}
.pitched-stem {
  opacity: 0.85;
}
```

**Verificación:** `cd frontend && npm run build`

---

### Task 4: CHANGELOG + despliegue

**Objective:** Actualizar changelog y desplegar en .87

**Files:**
- Modify: `CHANGELOG.md`

Añadir entradas:
```
- **PipelinePanel eliminado**: la sección redundante con ViperX/Demucs/Pitch ya no se muestra. PipelineEditor es la única interfaz de configuración.
- **Pitch shift post-procesamiento**: nuevo endpoint `POST /api/pitch`. Slider de tono debajo de cada grupo de stems en ResultsPanel.
- **Subgrupos con pitch**: al aplicar cambio de tono, se genera un subgrupo anidado con los stems procesados (+ drums sin tocar).
```

---

## Orden de ejecución

1. **Task 1** → Endpoint backend POST /api/pitch (back-end, independiente)
2. **Task 2** → Eliminar PipelinePanel de App.svelte (frontend, independiente)
3. **Task 3** → Pitch shift + subgrupos en ResultsPanel (frontend, depende de Task 1)
4. **Task 4** → CHANGELOG + despliegue
