<script lang="ts">
  import PresetsPanel from './PresetsPanel.svelte';
  import { uploadAudio, deleteInput } from './api';
  import { IconUpload } from './icons';

  interface QueueFile {
    file: File;
    id: string;
    status: string;
    checked: boolean;
    progress?: number;
    path?: string;
    errorMsg?: string;
  }

  let {
    presetName = '',
    displayName = '',
    queueFiles = [] as QueueFile[],
    savedPresets = [] as {name: string, config: any}[],
    separating = false,
    pipelineStatus = 'idle',
    currentProgress = 0,
    pipelineStep = '',
    pipelineSong = '',
    pipelineEta = '',
    inferenceDevice = '',
    hidePresetSelector = false,
    onPresetChange = (name: string) => {},
    onError = (msg: string) => {},
    onQueueChange = (files: QueueFile[]) => {},
    onStart = (config: any) => {},
    onCancel = () => {},
    onRemoveFile = (id: string) => {},
  } = $props();

  // ---- Drag & Drop state ----
  let dragCounter = $state(0);

  // ---- File handlers ----
  async function handleFilesAdded(newFiles: File[]) {
    const updated = [...queueFiles];
    for (const f of newFiles) {
      const id = crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
      const qf: QueueFile = {
        file: f,
        id,
        status: 'uploading',
        checked: true,
      };
      updated.push(qf);
      onQueueChange([...updated]);
      try {
        const res = await uploadAudio(f);
        const idx = updated.findIndex(q => q.id === id);
        if (idx !== -1) {
          updated[idx] = { ...updated[idx], status: 'waiting', path: res.path };
          onQueueChange([...updated]);
        }
      } catch (err: any) {
        const idx = updated.findIndex(q => q.id === id);
        if (idx !== -1) {
          updated[idx] = { ...updated[idx], status: 'error', errorMsg: err.message || 'Upload failed' };
          onQueueChange([...updated]);
        }
      }
    }
  }

  function handleDropZoneFile(f: File) {
    handleFilesAdded([f]);
  }

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
    const input = document.getElementById('pipeline-dropzone-input') as HTMLInputElement;
    input?.click();
  }

  function handleDropZoneInput(e: Event) {
    const input = e.target as HTMLInputElement;
    if (input.files) {
      handleFilesAdded(Array.from(input.files));
      input.value = '';
    }
  }

  function handleToggleQueueFile(id: string) {
    const updated = queueFiles.map((qf) =>
      qf.id === id ? { ...qf, checked: !qf.checked } : qf,
    );
    onQueueChange(updated);
  }

  function handleToggleAll() {
    const allChecked = queueFiles.every(qf => qf.checked);
    const updated = queueFiles.map(qf => ({ ...qf, checked: !allChecked }));
    onQueueChange(updated);
  }

  function handleClearQueue() {
    onQueueChange([]);
  }

  async function handleRemoveQueueFile(id: string) {
    const qf = queueFiles.find((q) => q.id === id);
    if (!qf) return;
    if (qf.path) {
      try {
        await deleteInput(qf.file.name);
        onQueueChange(queueFiles.filter((q) => q.id !== id));
      } catch (err: any) {
        // silently fail — caller may handle
      }
    } else {
      onQueueChange(queueFiles.filter((q) => q.id !== id));
    }
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

  // ---- Execute handler ----
  function handleExecute() {
    const selected = savedPresets.find(p => p.name === presetName);
    if (!selected) {
      // Built-in preset mode: use hardcoded default configs
      if (hidePresetSelector && presetName) {
        const hardcodedConfig = getHardcodedPreset(presetName);
        if (hardcodedConfig) {
          onStart(hardcodedConfig);
          return;
        }
      }
      onError(`Preset "${presetName}" no encontrado en el servidor`);
      return;
    }
    const config = selected.config;
    config.preset = presetName || undefined;
    onStart(config);
  }

  /** Hardcoded configs for built-in presets (fallback when backend has no saved presets) */
  function getHardcodedPreset(name: string): Record<string, any> | null {
    switch (name) {
      case 'Separador Voces Total':
        return { viperx: true, viperxKeep: 'both', vocalModel: 'model_bs_roformer_ep_317_sdr_12.9755' };
      case 'Eliminador de Voz':
        return { viperx: true, viperxKeep: 'instrumental', vocalModel: 'model_bs_roformer_ep_317_sdr_12.9755' };
      case 'Separador Completo':
        return { demucs: true, demucsKeep: ['drums', 'bass', 'other', 'vocals'], stemModel: 'htdemucs_ft' };
      case 'Separador solo instrumentos':
        return { demucs: true, demucsKeep: ['drums', 'bass', 'other'], stemModel: 'htdemucs_ft' };
      default:
        return null;
    }
  }
</script>

<section class="pipeline-view">
  <h2 class="pipeline-title">{displayName || presetName || 'Pipeline'}</h2>

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
      <span class="dropzone-icon">{@html IconUpload}</span>
      <span class="dropzone-text">Arrastra archivos aquí o haz clic</span>
      <span class="dropzone-hint">WAV, MP3, FLAC, OGG, M4A</span>
    </div>
    <input
      id="pipeline-dropzone-input"
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
        <span class="queue-title">Cola ({queueFiles.length})</span>
        <button class="btn-clear" onclick={handleClearQueue}>Limpiar</button>
      </div>
      <div class="queue-columns-header">
        <input
          type="checkbox"
          checked={queueFiles.length > 0 && queueFiles.every(qf => qf.checked)}
          onchange={handleToggleAll}
          title="Seleccionar / Deseleccionar todas"
        />
        <span class="col-title">Título</span>
        <span class="col-progress">Progreso</span>
        <span class="col-status">Estado</span>
        <span class="col-action"></span>
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

  <!-- PresetsPanel (locked to presetName, no preset selector) -->
  {#if queueFiles.length > 0}
    {#if hidePresetSelector}
      <!-- Built-in preset: no selector, direct execute button -->
      <section class="direct-execute-section">
        <button class="btn-execute-direct" onclick={handleExecute} disabled={separating || queueFiles.filter(q => q.checked).length === 0}>
          ▶ Ejecutar {displayName || presetName}
        </button>

        {#if separating}
          <button class="btn-stop" onclick={onCancel}>⏹ Detener</button>

          <div class="progress-card">
            <div class="progress-header">
              <span class="progress-status">{pipelineStatus}</span>
              {#if pipelineStep}<span class="progress-step">{pipelineStep}</span>{/if}
            </div>
            <div class="progress-bar-wrap">
              <div class="progress-bar-fill" style="width: {currentProgress * 100}%"></div>
            </div>
            <div class="progress-meta">
              <span class="progress-pct">{Math.round(currentProgress * 100)}%</span>
              {#if pipelineSong}<span class="progress-song">{pipelineSong}</span>{/if}
              {#if pipelineEta}<span class="progress-eta">⏱ {pipelineEta}</span>{/if}
              {#if inferenceDevice}
                <span class="progress-device">{inferenceDevice === 'cuda' || inferenceDevice === 'gpu' ? 'GPU' : 'CPU'}</span>
              {/if}
            </div>
          </div>
        {/if}
      </section>
    {:else}
      <PresetsPanel
        presets={savedPresets}
        selectedPreset={presetName}
        onSelectPreset={(name) => onPresetChange(name)}
        hasFiles={queueFiles.some(qf => qf.checked)}
        disabled={separating}
        onExecute={handleExecute}
        onCancel={onCancel}
        progress={currentProgress}
        status={pipelineStatus}
        step={pipelineStep}
        song={pipelineSong}
        eta={pipelineEta}
        device={inferenceDevice}
      />
    {/if}
  {/if}
</section>

<style>
  .pipeline-view {
    width: 100%;
    margin: 0 auto;
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1.5rem;
    box-sizing: border-box;
    padding: 0 1rem;
  }

  .pipeline-title {
    margin: 0;
    font-size: 1.2rem;
    font-weight: 700;
    color: var(--text-primary);
    text-align: center;
    padding: 0.5rem 1rem;
    border-bottom: 2px solid transparent;
    border-image: linear-gradient(90deg, var(--accent-glow), var(--accent-subtle)) 1;
    width: 100%;
    max-width: 500px;
  }

  /* DropZone */
  .dropzone-section {
    width: 100%;
  }

  .dropzone {
    width: 100%;
    box-sizing: border-box;
    border: 2px dashed var(--border);
    border-radius: 12px;
    padding: 2rem 1rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    background: var(--bg-primary);
  }
  .dropzone:hover {
    border-color: var(--accent);
    background: var(--bg-hover);
  }
  .dropzone-icon {
    font-size: 2rem;
  }
  .dropzone-text {
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--text-primary);
  }
  .dropzone-hint {
    font-size: 0.75rem;
    color: var(--text-muted);
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
    color: var(--text-primary);
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
  .queue-columns-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    background: rgba(128,128,128,0.08);
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-secondary);
  }
  .queue-columns-header input[type="checkbox"] {
    flex-shrink: 0;
    width: 16px;
    height: 16px;
    cursor: pointer;
    accent-color: var(--accent);
  }
  .col-title { flex: 1; }
  .col-progress { width: 180px; text-align: center; }
  .col-status { width: 90px; text-align: center; }
  .col-action { width: 32px; }
  .queue-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    font-size: 0.85rem;
  }
  .queue-row input[type="checkbox"] {
    accent-color: var(--accent);
    flex-shrink: 0;
  }
  .queue-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    color: var(--text-primary);
  }
  .badge {
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
    font-size: 0.65rem;
    font-weight: 700;
    text-transform: uppercase;
    flex-shrink: 0;
    background: #2a2a4a;
    color: var(--text-secondary);
  }
  .badge-green { background: #1b3a1b; color: #81c784; }
  .badge-red { background: #3a1b1b; color: #e57373; }
  .badge-yellow { background: #3a3a1b; color: #ffd54f; }
  .badge-blue { background: #1b2a3a; color: #64b5f6; }
  .btn-remove {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 0.8rem;
    cursor: pointer;
    padding: 0.1rem 0.3rem;
  }
  .btn-remove:hover {
    color: #e57373;
  }

  /* Animation */
  .pipeline-view {
    animation: fadeIn 0.3s ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  /* Direct execute section (hidePresetSelector mode) */
  .direct-execute-section {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 20px;
    margin: 12px 0;
  }

  .btn-execute-direct {
    width: 100%;
    padding: 14px;
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: 8px;
    font-size: 17px;
    font-weight: bold;
    cursor: pointer;
    margin-bottom: 12px;
    transition: background 0.2s;
  }
  .btn-execute-direct:hover {
    background: var(--accent-light);
  }
  .btn-execute-direct:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .progress-card {
    background: var(--bg-primary);
    border-radius: 8px;
    padding: 14px;
  }
  .progress-header {
    display: flex;
    gap: 12px;
    align-items: center;
    margin-bottom: 8px;
  }
  .progress-status {
    font-weight: bold;
    color: var(--accent-light);
    text-transform: uppercase;
    font-size: 13px;
  }
  .progress-step {
    color: var(--text-secondary);
    font-size: 13px;
  }
  .progress-bar-wrap {
    height: 8px;
    background: var(--bg-surface);
    border-radius: 4px;
    margin-bottom: 8px;
    overflow: hidden;
  }
  .progress-bar-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), #4caf50);
    border-radius: 4px;
    transition: width 0.3s ease;
  }
  .progress-meta {
    display: flex;
    gap: 16px;
    flex-wrap: wrap;
    align-items: center;
    font-size: 12px;
  }
  .progress-pct {
    font-weight: bold;
    color: #4caf50;
    font-size: 16px;
  }
  .progress-song {
    color: var(--text-secondary);
  }
  .progress-eta {
    color: #ff9800;
  }
  .progress-device {
    color: var(--text-secondary);
    font-size: 11px;
    background: rgba(128,128,128,0.1);
    padding: 2px 8px;
    border-radius: 4px;
  }

  .btn-stop {
    width: 100%;
    padding: 12px;
    background: #4a1a1a;
    color: #e57373;
    border: 1px solid #6a2a2a;
    border-radius: 8px;
    font-size: 15px;
    font-weight: bold;
    cursor: pointer;
    margin-bottom: 12px;
    transition: background 0.2s;
  }
  .btn-stop:hover {
    background: #5a2a2a;
  }
</style>
