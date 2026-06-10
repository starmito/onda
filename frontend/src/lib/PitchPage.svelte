<script lang="ts">
  import { onMount } from 'svelte';
  import ResultsPanel from './ResultsPanel.svelte';
  import type { ResultStem } from './types';
  import { uploadPitchAudio } from './api';
  import { IconUpload } from './icons';

  let { results = [] as ResultStem[], onResultsChange = () => {} } = $props();

  interface PitchFile {
    file: File;
    id: string;
    name: string;
    status: 'uploading' | 'ready' | 'error';
    errorMsg?: string;
    pitch: number;
  }

  let pitchFiles = $state<PitchFile[]>([]);
  let dragCounter = $state(0);

  function handleDropZoneFile(f: File) {
    const id = crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
    const pf: PitchFile = { file: f, id, name: f.name, status: 'uploading', pitch: 0 };
    pitchFiles = [...pitchFiles, pf];

    // Upload to regular endpoint — could be changed to input_rubberband later
    uploadPitchAudio(f).then(() => {
      pitchFiles = pitchFiles.map(p => p.id === id ? { ...p, status: 'ready' } : p);
    }).catch((err) => {
      pitchFiles = pitchFiles.map(p => p.id === id ? { ...p, status: 'error', errorMsg: err.message } : p);
    });
  }

  function handleDrop() { dragCounter = 0; }
  function handleDragOver(e: DragEvent) { e.preventDefault(); }
  function handleDropEvent(e: DragEvent) {
    e.preventDefault();
    dragCounter = 0;
    const files = e.dataTransfer?.files;
    if (files) { for (let i = 0; i < files.length; i++) handleDropZoneFile(files[i]); }
  }
  function handleClick() {
    const input = document.getElementById('pitch-dropzone-input') as HTMLInputElement;
    input?.click();
  }
  function handleInput(e: Event) {
    const input = e.target as HTMLInputElement;
    if (input.files) {
      for (let i = 0; i < input.files.length; i++) handleDropZoneFile(input.files[i]);
      input.value = '';
    }
  }
</script>

<div class="pitch-page">
  <!-- Top: existing pipeline results -->
  <section class="pitch-results-section">
    <h3 class="section-title">Resultados</h3>
    <ResultsPanel
      files={results}
      onstemdeleted={() => {}}
      ongroupdeleted={() => {}}
    />
  </section>

  <!-- Bottom: independent pitch dropzone -->
  <section class="pitch-dropzone-section">
    <h3 class="section-title">Subir audio para cambio de tono</h3>
    <p class="section-desc">Los archivos se guardan en la carpeta input_rubberband</p>

    <div
      class="pitch-dropzone"
      ondragover={handleDragOver}
      ondrop={handleDropEvent}
      onclick={handleClick}
      role="button"
      tabindex="0"
    >
      <span class="pitch-dropzone-icon">{@html IconUpload}</span>
      <span class="pitch-dropzone-text">Arrastra archivos aquí o haz clic</span>
      <span class="pitch-dropzone-hint">WAV, MP3, FLAC, OGG, M4A</span>
    </div>
    <input id="pitch-dropzone-input" type="file" hidden accept="audio/*" multiple onchange={handleInput} />

    {#if pitchFiles.length > 0}
      <div class="pitch-file-list">
        <h4 class="list-title">Archivos subidos ({pitchFiles.length})</h4>
        {#each pitchFiles as pf (pf.id)}
          <div class="pitch-file-row">
            <span class="pitch-file-name">{pf.name}</span>
            <span class="pitch-file-status" class:ready={pf.status === 'ready'} class:error={pf.status === 'error'}>
              {pf.status === 'uploading' ? 'Subiendo...' : pf.status === 'error' ? `Error: ${pf.errorMsg}` : '✅ Listo'}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </section>
</div>

<style>
  .pitch-page {
    width: 100%;
    max-width: 900px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 2rem;
    padding: 1rem;
  }

  .section-title {
    margin: 0 0 0.5rem 0;
    font-size: 1rem;
    font-weight: 700;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .section-desc {
    margin: 0 0 1rem 0;
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .pitch-results-section {
    width: 100%;
  }

  .pitch-dropzone-section {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 20px;
  }

  .pitch-dropzone {
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
  .pitch-dropzone:hover {
    border-color: var(--accent);
    background: var(--bg-hover);
  }
  .pitch-dropzone-icon { font-size: 2rem; }
  .pitch-dropzone-text { font-size: 0.95rem; font-weight: 600; color: var(--text-primary); }
  .pitch-dropzone-hint { font-size: 0.75rem; color: var(--text-muted); }

  .pitch-file-list {
    margin-top: 1rem;
  }
  .list-title {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0 0 0.5rem 0;
  }
  .pitch-file-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.5rem 0.75rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 8px;
    margin-bottom: 0.3rem;
  }
  .pitch-file-name { font-size: 0.85rem; color: var(--text-primary); }
  .pitch-file-status { font-size: 0.75rem; color: var(--text-secondary); }
  .pitch-file-status.ready { color: #81c784; }
  .pitch-file-status.error { color: #e57373; }
</style>
