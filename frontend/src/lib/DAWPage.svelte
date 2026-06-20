<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import WaveSurfer from 'wavesurfer.js';
  import RegionsPlugin from 'wavesurfer.js/dist/plugins/regions.js';
  import TimelinePlugin from 'wavesurfer.js/dist/plugins/timeline.js';
  import { fadeAudio, exportAudio, trimAudio } from './api';

  type RegionLike = { start: number; end: number };
  type DAWState = {
    fileName: string;
    source: string;
    regions: RegionLike[];
  };

  let waveformContainer: HTMLDivElement | null = $state(null);
  let fileInput: HTMLInputElement | null = $state(null);
  let formatSelect: HTMLSelectElement | null = $state(null);

  let ws: WaveSurfer | null = null;
  let regionsPlugin: ReturnType<typeof RegionsPlugin.create> | null = null;
  let timelinePlugin: ReturnType<typeof TimelinePlugin.create> | null = null;

  let currentFileName = $state('');
  let currentSource = $state('');
  let isReady = $state(false);
  let isPlaying = $state(false);
  let zoom = $state(100);
  let status = $state('Carga un archivo de audio para empezar');
  let isProcessing = $state(false);

  let history: DAWState[] = $state([]);
  let historyIndex = $state(-1);

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

    if (regionsPlugin) {
      regionsPlugin.on('region-created', () => {
        // Ensure at least the default region exists; user can still create more.
      });
    }
  }

  function destroyWaveSurfer() {
    if (ws) {
      ws.destroy();
      ws = null;
    }
    regionsPlugin = null;
    timelinePlugin = null;
  }

  function getCurrentRegions(): RegionLike[] {
    if (!regionsPlugin) return [];
    return regionsPlugin.getRegions().map((r) => ({ start: r.start, end: r.end }));
  }

  function setRegions(regions: RegionLike[]) {
    if (!regionsPlugin || !ws) return;
    regionsPlugin.clearRegions();
    for (const r of regions) {
      regionsPlugin.addRegion({
        start: r.start,
        end: r.end,
        color: 'rgba(108, 92, 231, 0.28)',
        borderColor: 'rgba(108, 92, 231, 0.6)',
        drag: true,
        resize: true,
      });
    }
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
    const source = URL.createObjectURL(file);
    loadSource(file.name, source, true);
  }

  function loadSource(fileName: string, source: string, addRegionFlag: boolean) {
    if (!ws) return;
    currentFileName = fileName;
    currentSource = source;
    isReady = false;
    status = 'Generando waveform...';
    ws.empty();
    ws.load(source);
    if (addRegionFlag) {
      // Defer default region until ready, then store initial history state.
      const once = () => {
        addDefaultRegion();
        pushHistory(fileName, source, getCurrentRegions());
        ws?.un('ready', once);
      };
      ws.on('ready', once);
    }
  }

  function pushHistory(fileName: string, source: string, regions: RegionLike[]) {
    if (historyIndex < history.length - 1) {
      history = history.slice(0, historyIndex + 1);
    }
    history = [...history, { fileName, source, regions }];
    historyIndex = history.length - 1;
  }

  function restoreHistory(index: number) {
    const state = history[index];
    if (!state || !ws) return;
    historyIndex = index;
    currentFileName = state.fileName;
    currentSource = state.source;
    isReady = false;
    status = 'Generando waveform...';
    ws.empty();
    ws.load(state.source);
    const once = () => {
      setRegions(state.regions);
      ws?.zoom(zoom);
      status = `${currentFileName || 'Audio'} listo · ${formatTime(ws!.getDuration())}`;
      ws?.un('ready', once);
    };
    ws.on('ready', once);
  }

  function undo() {
    if (historyIndex > 0) {
      restoreHistory(historyIndex - 1);
    }
  }

  function redo() {
    if (historyIndex >= 0 && historyIndex < history.length - 1) {
      restoreHistory(historyIndex + 1);
    }
  }

  function getSelectedRegion() {
    if (!regionsPlugin) return null;
    const regions = regionsPlugin.getRegions();
    return regions[0] ?? null;
  }

  function requireRegion(): { start: number; end: number } | null {
    const region = getSelectedRegion();
    if (!region) {
      status = 'Selecciona un rango en el waveform';
      return null;
    }
    return { start: region.start, end: region.end };
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

  async function handleTrim() {
    const region = requireRegion();
    if (!region || !currentFileName) return;
    isProcessing = true;
    status = 'Recortando...';
    try {
      const resp = await trimAudio(currentFileName, region.start, region.end);
      const source = `/daw-data/${resp.file}`;
      const newRegions: RegionLike[] = [{ start: 0, end: region.end - region.start }];
      pushHistory(resp.file, source, newRegions);
      loadSource(resp.file, source, false);
      setTimeout(() => setRegions(newRegions), 0);
      status = `Recortado: ${resp.file}`;
    } catch (err) {
      status = `Error al recortar: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  async function handleFade(type: 'in' | 'out') {
    const region = requireRegion();
    if (!region || !currentFileName) return;
    isProcessing = true;
    status = `Aplicando fade ${type}...`;
    try {
      const duration = region.end - region.start;
      const resp = await fadeAudio(currentFileName, type, region.start, duration);
      const source = `/daw-data/${resp.file}`;
      const newRegions: RegionLike[] = [{ start: region.start, end: region.end }];
      pushHistory(resp.file, source, newRegions);
      loadSource(resp.file, source, false);
      setTimeout(() => setRegions(newRegions), 0);
      status = `Fade ${type}: ${resp.file}`;
    } catch (err) {
      status = `Error al aplicar fade: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  async function handleExport() {
    if (!currentFileName) return;
    isProcessing = true;
    status = 'Exportando...';
    const format = formatSelect?.value || 'wav';
    try {
      const resp = await exportAudio(currentFileName, format);
      status = `Exportado: ${resp.file} (${resp.format}, ${formatBytes(resp.size)})`;
    } catch (err) {
      status = `Error al exportar: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  function formatTime(seconds: number): string {
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    const cs = Math.floor((seconds % 1) * 100);
    return `${m}:${s.toString().padStart(2, '0')}.${cs.toString().padStart(2, '0')}`;
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
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

    <div class="toolbar-group">
      <button class="btn" onclick={handleTrim} disabled={!isReady || isProcessing}>
        Trim
      </button>
      <button class="btn" onclick={() => handleFade('in')} disabled={!isReady || isProcessing}>
        Fade In
      </button>
      <button class="btn" onclick={() => handleFade('out')} disabled={!isReady || isProcessing}>
        Fade Out
      </button>
    </div>

    <div class="toolbar-group export-group">
      <select class="format-select" bind:this={formatSelect} disabled={!isReady || isProcessing}>
        <option value="wav">WAV</option>
      </select>
      <button class="btn" onclick={handleExport} disabled={!isReady || isProcessing}>
        Export
      </button>
    </div>

    <div class="toolbar-group history-group">
      <button class="btn" onclick={undo} disabled={historyIndex <= 0 || isProcessing}>
        Undo
      </button>
      <button class="btn" onclick={redo} disabled={historyIndex < 0 || historyIndex >= history.length - 1 || isProcessing}>
        Redo
      </button>
    </div>

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

  .toolbar-group {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding-left: 0.6rem;
    border-left: 1px solid var(--border);
  }

  .export-group {
    gap: 0.3rem;
  }

  .format-select {
    padding: 0.45rem 0.5rem;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.85rem;
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
