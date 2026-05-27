<script lang="ts">
  import ResultsPanel from './lib/ResultsPanel.svelte';
  import PipelinePanel from './lib/PipelinePanel.svelte';
  import ConfigPanel from './lib/ConfigPanel.svelte';
  import StatusBar from './lib/StatusBar.svelte';
  import type { ResultStem } from './lib/types';
  import { detectStemType } from './lib/types';
  import { getModels, separateAudio, getStatus, uploadAudio, getLocalModels } from './lib/api';
  import type { LocalModel, StatusResponse } from './lib/api';

  interface QueueFile {
    file: File;
    id: string;
    status: string;
    checked: boolean;
    progress?: number;
    path?: string;
    errorMsg?: string;
  }
  interface PipelineConfigType {
    preset?: string;
    viperx: boolean;
    viperxKeep?: string;
    demucs: boolean;
    demucsKeep?: string[];
  }

  // ---- State ----
  let queueFiles = $state<QueueFile[]>([]);
  let presets = $state<Record<string, any>>({});
  let separating = $state(false);
  let results = $state<ResultStem[]>([]);
  let modelsError = $state(false);
  let pitchValue = $state(0);
  let pipelineStatus = $state<'idle'|'running'|'done'|'error'>('idle');
  let pipelineStep = $state('');
  let pipelineSong = $state('');
  let pipelineEta = $state(0);
  let currentProgress = $state(0);
  let pipelineError = $state('');
  let pipelineModel = $state('');
  let pollingTimer: ReturnType<typeof setInterval> | null = null;

  // Toast
  let toastMessage = $state('');
  let toastType = $state<'success' | 'error'>('success');
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  function showToast(message: string, type: 'success' | 'error') {
    toastMessage = message;
    toastType = type;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toastMessage = '';
    }, 3000);
  }

  // Advanced model config
  let modelConfig = $state({
    vocalModel: '',
    stemModel: '',
    drumsModel: '',
    bassModel: '',
    otherModel: '',
    vocalOverlap: 4,
  });
  let modelInfos = $state<LocalModel[]>([]);
  let showModelConfig = $state(false);

  // Load presets + model list on mount
  $effect(() => {
    getModels()
      .then((p) => (presets = p))
      .catch(() => {
        modelsError = true;
      });
    getLocalModels()
      .then((res) => (modelInfos = res.models || []))
      .catch(() => {}); // silent fail — dropdowns just stay empty

    // Cargar resultados existentes al iniciar (separación previa completada)
    getStatus()
      .then((status) => {
        console.log('getStatus response:', status.status, status.files?.length);
        if (
          status.status === 'done' &&
          status.files &&
          status.files.length > 0
        ) {
          loadResults(status);
          pipelineStep = 'completado';
          currentProgress = 1;
          console.log('Loaded existing results:', results.length, results);
        } else {
          console.log('No results to load - status:', status.status, 'files:', status.files?.length);
        }
      })
      .catch((err) => {
        console.error('Failed to load existing results:', err);
      });
  });

  // Debug: trace results reactivity for production diagnosis
  $inspect('results changed:', results.length, results);

  // ---- File Queue handlers ----
  function handleFilesAdded(newFiles: File[]) {
    const newItems: QueueFile[] = newFiles.map((f) => ({
      file: f,
      id: crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`,
      status: 'waiting',
      checked: true,
    }));
    queueFiles = [...queueFiles, ...newItems];
  }

  function handleDropZoneFile(f: File) {
    handleFilesAdded([f]);
  }

  function handleClearQueue() {
    queueFiles = [];
    results = [];
    showToast('Cola limpiada', 'success');
  }

  function handleToggleQueueFile(id: string) {
    queueFiles = queueFiles.map((qf) =>
      qf.id === id ? { ...qf, checked: !qf.checked } : qf,
    );
  }

  function handlePitchApply(pitch: number) {
    pitchValue = pitch;
  }

  // ---- Preset start (from PresetSelector) ----
  function handlePresetStart(preset: string) {
    const p = presets[preset];
    if (!p) {
      alert('Preset not found: ' + preset);
      return;
    }
    handlePipelineStart({
      preset,
      viperx: !!p.vocalModel,
      viperxKeep: 'both',
      demucs: !!p.stemModel,
      demucsKeep: ['drums', 'bass', 'other', 'vocals'],
    });
  }

  // ---- Per-step handlers (PipelineConfig individual buttons) ----
  async function handleViperxOnly(config: PipelineConfigType) {
    await handlePipelineStart({ ...config, demucs: false });
    // Force refresh after pipeline completes
    setTimeout(async () => {
      try {
        const status = await getStatus();
        if (status.status === 'done' && status.files && status.files.length > 0) {
          loadResults(status);
        }
      } catch { /* ignore transient errors */ }
    }, 5000);
  }

  async function handleDemucsOnly(config: PipelineConfigType) {
    await handlePipelineStart({ ...config, viperx: false });
    // Force refresh after pipeline completes
    setTimeout(async () => {
      try {
        const status = await getStatus();
        if (status.status === 'done' && status.files && status.files.length > 0) {
          loadResults(status);
        }
      } catch { /* ignore transient errors */ }
    }, 5000);
  }

  // ---- Pipeline start ----
  async function handlePipelineStart(config: PipelineConfigType) {
    // Clear any existing polling
    if (pollingTimer) {
      clearInterval(pollingTimer);
      pollingTimer = null;
    }

    const checked = queueFiles.filter((qf) => qf.checked && qf.status !== 'done');
    if (checked.length === 0) {
      alert('No checked files in queue.');
      return;
    }

    separating = true;
    results = [];
    pipelineStatus = 'idle';
    pipelineStep = '';
    pipelineError = '';
    currentProgress = 0;
    pipelineEta = 0;

    // Mark checked files as uploading
    for (const qf of checked) {
      qf.status = 'uploading';
      qf.progress = 0;
    }

    try {
      // Upload all checked files
      const uploaded: { qf: QueueFile; path: string }[] = [];
      for (const qf of checked) {
        try {
          const res = await uploadAudio(qf.file);
          qf.status = 'processing';
          qf.path = res.path;
          uploaded.push({ qf, path: res.path });
        } catch (err: any) {
          qf.status = 'error';
          qf.errorMsg = err.message;
        }
      }

      if (uploaded.length === 0) {
        separating = false;
        alert('No files uploaded successfully.');
        return;
      }

      // Use the preset passed from PresetSelector, or fall back to 'balance'
      const preset = config.preset || (Object.keys(presets).length > 0 ? 'balance' : 'htdemucs');

      // Start separation for each uploaded file
      for (const { qf, path } of uploaded) {
        try {
          qf.status = 'processing';
          qf.progress = 0;

          await separateAudio({
            preset,
            input: path,
            pitch: pitchValue !== 0 ? pitchValue : undefined,
            viperx: config.viperx,
            viperx_keep: config.viperxKeep,
            demucs: config.demucs,
            demucs_keep: config.demucsKeep,
          });
        } catch (err: any) {
          qf.status = 'error';
          qf.errorMsg = err.message;
        }
      }

      // Start polling /api/status every 500ms
      pipelineStatus = 'running';
      pipelineSong = uploaded[0]?.qf.file.name || '';

      pollingTimer = setInterval(async () => {
        try {
          const status = await getStatus();
          pipelineStep = status.step || '';
          pipelineModel = status.vocal_model || status.stem_model || '';
          pipelineEta = status.eta || 0;
          currentProgress = status.progress || 0;

          // Update progress on the first uploaded file
          if (uploaded.length > 0) {
            uploaded[0].qf.progress = status.progress || 0;
          }

          if (status.status === 'done') {
            pipelineStatus = 'done';
            clearInterval(pollingTimer!);
            pollingTimer = null;

            loadResults(status);
            separating = false;
            // Marcar archivos de la cola como done
            if (uploaded.length > 0) {
              for (const uf of uploaded) {
                uf.qf.status = 'done';
                uf.qf.progress = 1;
              }
            }
          } else if (status.status === 'error') {
            pipelineStatus = 'error';
            pipelineError = status.error || 'Unknown error';
            clearInterval(pollingTimer!);
            pollingTimer = null;
            separating = false;
            if (uploaded.length > 0) {
              uploaded[0].qf.status = 'error';
              uploaded[0].qf.errorMsg = status.error || 'Unknown error';
            }
          }
        } catch (e) {
          // keep polling on transient network errors
        }
      }, 500);
    } catch (err: any) {
      alert('Pipeline error: ' + err.message);
      separating = false;
    }
  }

  function extractSongFromName(name: string): string {
    return name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, '');
  }

  function loadResults(status: StatusResponse) {
    if (status.files && status.files.length > 0) {
      const newResults: ResultStem[] = status.files.map((f: any) => ({
        name: f.name,
        path: f.path,
        song: status.song || extractSongFromName(f.name),
        stemType: detectStemType(f.name),
      }));
      results = [...newResults];
      pipelineStatus = 'done';
    }
  }

  // ---- ResultsPanel delete callbacks ---- 
  async function handleStemDeleted() {
    try {
      const status = await getStatus();
      if (status.files && status.files.length > 0) {
        const newResults: ResultStem[] = status.files.map((f: any) => ({
          name: f.name,
          path: f.path,
          song: status.song || f.name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, ''),
          stemType: detectStemType(f.name),
        }));
        results = [...newResults];
      } else {
        results = [];
        pipelineStatus = 'idle';
      }
    } catch (err) {
      console.error('Failed to refresh results after stem delete:', err);
    }
  }

  async function handleGroupDeleted() {
    try {
      const status = await getStatus();
      if (status.files && status.files.length > 0) {
        const newResults: ResultStem[] = status.files.map((f: any) => ({
          name: f.name,
          path: f.path,
          song: status.song || f.name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, ''),
          stemType: detectStemType(f.name),
        }));
        results = [...newResults];
      } else {
        results = [];
        pipelineStatus = 'idle';
      }
    } catch (err) {
      console.error('Failed to refresh results after group delete:', err);
    }
  }

  // ---- DropZone + FileQueue helpers ----
  function handleDropZoneDragOver(e: DragEvent) {
    e.preventDefault();
  }

  function handleDropZoneDrop(e: DragEvent) {
    e.preventDefault();
    const files = e.dataTransfer?.files;
    if (files && files.length > 0) {
      for (let i = 0; i < files.length; i++) {
        handleDropZoneFile(files[i]);
      }
    }
  }

  function handleDropZoneClick() {
    const input = document.getElementById('dropzone-input') as HTMLInputElement;
    input?.click();
  }

  function handleDropZoneInput(e: Event) {
    const input = e.target as HTMLInputElement;
    if (input.files) {
      handleFilesAdded(Array.from(input.files));
      input.value = '';
    }
  }

  function handleRemoveQueueFile(id: string) {
    queueFiles = queueFiles.filter((qf) => qf.id !== id);
  }

  function statusBadgeClass(status: string): string {
    switch (status) {
      case 'done': return 'badge badge-green';
      case 'error': return 'badge badge-red';
      case 'processing': return 'badge badge-yellow';
      case 'uploading': return 'badge badge-blue';
      default: return 'badge';
    }
  }
</script>

<main>
  <header>
    <h1>🎵 Onda</h1>
    <span class="version">v2.0.0-alpha</span>
  </header>

  <!-- DropZone -->
  <section class="dropzone-section">
    <div
      class="dropzone"
      ondragover={handleDropZoneDragOver}
      ondrop={handleDropZoneDrop}
      onclick={handleDropZoneClick}
      role="button"
      tabindex="0"
    >
      <span class="dropzone-icon">📂</span>
      <span class="dropzone-text">Arrastra archivos aquí o haz clic</span>
      <span class="dropzone-hint">WAV, MP3, FLAC, OGG, M4A</span>
    </div>
    <input
      id="dropzone-input"
      type="file"
      hidden
      accept="audio/*"
      multiple
      onchange={handleDropZoneInput}
    />
  </section>

  <!-- FileQueue -->
  {#if queueFiles.length > 0}
    <section class="queue-section">
      <div class="queue-header">
        <span class="queue-title">📋 Cola ({queueFiles.length})</span>
        <button class="btn-clear" onclick={handleClearQueue}>Limpiar</button>
      </div>
      <div class="queue-list">
        {#each queueFiles as qf (qf.id)}
          <div class="queue-row">
            <input
              type="checkbox"
              checked={qf.checked}
              onchange={() => handleToggleQueueFile(qf.id)}
              disabled={qf.status === 'done'}
            />
            <span class="queue-name" title={qf.file.name}>{qf.file.name}</span>
            <span class={statusBadgeClass(qf.status)}>{qf.status}</span>
            <button class="btn-remove" onclick={() => handleRemoveQueueFile(qf.id)}>✕</button>
          </div>
        {/each}
      </div>
    </section>
  {/if}

  <!-- PipelinePanel -->
  <section class="pipeline-section">
    <PipelinePanel
      disabled={separating}
      presets={presets}
      onstart={handlePipelineStart}
      onviperxonly={handleViperxOnly}
      ondemucsonly={handleDemucsOnly}
      onpitch={handlePitchApply}
    />
  </section>

  <!-- ConfigPanel -->
  <section class="config-section">
    <ConfigPanel
      disabled={separating}
      onchange={(cfg) => (modelConfig = cfg)}
    />
  </section>

  <!-- Progress -->
  {#if pipelineStatus !== 'idle'}
    <section class="progress-section">
      <div class="progress-card">
        <div class="progress-header">
          <span class="progress-status">{pipelineStatus}</span>
          {#if pipelineStep}
            <span class="progress-step">{pipelineStep}</span>
          {/if}
          {#if pipelineModel}
            <span class="progress-model">{pipelineModel}</span>
          {/if}
        </div>
        <div class="progress-bar-wrap">
          <div class="progress-bar-fill" style="width: {currentProgress * 100}%"></div>
        </div>
        <div class="progress-meta">
          <span class="progress-pct">{Math.round(currentProgress * 100)}%</span>
          {#if pipelineSong}
            <span class="progress-song">{pipelineSong}</span>
          {/if}
          {#if pipelineEta > 0}
            <span class="progress-eta">ETA: {pipelineEta}s</span>
          {/if}
        </div>
        {#if pipelineError}
          <div class="progress-error">{pipelineError}</div>
        {/if}
      </div>
    </section>
  {/if}

  <!-- ResultsPanel -->
  {#if results.length > 0}
    <section class="results">
      <ResultsPanel files={results} onstemdeleted={handleStemDeleted} ongroupdeleted={handleGroupDeleted} />
    </section>
  {/if}

  <!-- StatusBar -->
  <StatusBar />

  <!-- Toast -->
  {#if toastMessage}
    <div class="toast {toastType}">{toastMessage}</div>
  {/if}
</main>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    background: #0a0a14;
    color: #e0e0e0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto,
      Oxygen-Sans, Ubuntu, Cantarell, 'Helvetica Neue', sans-serif;
    min-height: 100vh;
  }

  main {
    display: flex;
    flex-direction: column;
    align-items: center;
    max-width: 800px;
    margin: 0 auto;
    padding: 2rem 1.5rem 4rem;
    gap: 1.5rem;
    padding-bottom: 60px; /* space for StatusBar */
  }

  header {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    padding: 0.75rem 0 0.5rem;
    width: 100%;
    border-bottom: 2px solid transparent;
    border-image: linear-gradient(
        90deg,
        rgba(0, 212, 255, 0.3),
        rgba(0, 212, 255, 0.05)
      )
      1;
  }

  header h1 {
    margin: 0;
    font-size: 1.75rem;
    font-weight: 700;
    background: linear-gradient(135deg, #00d4ff, #b388ff);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .version {
    font-size: 0.8rem;
    color: #555;
    font-weight: 500;
    letter-spacing: 0.5px;
  }

  /* DropZone */
  .dropzone-section {
    width: 100%;
  }

  .dropzone {
    width: 100%;
    box-sizing: border-box;
    border: 2px dashed #2a2a4a;
    border-radius: 12px;
    padding: 2rem 1rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    background: #0e0e1a;
  }
  .dropzone:hover {
    border-color: #00d4ff;
    background: #111128;
  }
  .dropzone-icon {
    font-size: 2rem;
  }
  .dropzone-text {
    font-size: 0.95rem;
    font-weight: 600;
    color: #c0c0d0;
  }
  .dropzone-hint {
    font-size: 0.75rem;
    color: #606080;
  }

  /* FileQueue */
  .queue-section {
    width: 100%;
  }
  .queue-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }
  .queue-title {
    font-size: 0.9rem;
    font-weight: 600;
    color: #c0c0d0;
  }
  .btn-clear {
    padding: 0.3rem 0.8rem;
    background: #2a1a1a;
    border: 1px solid #4a2a2a;
    border-radius: 6px;
    color: #e57373;
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
  }
  .btn-clear:hover {
    background: #3a1a1a;
  }
  .queue-list {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }
  .queue-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 8px;
    font-size: 0.85rem;
  }
  .queue-row input[type="checkbox"] {
    accent-color: #00d4ff;
    flex-shrink: 0;
  }
  .queue-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    color: #e0e0e0;
  }
  .badge {
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
    font-size: 0.65rem;
    font-weight: 700;
    text-transform: uppercase;
    flex-shrink: 0;
    background: #2a2a4a;
    color: #888;
  }
  .badge-green { background: #1b3a1b; color: #81c784; }
  .badge-red { background: #3a1b1b; color: #e57373; }
  .badge-yellow { background: #3a3a1b; color: #ffd54f; }
  .badge-blue { background: #1b2a3a; color: #64b5f6; }
  .btn-remove {
    background: none;
    border: none;
    color: #666;
    font-size: 0.8rem;
    cursor: pointer;
    padding: 0.1rem 0.3rem;
  }
  .btn-remove:hover {
    color: #e57373;
  }

  /* Sections */
  .pipeline-section {
    width: 100%;
  }

  .config-section {
    width: 100%;
  }

  .progress-section {
    width: 100%;
  }

  .results {
    width: 100%;
  }

  /* Progress card */
  .progress-card {
    width: 100%;
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 12px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }
  .progress-header {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .progress-status {
    font-weight: 700;
    color: #00d4ff;
    text-transform: uppercase;
    font-size: 0.8rem;
  }
  .progress-step {
    font-size: 0.8rem;
    color: #c0c0d0;
  }
  .progress-model {
    font-size: 0.7rem;
    color: #606080;
    margin-left: auto;
  }
  .progress-bar-wrap {
    width: 100%;
    height: 8px;
    background: #0a0a14;
    border-radius: 4px;
    overflow: hidden;
  }
  .progress-bar-fill {
    height: 100%;
    background: linear-gradient(90deg, #00d4ff, #b388ff);
    border-radius: 4px;
    transition: width 0.3s ease;
  }
  .progress-meta {
    display: flex;
    gap: 1rem;
    font-size: 0.75rem;
    color: #888;
  }
  .progress-pct {
    font-weight: 700;
    color: #00d4ff;
  }
  .progress-eta {
    margin-left: auto;
  }
  .progress-error {
    font-size: 0.8rem;
    color: #e57373;
    background: #2a1a1a;
    padding: 0.5rem;
    border-radius: 6px;
  }

  /* Toast */
  .toast {
    position: fixed;
    bottom: 60px;
    left: 50%;
    transform: translateX(-50%);
    padding: 12px 24px;
    border-radius: 8px;
    color: white;
    font-weight: 600;
    z-index: 1000;
    animation: toastIn 0.3s ease, toastOut 0.3s ease 2.7s forwards;
  }
  .toast.success {
    background: #4caf50;
  }
  .toast.error {
    background: #f44336;
  }
  @keyframes toastIn {
    from {
      opacity: 0;
      transform: translateX(-50%) translateY(20px);
    }
  }
  @keyframes toastOut {
    to {
      opacity: 0;
      transform: translateX(-50%) translateY(-20px);
    }
  }

  /* Smooth transitions between states */
  section {
    animation: fadeIn 0.3s ease;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  /* Responsive */
  @media (max-width: 600px) {
    main {
      padding: 1rem 1rem 3rem;
      gap: 1rem;
    }

    header h1 {
      font-size: 1.5rem;
    }
  }
</style>
