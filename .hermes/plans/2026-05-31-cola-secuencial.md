# Cola Secuencial + Resultados Acumulados — Plan de Implementación

> **Para subagentes:** Usar subagent-driven-development. Un subagente por tarea.

**Objetivo:** Reemplazar la ejecución paralela de pipelines por una cola secuencial (FIFO) con resultados acumulados en el frontend.

**Arquitectura:** Cola en memoria (Go channel) con worker único. Un endpoint para añadir trabajos, otro para consultar estado. Frontend muestra cola visible (waiting → processing → done/error) y acumula resultados por canción sin reemplazar.

---

## 🔵 Backend — server.go

### Task 1: Estructura de cola y worker

**Archivo:** `backend/internal/api/server.go`

Añadir a la struct `Server`:
```go
type Server struct {
    mux       *http.ServeMux
    jobQueue  chan JobRequest       // cola FIFO
    jobs      map[string]*JobState  // estado de todos los trabajos (key=song)
    jobsMu    sync.RWMutex           // protege jobs map
    workerRunning bool               // si el worker está activo
}

type JobRequest struct {
    Song   string              // nombre de la canción
    Args   []string            // argumentos para pipeline.sh
    Config SeparateRequest     // request original
}

type JobState struct {
    Song     string `json:"song"`
    Status   string `json:"status"`   // waiting, processing, done, error
    Progress int    `json:"progress"` // 0-100 (solo para processing)
    Error    string `json:"error,omitempty"`
    Files    []FileEntry `json:"files,omitempty"`
}
```

Inicializar en `NewServer()`:
```go
s := &Server{
    mux:      http.NewServeMux(),
    jobQueue: make(chan JobRequest, 20), // buffer para 20 canciones
    jobs:     make(map[string]*JobState),
}
go s.worker() // lanzar worker al iniciar servidor
```

### Task 2: Worker secuencial

```go
func (s *Server) worker() {
    for job := range s.jobQueue {
        s.jobsMu.Lock()
        s.jobs[job.Song].Status = "processing"
        s.jobsMu.Unlock()

        // Ejecutar pipeline
        dockerArgs := append([]string{"exec", "onda", "bash", "/pipeline.sh"}, job.Args...)
        cmd := exec.Command("docker", dockerArgs...)
        out, err := cmd.CombinedOutput()

        s.jobsMu.Lock()
        if err != nil {
            s.jobs[job.Song].Status = "error"
            s.jobs[job.Song].Error = string(out)
        } else {
            s.jobs[job.Song].Status = "done"
            // Leer stems generados
            s.jobs[job.Song].Files = listStems(job.Song)
        }
        s.jobsMu.Unlock()
    }
}
```

### Task 3: Endpoint POST /api/queue (añadir trabajo)

```go
func (s *Server) handleQueueAdd(w http.ResponseWriter, r *http.Request) {
    var req SeparateRequest
    json.NewDecoder(r.Body).Decode(&req)
    song := filepath.Base(req.Input)

    s.jobsMu.Lock()
    if _, exists := s.jobs[song]; exists {
        // Ya está en cola
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"error": "song already queued"})
        s.jobsMu.Unlock()
        return
    }
    s.jobs[song] = &JobState{Song: song, Status: "waiting"}
    s.jobsMu.Unlock()

    s.jobQueue <- JobRequest{Song: song, Args: buildPipelineArgs(req), Config: req}

    w.WriteHeader(http.StatusAccepted)
    json.NewEncoder(w).Encode(map[string]string{"status": "queued", "song": song})
}
```

### Task 4: Endpoint GET /api/queue/status (consultar cola)

```go
func (s *Server) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
    s.jobsMu.RLock()
    defer s.jobsMu.RUnlock()

    var jobs []*JobState
    for _, j := range s.jobs {
        jobs = append(jobs, j)
    }
    // Ordenar: processing primero, luego waiting, luego done, luego error
    sort.Slice(jobs, func(i, j int) bool {
        order := map[string]int{"processing": 0, "waiting": 1, "done": 2, "error": 3}
        return order[jobs[i].Status] < order[jobs[j].Status]
    })

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobs})
}
```

### Task 5: Migrar handleSeparate → usar cola

Modificar `handleSeparate` para que use la cola en vez de lanzar goroutine:
```go
func (s *Server) handleSeparate(w http.ResponseWriter, r *http.Request) {
    var req SeparateRequest
    json.NewDecoder(r.Body).Decode(&req)
    // ... validación ...

    song := filepath.Base(req.Input)
    s.jobsMu.Lock()
    if _, exists := s.jobs[song]; exists {
        s.jobsMu.Unlock()
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"error": "song already queued"})
        return
    }
    s.jobs[song] = &JobState{Song: song, Status: "waiting"}
    s.jobsMu.Unlock()

    s.jobQueue <- JobRequest{Song: song, Args: buildPipelineArgs(req), Config: req}

    w.WriteHeader(http.StatusAccepted)
    json.NewEncoder(w).Encode(map[string]string{"status": "queued", "song": song})
}
```

### Task 6: Eliminar código obsoleto

- Eliminar `handleStatus` (GET /api/status) → reemplazado por GET /api/queue/status
- Eliminar `handleEvents` (SSE /api/events) → reemplazado por polling de queue
- Eliminar `pipelineStatusFilePath()` y constantes relacionadas
- Eliminar `SeparateResponse`, `StatusResponse` structs obsoletos

---

## 🟢 Frontend — Svelte

### Task 7: api.ts — nuevos tipos y endpoints

```typescript
export interface QueueJob {
  song: string;
  status: 'waiting' | 'processing' | 'done' | 'error';
  progress: number;
  error?: string;
  files?: { name: string; path: string }[];
}

export interface QueueStatusResponse {
  jobs: QueueJob[];
}

export async function addToQueue(opts: SeparateOptions): Promise<{status: string; song: string}> {
  const body = buildRequestBody(opts);
  const res = await fetch(`${API_BASE}/api/separate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`Queue add failed: ${res.status}`);
  return (await res.json()) as {status: string; song: string};
}

export async function getQueueStatus(): Promise<QueueStatusResponse> {
  const res = await fetch(`${API_BASE}/api/queue/status`);
  if (!res.ok) throw new Error(`Queue status failed: ${res.status}`);
  return (await res.json()) as QueueStatusResponse;
}
```

### Task 8: PipelinePanel.svelte — cola visible

Añadir sección de cola debajo del DropZone:

```svelte
<!-- Cola de trabajos -->
{#if queueJobs.length > 0}
  <div class="queue-section">
    <h3>📋 Cola de procesamiento</h3>
    {#each queueJobs as job}
      <div class="queue-item" class:processing={job.status === 'processing'} 
           class:done={job.status === 'done'} class:error={job.status === 'error'}>
        <span class="queue-song">{job.song}</span>
        <span class="queue-status">{job.status}</span>
        {#if job.status === 'error' && job.error}
          <span class="queue-error">{job.error}</span>
        {/if}
      </div>
    {/each}
  </div>
{/if}
```

Estado `queueJobs` actualizado vía polling de `/api/queue/status` cada 500ms.

### Task 9: App.svelte — integrar cola

Cambiar `handlePipelineStart`:
```typescript
async function handlePipelineStart(config) {
  // Subir archivos
  for (const qf of checked) {
    const path = await uploadAudio(qf.file);
    // Añadir a cola (una llamada por canción)
    await addToQueue({...config, input: path});
  }
  // Iniciar polling de cola
  startQueuePolling();
}
```

`startQueuePolling()`: cada 500ms llama a `getQueueStatus()`, actualiza `queueJobs`, y cuando un job pasa a `done`, añade sus stems a `results` acumulados.

### Task 10: ResultsPanel.svelte — resultados acumulados

Cambiar `loadResults()` para que lea del filesystem y acumule:
```typescript
async function loadResults() {
  // TODO: endpoint que liste canciones en /output/
  // Por ahora, usar queueJobs con status=done
  const allSongs = queueJobs.filter(j => j.status === 'done');
  // Agrupar stems por canción
  results = allSongs.map(j => ({
    song: j.song,
    files: j.files || []
  }));
}
```

Los resultados se acumulan: la canción 1 no desaparece cuando termina la 2.

---

## ✅ Verificación

- Subir 3 canciones → ejecutar → ver cola: "waiting, waiting, waiting"
- Primera en "processing", otras en "waiting"
- Al terminar primera → aparece en resultados, segunda pasa a "processing"
- Resultados muestra canción 1 Y canción 2 (no se reemplazan)
- Borrar canción 1 → solo desaparece esa, canción 2 sigue
- Si una falla → estado "error" con mensaje, pasa a la siguiente
