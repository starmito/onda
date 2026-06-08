# Limpieza de Pipeline y Reparaciones UI

> **Para Hermes:** Usar `subagent-driven-development` para implementar tarea por tarea.

**Goal:** Eliminar componentes obsoletos, unificar presets con persistencia backend, arreglar bugs de UI.

**Arquitectura:** Nuevos endpoints backend para presets + cambios Svelte. Backend almacena presets en `presets_user.json`.

**Tech Stack:** Go (backend), Svelte 5 (rune), CSS nativo, SVG inline.

---

## Tasks

### Task 1: API de presets en backend (persistencia)

**Objective:** Crear endpoints para guardar/cargar/eliminar presets de usuario en el backend. Unificar con los presets predefinidos (turbo, balance, master, ultimate) en una sola respuesta.

**Files:**
- Create: `backend/internal/api/presets.go` — nueva API de presets
- Modify: `backend/internal/api/server.go` — registrar rutas
- Modify: `frontend/src/lib/api.ts` — funciones del frontend para la API

**Step 1: Crear presets.go en backend**

Estructura de datos (comparte el mismo `Preset` de `cli/flags.go`):

```go
package api

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "github.com/starmito/onda/internal/cli"
)

const userPresetsFile = "presets_user.json"

var (
    userPresets     map[string]cli.Preset
    userPresetsMu   sync.RWMutex
)

func init() {
    userPresets = make(map[string]cli.Preset)
    loadUserPresets()
}

func loadUserPresets() {
    data, err := os.ReadFile(userPresetsFile)
    if err != nil {
        return // file doesn't exist yet — that's OK
    }
    var presets map[string]cli.Preset
    if err := json.Unmarshal(data, &presets); err != nil {
        return
    }
    userPresets = presets
}

func saveUserPresets() error {
    userPresetsMu.RLock()
    defer userPresetsMu.RUnlock()
    data, err := json.MarshalIndent(userPresets, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal presets: %w", err)
    }
    if err := os.WriteFile(userPresetsFile, data, 0644); err != nil {
        return fmt.Errorf("failed to write presets: %w", err)
    }
    return nil
}

// getAllPresets returns built-in presets + user presets merged
func getAllPresets() map[string]cli.Preset {
    // Start with built-in presets
    result := make(map[string]cli.Preset, len(cli.Presets)+len(userPresets))
    for k, v := range cli.Presets {
        result[k] = v
    }
    // User presets override built-in with same name
    userPresetsMu.RLock()
    defer userPresetsMu.RUnlock()
    for k, v := range userPresets {
        result[k] = v
    }
    return result
}
```

**Step 2: Añadir handlers en presets.go**

```go
// handleGetPresets devuelve todos los presets (built-in + usuario)
func (s *Server) handleGetPresets(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(getAllPresets())
}

// handleSavePreset guarda o actualiza un preset de usuario
func (s *Server) handleSavePreset(w http.ResponseWriter, r *http.Request) {
    var preset cli.Preset
    if err := json.NewDecoder(r.Body).Decode(&preset); err != nil {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
        return
    }
    if preset.Name == "" {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        json.NewEncoder(w).Encode(map[string]string{"error": "preset name is required"})
        return
    }

    userPresetsMu.Lock()
    userPresets[preset.Name] = preset
    userPresetsMu.Unlock()

    if err := saveUserPresets(); err != nil {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDeletePreset elimina un preset de usuario
func (s *Server) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
    name := r.PathValue("name")
    if name == "" {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        json.NewEncoder(w).Encode(map[string]string{"error": "preset name is required"})
        return
    }

    userPresetsMu.Lock()
    delete(userPresets, name)
    userPresetsMu.Unlock()

    if err := saveUserPresets(); err != nil {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

**Step 3: Registrar rutas en server.go**

En la función `NewServer` o donde se registren las rutas, añadir:

```go
mux.HandleFunc("GET /api/presets", s.handleGetPresets)
mux.HandleFunc("POST /api/presets", s.handleSavePreset)
mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)
```

Es importante que estas rutas se registren ANTES del catch-all de handleModels (que actualmente maneja GET /api/models y puede estar en una ruta conflictiva).

**Step 4: Añadir funciones en api.ts del frontend**

```typescript
export interface PresetData {
  name: string;
  vocalModel: string;
  vocalOverlap: number;
  stemModel: string;
  drumsModel: string;
  bassModel: string;
  otherModel: string;
  pitch: number;
  description: string;
}

export async function getPresets(): Promise<Record<string, PresetData>> {
  const res = await fetch(`${API_BASE}/api/presets`);
  if (!res.ok) throw new Error(`Failed to fetch presets: ${res.status}`);
  return res.json();
}

export async function savePreset(preset: PresetData): Promise<void> {
  const res = await fetch(`${API_BASE}/api/presets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(preset),
  });
  if (!res.ok) throw new Error(`Failed to save preset: ${res.status}`);
}

export async function deletePreset(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/presets/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error(`Failed to delete preset: ${res.status}`);
}
```

**Step 5: Actualizar PipelineEditor para usar API en vez de localStorage**

En `PipelineEditor.svelte`:
- Importar `getPresets`, `savePreset`, `deletePreset` de api.ts
- Reemplazar `loadPresets()` y `savePresetsToStorage()` por llamadas a la API
- Cargar presets al montar el componente (en $effect)
- Al guardar: llamar a `savePreset()` y refrescar lista
- Al eliminar: llamar a `deletePreset()` y refrescar lista
- Añadir indicador de carga/error para la lista de presets

**Estructura del preset que guarda PipelineEditor (mapear a PresetData):**
```typescript
const presetData: PresetData = {
  name,
  vocalModel: viperxModel,
  vocalOverlap: 4,          // valor fijo
  stemModel: demucsModel,
  drumsModel: '',
  bassModel: '',
  otherModel: '',
  pitch: 0,
  description: '',
};
```

**Step 6: Verificar build**
- `cd backend && go build ./cmd/onda/` — debe compilar
- `cd frontend && npm run build` — debe compilar

**Step 7: Añadir presets_user.json a .dockerignore y Dockerfile**

El archivo `presets_user.json` debe persistir entre deploys. Como está dentro del contenedor, hay que montarlo como volumen o copiarlo.

Opción más simple: guardar en `/app/presets_user.json` (el directorio /app ya existe en el contenedor). Añadir al Dockerfile:
```
RUN touch /app/presets_user.json
```

**Step 8: Commit**
```bash
git add backend/internal/api/presets.go backend/internal/api/server.go frontend/src/lib/api.ts
git commit -m "feat: add presets API with backend persistence"
```

---

### Task 2: Eliminar selector de presets de PipelinePanel

**Objective:** Quitar el selector de presets de PipelinePanel. PipelineEditor ahora unifica todos los presets.

**Files:**
- Modify: `frontend/src/lib/PipelinePanel.svelte`
- Modify: `frontend/src/App.svelte`

**Step 1-4:** (mismo contenido que antes pero adaptado — eliminar preset selector, simplificar App.svelte)

---

### Task 3: Eliminar ConfigPanel

**Objective:** (igual que antes)

---

### Task 4: Arreglar zoom en ResultsPanel

**Objective:** (igual que antes)

---

### Task 5: Arreglar SVG PipelineEditor

**Objective:** (igual que antes)

---

### Task 6: CHANGELOG + despliegue

**Objective:** Actualizar CHANGELOG y desplegar en .87

---

## Orden de ejecución

1. **Task 1** → API backend de presets + frontend (cambios en backend + frontend)
2. **Task 2** → Eliminar PipelinePanel presets (depende de Task 1: ahora PipelineEditor unifica)
3. **Task 3** → Eliminar ConfigPanel (independiente)
4. **Task 4** → ResultsPanel zoom (independiente)
5. **Task 5** → SVG PipelineEditor (independiente)
6. **Task 6** → CHANGELOG + despliegue

> **Nota:** La nueva interfaz (punto 5 de tu petición) queda pospuesta para la siguiente fase.

---

### Task 5: Nueva interfaz — plan de alto nivel (pendiente de diseño)

**Objective:** Crear una pantalla de menú de interfaz a la que se accede desde un botón en la zona de botones de configuración (📥 ⚙️). Inspirada en vocalremover.org.

**Nota:** Esto es un rediseño sustancial. Requiere:
- Diseñar una nueva estructura de navegación (sidebar con pestañas)
- Crear un componente `InterfaceMenu.svelte`
- Añadir botón en el header de App.svelte
- Migrar las pantallas actuales (PipelinePanel, PipelineEditor, ModelDownloader, ModelManager, ResultsPanel) al nuevo layout

**Sugerencia:** Posponer esta tarea y hacerla en una fase separada, ya que requiere diseño y es independiente de los bugs anteriores.

---

## Orden de ejecución sugerido

1. **Task 1** → Eliminar presets de PipelinePanel (independiente)
2. **Task 2** → Eliminar ConfigPanel (independiente)
3. **Task 3** → Arreglar zoom en ResultsPanel (independiente)
4. **Task 4** → Arreglar SVG PipelineEditor (independiente)
5. **Task 5** → Nueva interfaz (depende de diseño, posponer)

## Riesgos

- **PipelineEditor sin preset**: Si no se maneja bien, el pipeline podría fallar porque el backend requiere un preset. Verificar que el backend acepte `viperxModel` y `demucsModel` directamente sin preset.
- **Backward compatibility**: Los presets del backend (turbo/balance/master/ultimate) seguirán existiendo en el backend pero ya no se mostrarán en la UI.
