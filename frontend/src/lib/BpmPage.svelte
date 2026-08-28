<script lang="ts">
  import { onMount } from 'svelte';
  import { uploadAudio, detectBpm, getInputs, type TempoResponse } from './api';
  import { IconUpload } from './icons';

  type InputFile = { name: string; path: string };

  let fileInput: HTMLInputElement | null = $state(null);
  let existingInputs = $state<InputFile[]>([]);
  let selectedInput = $state('');
  let loading = $state(false);
  let error = $state('');
  let result = $state<TempoResponse | null>(null);
  let uploadedFileName = $state('');

  onMount(() => {
    loadInputs();
  });

  async function loadInputs() {
    try {
      existingInputs = await getInputs();
    } catch {
      existingInputs = [];
    }
  }

  function resetResult() {
    result = null;
    error = '';
  }

  function baseNameFromPath(path: string): string {
    return path.split('/').pop() || path;
  }

  async function handleFile(file: File) {
    resetResult();
    loading = true;
    try {
      const uploaded = await uploadAudio(file);
      uploadedFileName = baseNameFromPath(uploaded.path);
      await runDetection(uploadedFileName);
    } catch (err: any) {
      error = err?.message || 'Error al subir el archivo';
    } finally {
      loading = false;
    }
  }

  async function handleSelectExisting() {
    if (!selectedInput) return;
    resetResult();
    loading = true;
    try {
      uploadedFileName = selectedInput;
      await runDetection(selectedInput);
    } catch (err: any) {
      error = err?.message || 'Error al detectar el BPM';
    } finally {
      loading = false;
    }
  }

  async function runDetection(fileName: string) {
    const data = await detectBpm(fileName);
    result = data;
  }

  function handleInputChange(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file) {
      handleFile(file);
    }
    input.value = '';
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    const files = e.dataTransfer?.files;
    if (files && files.length > 0) {
      handleFile(files[0]);
    }
  }

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
  }
</script>

<section class="bpm-page">
  <header class="page-header">
    <h2>Detectar velocidad (BPM)</h2>
    <div class="header-actions">
      <button class="btn-primary" onclick={() => fileInput?.click()} disabled={loading}>
        <span class="icon">{@html IconUpload}</span>
        Subir audio
      </button>
      <input
        bind:this={fileInput}
        type="file"
        accept="audio/*"
        onchange={handleInputChange}
        class="file-input"
      />
    </div>
  </header>

  {#if error}
    <div class="page-error">
      <span>{error}</span>
      <button class="btn-close-error" onclick={() => (error = '')}>✕</button>
    </div>
  {/if}

  <div class="bpm-card">
    {#if existingInputs.length > 0}
      <label class="existing-select">
        <span>O selecciona un archivo ya cargado:</span>
        <select bind:value={selectedInput} onchange={handleSelectExisting} disabled={loading}>
          <option value="">— Seleccionar —</option>
          {#each existingInputs as input (input.path)}
            <option value={baseNameFromPath(input.path)}>{input.name}</option>
          {/each}
        </select>
      </label>
    {/if}

    {#if loading}
      <div class="bpm-loading">
        <span class="spinner"></span>
        <span>{uploadedFileName ? 'Detectando BPM…' : 'Subiendo audio…'}</span>
      </div>
    {:else if result}
      <div class="bpm-result">
        <div class="bpm-value">{result.bpm.toFixed(2)}</div>
        <div class="bpm-label">BPM detectados</div>
        <div class="bpm-meta">
          <span>Duración: {result.duration.toFixed(2)} s</span>
          <span>Beats: {result.beats.length}</span>
        </div>
        {#if uploadedFileName}
          <div class="bpm-file">{uploadedFileName}</div>
        {/if}
      </div>
    {:else}
      <div
        class="bpm-empty"
        role="button"
        tabindex="0"
        onclick={() => fileInput?.click()}
        onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') fileInput?.click(); }}
        ondrop={handleDrop}
        ondragover={handleDragOver}
      >
        <span class="empty-icon">🎵</span>
        <p class="empty-title">Sube o selecciona un audio para detectar el BPM</p>
        <p class="empty-hint">El backend usa aubio para calcular el tempo real</p>
      </div>
    {/if}
  </div>
</section>

<style>
  .bpm-page {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .page-header h2 {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 700;
    color: var(--text-primary);
  }

  .header-actions {
    display: flex;
    gap: 0.5rem;
  }

  .btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.55rem 0.9rem;
    border-radius: 8px;
    border: 1px solid var(--accent);
    background: var(--accent);
    color: #fff;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn-primary:hover:not(:disabled) {
    background: var(--accent-dark);
    border-color: var(--accent-dark);
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .icon :global(svg) {
    width: 16px;
    height: 16px;
    display: block;
  }

  .file-input {
    display: none;
  }

  .page-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.75rem 1rem;
    background: rgba(244, 67, 54, 0.12);
    border: 1px solid rgba(244, 67, 54, 0.25);
    border-radius: 8px;
    color: #e57373;
    font-size: 0.85rem;
  }

  .btn-close-error {
    background: rgba(244, 67, 54, 0.15);
    border: 1px solid rgba(244, 67, 54, 0.25);
    color: #e57373;
    border-radius: 4px;
    cursor: pointer;
    line-height: 1;
    padding: 0.15rem 0.4rem;
  }

  .btn-close-error:hover {
    background: rgba(244, 67, 54, 0.25);
  }

  .bpm-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 1rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow-y: auto;
  }

  .existing-select {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .existing-select select {
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 0.85rem;
    min-width: 220px;
  }

  .bpm-loading {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.75rem;
    color: var(--text-secondary);
    font-size: 0.95rem;
  }

  .spinner {
    width: 32px;
    height: 32px;
    border: 3px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .bpm-result {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    text-align: center;
  }

  .bpm-value {
    font-size: 5rem;
    font-weight: 800;
    color: var(--accent);
    line-height: 1;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }

  .bpm-label {
    font-size: 1rem;
    color: var(--text-secondary);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .bpm-meta {
    display: flex;
    gap: 1.5rem;
    margin-top: 0.5rem;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .bpm-file {
    margin-top: 0.75rem;
    font-size: 0.85rem;
    color: var(--text-muted);
    word-break: break-all;
  }

  .bpm-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    min-height: 280px;
    border: 2px dashed var(--border);
    border-radius: 12px;
    background: var(--bg-primary);
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    text-align: center;
  }

  .bpm-empty:hover {
    border-color: var(--accent);
    background: var(--bg-hover);
  }

  .empty-icon {
    font-size: 3rem;
  }

  .empty-title {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .empty-hint {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-muted);
  }
</style>
