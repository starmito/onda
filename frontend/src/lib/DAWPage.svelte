<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import WaveSurfer from 'wavesurfer.js';
  import RegionsPlugin from 'wavesurfer.js/dist/plugins/regions.js';
  import TimelinePlugin from 'wavesurfer.js/dist/plugins/timeline.js';

  let waveformContainer: HTMLDivElement | null = $state(null);
  let fileInput: HTMLInputElement | null = $state(null);

  let ws: WaveSurfer | null = null;
  let regionsPlugin: ReturnType<typeof RegionsPlugin.create> | null = null;
  let timelinePlugin: ReturnType<typeof TimelinePlugin.create> | null = null;

  let currentFileName = $state('');
  let isReady = $state(false);
  let isPlaying = $state(false);
  let zoom = $state(100);
  let status = $state('Carga un archivo de audio para empezar');

  onMount(() => {
    createWaveSurfer();
  });

  onDestroy(() => {
    destroyWaveSurfer();
  });

  function createWaveSurfer() {
    if (!waveformContainer) return;

    regionsPlugin = RegionsPlugin.create();
    timelinePlugin = TimelinePlugin.create({
      height: 24,
      timeInterval: 1,
      primaryLabelInterval: 5,
      secondaryLabelInterval: 1,
      style: { color: 'var(--text-secondary)' } as Partial<CSSStyleDeclaration>,
    });

    ws = WaveSurfer.create({
      container: waveformContainer,
      waveColor: 'var(--accent)',
      progressColor: 'var(--accent-light)',
      cursorColor: 'var(--text-primary)',
      height: 280,
      normalize: true,
      plugins: [regionsPlugin, timelinePlugin],
    });

    ws.on('ready', () => {
      isReady = true;
      status = `${currentFileName || 'Audio'} listo · ${formatTime(ws!.getDuration())}`;
      addDefaultRegion();
      ws!.zoom(zoom);
    });

    ws.on('play', () => {
      isPlaying = true;
    });

    ws.on('pause', () => {
      isPlaying = false;
    });

    ws.on('error', (err: any) => {
      status = `Error: ${err?.message || err || 'desconocido'}`;
      isReady = false;
    });
  }

  function destroyWaveSurfer() {
    if (ws) {
      ws.destroy();
      ws = null;
    }
    regionsPlugin = null;
    timelinePlugin = null;
  }

  function addDefaultRegion() {
    if (!ws || !regionsPlugin) return;
    const duration = ws.getDuration();
    const end = Math.min(duration, Math.max(duration * 0.2, 5));
    regionsPlugin.addRegion({
      start: 0,
      end,
      color: 'rgba(108, 92, 231, 0.28)',
      borderColor: 'rgba(108, 92, 231, 0.6)',
      drag: true,
      resize: true,
    });
  }

  function handleFileSelect(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file) loadFile(file);
    input.value = '';
  }

  function loadFile(file: File) {
    if (!ws) return;
    currentFileName = file.name;
    isReady = false;
    status = 'Generando waveform...';
    ws.empty();
    ws.loadBlob(file);
  }

  function handleZoom(e: Event) {
    zoom = parseInt((e.target as HTMLInputElement).value, 10);
    if (ws && isReady) {
      ws.zoom(zoom);
    }
  }

  function togglePlay() {
    if (ws && isReady) {
      ws.playPause();
    }
  }

  function addRegion() {
    if (!ws || !regionsPlugin || !isReady) return;
    const start = ws.getCurrentTime();
    const duration = ws.getDuration();
    const end = Math.min(duration, start + 5);
    regionsPlugin.addRegion({
      start,
      end,
      color: 'rgba(108, 92, 231, 0.28)',
      borderColor: 'rgba(108, 92, 231, 0.6)',
      drag: true,
      resize: true,
    });
  }

  function clearRegions() {
    regionsPlugin?.clearRegions();
  }

  function formatTime(seconds: number): string {
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    const cs = Math.floor((seconds % 1) * 100);
    return `${m}:${s.toString().padStart(2, '0')}.${cs.toString().padStart(2, '0')}`;
  }
</script>

<div class="daw-page">
  <header class="daw-header">
    <h2>DAW — Waveform interactivo</h2>
    <span class="status">{status}</span>
  </header>

  <div class="toolbar">
    <button class="btn-primary" onclick={() => fileInput?.click()}>
      Cargar audio
    </button>
    <button class="btn" onclick={togglePlay} disabled={!isReady}>
      {isPlaying ? 'Pausa' : 'Play'}
    </button>
    <button class="btn" onclick={addRegion} disabled={!isReady}>
      Añadir región
    </button>
    <button class="btn" onclick={clearRegions} disabled={!isReady}>
      Limpiar regiones
    </button>

    <label class="zoom-control">
      Zoom
      <input
        type="range"
        min="10"
        max="1000"
        bind:value={zoom}
        oninput={handleZoom}
        disabled={!isReady}
      />
      <span class="zoom-value">{zoom}px/s</span>
    </label>
  </div>

  {#if currentFileName}
    <div class="file-tag">
      <span class="file-name">{currentFileName}</span>
    </div>
  {/if}

  <div class="waveform-wrap">
    <div bind:this={waveformContainer} class="waveform"></div>
  </div>

  <input
    bind:this={fileInput}
    type="file"
    accept="audio/*"
    onchange={handleFileSelect}
    class="file-input"
  />
</div>

<style>
  .daw-page {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .daw-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .daw-header h2 {
    margin: 0;
    font-size: 1.2rem;
    color: var(--text-primary);
  }

  .status {
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.6rem;
  }

  .btn,
  .btn-primary {
    padding: 0.5rem 0.9rem;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn:hover:not(:disabled),
  .btn-primary:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: var(--accent);
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .btn-primary:hover:not(:disabled) {
    background: var(--accent-dark);
  }

  .zoom-control {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--text-secondary);
    font-size: 0.8rem;
    font-weight: 600;
    margin-left: auto;
  }

  .zoom-control input[type='range'] {
    width: 180px;
    accent-color: var(--accent);
  }

  .zoom-value {
    min-width: 3.5rem;
    text-align: right;
    color: var(--text-primary);
  }

  .file-tag {
    display: inline-flex;
  }

  .file-name {
    font-size: 0.8rem;
    color: var(--accent-light);
    background: var(--accent-bg);
    border: 1px solid var(--accent-border);
    padding: 0.25rem 0.6rem;
    border-radius: 6px;
  }

  .waveform-wrap {
    flex: 1;
    min-height: 320px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1rem;
    overflow: hidden;
  }

  .waveform {
    width: 100%;
    height: 100%;
  }

  .file-input {
    display: none;
  }
</style>
