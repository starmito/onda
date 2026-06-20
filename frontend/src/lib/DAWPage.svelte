<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import WaveSurfer from 'wavesurfer.js';
  import RegionsPlugin from 'wavesurfer.js/dist/plugins/regions.js';
  import TimelinePlugin from 'wavesurfer.js/dist/plugins/timeline.js';
  import {
    fadeAudio,
    exportAudio,
    trimAudio,
    listStems,
    importStem,
    uploadAudioDAW,
    getTempoGrid,
  } from './api';
  import type { TempoGridResponse } from './api';

  type RegionLike = { start: number; end: number };
  type DAWState = {
    fileName: string;
    source: string;
    regions: RegionLike[];
  };

  type Track = {
    id: string;
    fileName: string;
    name: string;
    source: string;
    size: number;
    ws: WaveSurfer | null;
    regionsPlugin: ReturnType<typeof RegionsPlugin.create> | null;
    timelinePlugin: ReturnType<typeof TimelinePlugin.create> | null;
    grid: TempoGridResponse | null;
    gridCanvas: HTMLCanvasElement | null;
    isReady: boolean;
    isPlaying: boolean;
    volume: number;
    muted: boolean;
    solo: boolean;
  };

  let formatSelect: HTMLSelectElement | null = $state(null);
  let fileInput: HTMLInputElement | null = $state(null);
  let importUploadInput: HTMLInputElement | null = $state(null);

  let tracks: Track[] = $state([]);
  let activeTrackId = $state<string>('');
  let trackContainers: Record<string, HTMLDivElement> = $state({});

  let importOpen = $state(false);
  let importTab = $state<'upload' | 'output' | 'pitch'>('upload');
  let stemsData = $state<{ output: Record<string, string[]>; pitch: string[] }>({
    output: {},
    pitch: [],
  });
  let stemsLoading = $state(false);
  let expandedSongs = $state<Record<string, boolean>>({});
  let uploadResult = $state<{ file: string; size: number } | null>(null);

  let zoom = $state(100);
  let status = $state('Carga o importa pistas de audio para empezar');
  let isProcessing = $state(false);
  let syncSeek = false;

  let history: DAWState[] = $state([]);
  let historyIndex = $state(-1);

  const isReady = $derived(tracks.some((t) => t.isReady));
  const isPlaying = $derived(tracks.some((t) => t.isPlaying));

  $effect(() => {
    for (const track of tracks) {
      const container = trackContainers[track.id];
      if (container && !track.ws) {
        initTrackWaveSurfer(track, container);
      }
    }
  });

  onDestroy(() => {
    for (const track of tracks) {
      destroyTrack(track);
    }
  });

  function destroyTrack(track: Track) {
    if (track.gridCanvas && track.gridCanvas.parentElement) {
      track.gridCanvas.remove();
    }
    track.gridCanvas = null;
    if (track.ws) {
      track.ws.destroy();
      track.ws = null;
    }
    track.regionsPlugin = null;
    track.timelinePlugin = null;
    track.grid = null;
  }

  async function loadTempoGrid(fileName: string, trackId: string) {
    try {
      const data = await getTempoGrid(fileName);
      const track = tracks.find((t) => t.id === trackId);
      if (!track) return;
      track.grid = data;
      if (track.ws && track.isReady) {
        setupGridOverlay(track);
        drawGrid(track);
      }
    } catch {
      // Grid data is optional; ignore errors silently.
    }
  }

  function setupGridOverlay(track: Track) {
    if (!track.ws || !track.grid) return;
    const wrapper = track.ws.getWrapper();
    if (!wrapper) return;
    if (!track.gridCanvas) {
      const canvas = document.createElement('canvas');
      canvas.className = 'tempo-grid-overlay';
      canvas.style.position = 'absolute';
      canvas.style.top = '0';
      canvas.style.left = '0';
      canvas.style.pointerEvents = 'none';
      canvas.style.zIndex = '10';
      if (!wrapper.style.position) {
        wrapper.style.position = 'relative';
      }
      wrapper.appendChild(canvas);
      track.gridCanvas = canvas;
    }
  }

  function drawGrid(track: Track) {
    if (!track.ws || !track.grid || !track.gridCanvas) return;
    const ws = track.ws;
    const grid = track.grid;
    const canvas = track.gridCanvas;
    requestAnimationFrame(() => {
      const wrapper = ws.getWrapper();
      if (!wrapper || !canvas.isConnected) return;
      const duration = ws.getDuration();
      const width = duration * zoom;
      const height = wrapper.clientHeight;
      if (width <= 0 || height <= 0) return;
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.ceil(width * dpr));
      canvas.height = Math.max(1, Math.ceil(height * dpr));
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, width, height);
      // Beat lines
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.22)';
      ctx.lineWidth = 1;
      for (const t of grid.beats) {
        const x = t * zoom;
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, height);
        ctx.stroke();
      }
      // Bar lines and labels
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.55)';
      ctx.lineWidth = 2;
      ctx.fillStyle = 'rgba(255, 255, 255, 0.75)';
      ctx.font = '11px sans-serif';
      for (const bar of grid.bars) {
        const x = bar.start * zoom;
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, height);
        ctx.stroke();
        ctx.fillText(String(bar.bar), x + 4, 14);
      }
    });
  }

  function generateId(): string {
    return `${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
  }

  function trackContainer(node: HTMLDivElement, id: string) {
    trackContainers[id] = node;
    return {
      destroy() {
        delete trackContainers[id];
      },
    };
  }

  function getActiveTrack(): Track | undefined {
    return tracks.find((t) => t.id === activeTrackId) ?? tracks[0];
  }

  function initTrackWaveSurfer(track: Track, container: HTMLDivElement) {
    const regionsPlugin = RegionsPlugin.create();
    const timelinePlugin = TimelinePlugin.create({
      height: 20,
      timeInterval: 1,
      primaryLabelInterval: 5,
      secondaryLabelInterval: 1,
      style: { color: 'var(--text-secondary)' } as Partial<CSSStyleDeclaration>,
    });

    const ws = WaveSurfer.create({
      container,
      waveColor: 'var(--accent)',
      progressColor: 'var(--accent-light)',
      cursorColor: 'var(--text-primary)',
      height: 120,
      normalize: true,
      plugins: [regionsPlugin, timelinePlugin],
    });

    ws.on('ready', () => {
      track.isReady = true;
      ws.zoom(zoom);
      updateStatus();
      applyTrackAudioState(track);
      if (track.grid) {
        setupGridOverlay(track);
        drawGrid(track);
      }
    });

    ws.on('zoom', () => drawGrid(track));
    ws.on('redraw', () => drawGrid(track));

    ws.on('play', () => {
      track.isPlaying = true;
      updateStatus();
    });

    ws.on('pause', () => {
      track.isPlaying = false;
      updateStatus();
    });

    ws.on('error', (err: any) => {
      status = `Error: ${err?.message || err || 'desconocido'}`;
      track.isReady = false;
    });

    ws.on('seek', () => {
      if (syncSeek) return;
      syncSeek = true;
      const time = ws.getCurrentTime();
      for (const t of tracks) {
        if (t.id !== track.id && t.ws && t.isReady) {
          t.ws.setTime(time);
        }
      }
      syncSeek = false;
    });

    ws.load(track.source);
    track.ws = ws;
    track.regionsPlugin = regionsPlugin;
    track.timelinePlugin = timelinePlugin;
  }

  function updateStatus() {
    const active = getActiveTrack();
    if (!active) {
      status = 'Carga o importa pistas de audio para empezar';
      return;
    }
    if (!active.isReady) {
      status = 'Generando waveform...';
      return;
    }
    const duration = active.ws?.getDuration() ?? 0;
    status = `${active.name} listo · ${formatTime(duration)} · ${tracks.length} pista${tracks.length === 1 ? '' : 's'}`;
  }

  function addTrack(fileName: string, source: string, size: number): Track {
    const id = generateId();
    const track: Track = {
      id,
      fileName,
      name: fileName,
      source,
      size,
      ws: null,
      regionsPlugin: null,
      timelinePlugin: null,
      grid: null,
      gridCanvas: null,
      isReady: false,
      isPlaying: false,
      volume: 1,
      muted: false,
      solo: false,
    };
    tracks = [...tracks, track];
    activeTrackId = id;
    updateStatus();
    return track;
  }

  function handleFileSelect(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file) {
      const track = addTrack(file.name, URL.createObjectURL(file), file.size);
      loadTempoGrid(file.name, track.id);
    }
    input.value = '';
  }

  function getTrackRegions(track: Track): RegionLike[] {
    if (!track.regionsPlugin) return [];
    return track.regionsPlugin.getRegions().map((r) => ({ start: r.start, end: r.end }));
  }

  function setTrackRegions(track: Track, regions: RegionLike[]) {
    if (!track.regionsPlugin || !track.ws) return;
    track.regionsPlugin.clearRegions();
    for (const r of regions) {
      track.regionsPlugin.addRegion({
        start: r.start,
        end: r.end,
        color: 'rgba(108, 92, 231, 0.28)',
        borderColor: 'rgba(108, 92, 231, 0.6)',
        drag: true,
        resize: true,
      });
    }
  }

  function addDefaultRegion(track: Track) {
    if (!track.ws || !track.regionsPlugin) return;
    const duration = track.ws.getDuration();
    const end = Math.min(duration, Math.max(duration * 0.2, 5));
    track.regionsPlugin.addRegion({
      start: 0,
      end,
      color: 'rgba(108, 92, 231, 0.28)',
      borderColor: 'rgba(108, 92, 231, 0.6)',
      drag: true,
      resize: true,
    });
  }

  function loadSource(track: Track, fileName: string, source: string, addRegionFlag: boolean) {
    if (!track.ws) return;
    track.fileName = fileName;
    track.name = fileName;
    track.source = source;
    track.isReady = false;
    status = 'Generando waveform...';
    track.ws.empty();
    track.ws.load(source);
    if (addRegionFlag) {
      const once = () => {
        addDefaultRegion(track);
        pushHistory(fileName, source, getTrackRegions(track));
        track.ws?.un('ready', once);
      };
      track.ws.on('ready', once);
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
    const track = getActiveTrack();
    if (!state || !track || !track.ws) return;
    historyIndex = index;
    loadSource(track, state.fileName, state.source, false);
    const once = () => {
      setTrackRegions(track, state.regions);
      track.ws?.zoom(zoom);
      updateStatus();
      track.ws?.un('ready', once);
    };
    track.ws.on('ready', once);
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

  function getSelectedRegion(track: Track) {
    if (!track.regionsPlugin) return null;
    const regions = track.regionsPlugin.getRegions();
    return regions[0] ?? null;
  }

  function requireRegion(): { start: number; end: number } | null {
    const track = getActiveTrack();
    if (!track || !track.regionsPlugin) {
      status = 'Selecciona una pista activa';
      return null;
    }
    const region = getSelectedRegion(track);
    if (!region) {
      status = 'Selecciona un rango en el waveform';
      return null;
    }
    return { start: region.start, end: region.end };
  }

  function handleZoom(e: Event) {
    zoom = parseInt((e.target as HTMLInputElement).value, 10);
    for (const t of tracks) {
      if (t.ws && t.isReady) {
        t.ws.zoom(zoom);
        drawGrid(t);
      }
    }
  }

  function togglePlay() {
    if (!isReady) return;
    const anyPlaying = tracks.some((t) => t.isPlaying);
    for (const t of tracks) {
      if (!t.ws || !t.isReady) continue;
      if (anyPlaying) t.ws.pause();
      else t.ws.play();
    }
  }

  function toggleTrackPlay(track: Track) {
    if (!track.ws || !track.isReady) return;
    track.ws.playPause();
  }

  function setTrackVolume(track: Track, vol: number) {
    track.volume = vol;
    applyTrackAudioState(track);
  }

  function toggleMute(track: Track) {
    track.muted = !track.muted;
    applyTrackAudioState(track);
  }

  function toggleSolo(track: Track) {
    track.solo = !track.solo;
    for (const t of tracks) applyTrackAudioState(t);
  }

  function applyTrackAudioState(track: Track) {
    if (!track.ws) return;
    const anySolo = tracks.some((t) => t.solo);
    const shouldMute = track.muted || (anySolo && !track.solo);
    track.ws.setVolume(shouldMute ? 0 : track.volume);
  }

  function addRegion() {
    const track = getActiveTrack();
    if (!track || !track.ws || !track.regionsPlugin || !track.isReady) return;
    const start = track.ws.getCurrentTime();
    const duration = track.ws.getDuration();
    const end = Math.min(duration, start + 5);
    track.regionsPlugin.addRegion({
      start,
      end,
      color: 'rgba(108, 92, 231, 0.28)',
      borderColor: 'rgba(108, 92, 231, 0.6)',
      drag: true,
      resize: true,
    });
  }

  function clearRegions() {
    const track = getActiveTrack();
    track?.regionsPlugin?.clearRegions();
  }

  async function handleTrim() {
    const region = requireRegion();
    const track = getActiveTrack();
    if (!region || !track || !track.fileName) return;
    isProcessing = true;
    status = 'Recortando...';
    try {
      const resp = await trimAudio(track.fileName, region.start, region.end);
      const source = `/daw-data/${resp.file}`;
      const newRegions: RegionLike[] = [{ start: 0, end: region.end - region.start }];
      pushHistory(resp.file, source, newRegions);
      loadSource(track, resp.file, source, false);
      setTimeout(() => setTrackRegions(track, newRegions), 0);
      status = `Recortado: ${resp.file}`;
    } catch (err) {
      status = `Error al recortar: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  async function handleFade(type: 'in' | 'out') {
    const region = requireRegion();
    const track = getActiveTrack();
    if (!region || !track || !track.fileName) return;
    isProcessing = true;
    status = `Aplicando fade ${type}...`;
    try {
      const duration = region.end - region.start;
      const resp = await fadeAudio(track.fileName, type, region.start, duration);
      const source = `/daw-data/${resp.file}`;
      const newRegions: RegionLike[] = [{ start: region.start, end: region.end }];
      pushHistory(resp.file, source, newRegions);
      loadSource(track, resp.file, source, false);
      setTimeout(() => setTrackRegions(track, newRegions), 0);
      status = `Fade ${type}: ${resp.file}`;
    } catch (err) {
      status = `Error al aplicar fade: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  async function handleExport() {
    const track = getActiveTrack();
    if (!track || !track.fileName) return;
    isProcessing = true;
    status = 'Exportando...';
    const format = formatSelect?.value || 'wav';
    try {
      const resp = await exportAudio(track.fileName, format);
      status = `Exportado: ${resp.file} (${resp.format}, ${formatBytes(resp.size)})`;
    } catch (err) {
      status = `Error al exportar: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  async function loadStems() {
    stemsLoading = true;
    try {
      stemsData = await listStems();
    } catch (err) {
      status = `Error al cargar stems: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      stemsLoading = false;
    }
  }

  function openImportTab(tab: 'upload' | 'output' | 'pitch') {
    importTab = tab;
    if (tab !== 'upload') loadStems();
  }

  async function handleImportUpload(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    isProcessing = true;
    status = 'Subiendo...';
    try {
      const resp = await uploadAudioDAW(file);
      uploadResult = { file: resp.file, size: resp.size };
      const track = addTrack(resp.file, `/daw-data/${resp.file}`, resp.size);
      loadTempoGrid(resp.file, track.id);
      status = `Subido: ${resp.file}`;
    } catch (err) {
      status = `Error al subir: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
      input.value = '';
    }
  }

  async function handleImportOutput(song: string, stem: string) {
    isProcessing = true;
    status = 'Importando...';
    try {
      const resp = await importStem('output', song, stem);
      const track = addTrack(resp.file, `/daw-data/${resp.file}`, resp.size);
      loadTempoGrid(resp.file, track.id);
      status = `Importado: ${resp.file}`;
    } catch (err) {
      status = `Error al importar: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  async function handleImportPitch(stem: string) {
    isProcessing = true;
    status = 'Importando pitch...';
    try {
      const resp = await importStem('pitch', undefined, stem);
      const track = addTrack(resp.file, `/daw-data/${resp.file}`, resp.size);
      loadTempoGrid(resp.file, track.id);
      status = `Importado: ${resp.file}`;
    } catch (err) {
      status = `Error al importar: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      isProcessing = false;
    }
  }

  function toggleExpandedSong(song: string) {
    expandedSongs[song] = !expandedSongs[song];
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

  onMount(() => {
    // DAW starts empty; tracks are added via load/import.
  });
</script>

<div class="daw-page" class:import-open={importOpen}>
  <div class="daw-main">
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

      <button class="btn import-btn" onclick={() => importOpen = !importOpen}>
        {importOpen ? 'Cerrar importar' : 'Importar'}
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

    <div class="tracks-wrap">
      {#if tracks.length === 0}
        <div class="empty-state">
          <p>No hay pistas cargadas.</p>
          <p>Usa "Cargar audio" o "Importar" para añadir pistas.</p>
        </div>
      {:else}
        {#each tracks as track (track.id)}
          <div
            class="track"
            class:active={track.id === activeTrackId}
            onclick={() => (activeTrackId = track.id)}
            role="button"
            tabindex="0"
          >
            <div class="track-header">
              <div class="track-info">
                <span class="track-name" title={track.name}>{track.name}</span>
                <span class="track-size">{formatBytes(track.size)}</span>
              </div>
              <div class="track-controls">
                <button
                  class="btn-small"
                  onclick={(e) => { e.stopPropagation(); toggleTrackPlay(track); }}
                  disabled={!track.isReady}
                >
                  {track.isPlaying ? 'Pausa' : 'Play'}
                </button>
                <button
                  class="btn-small"
                  class:active={track.solo}
                  onclick={(e) => { e.stopPropagation(); toggleSolo(track); }}
                >
                  Solo
                </button>
                <button
                  class="btn-small"
                  class:active={track.muted}
                  onclick={(e) => { e.stopPropagation(); toggleMute(track); }}
                >
                  Mute
                </button>
                <label class="volume-control">
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.01"
                    value={track.volume}
                    oninput={(e) => { e.stopPropagation(); setTrackVolume(track, parseFloat(e.currentTarget.value)); }}
                  />
                </label>
              </div>
            </div>
            <div class="track-waveform" use:trackContainer={track.id}></div>
          </div>
        {/each}
      {/if}
    </div>
  </div>

  {#if importOpen}
    <div class="import-panel">
      <div class="import-header">
        <h3>Importar pistas</h3>
        <button class="btn" onclick={() => (importOpen = false)}>Cerrar</button>
      </div>

      <div class="import-tabs">
        <button
          class="import-tab"
          class:active={importTab === 'upload'}
          onclick={() => openImportTab('upload')}
        >
          Subir desde PC
        </button>
        <button
          class="import-tab"
          class:active={importTab === 'output'}
          onclick={() => openImportTab('output')}
        >
          Stems de Onda
        </button>
        <button
          class="import-tab"
          class:active={importTab === 'pitch'}
          onclick={() => openImportTab('pitch')}
        >
          Pitch shift
        </button>
      </div>

      <div class="import-body">
        {#if importTab === 'upload'}
          <div class="import-section">
            <input
              bind:this={importUploadInput}
              type="file"
              accept=".wav,.mp3,.flac,.ogg,.m4a,.aiff"
              onchange={handleImportUpload}
              class="file-input"
            />
            <button class="btn-primary" onclick={() => importUploadInput?.click()}>
              Seleccionar archivo
            </button>
            {#if uploadResult}
              <div class="upload-result">
                <span class="upload-name">{uploadResult.file}</span>
                <span class="upload-size">{formatBytes(uploadResult.size)}</span>
              </div>
            {/if}
          </div>
        {:else if importTab === 'output'}
          <div class="import-section">
            {#if stemsLoading}
              <div class="loading">Cargando stems...</div>
            {:else if Object.keys(stemsData.output).length === 0}
              <div class="empty">No hay stems disponibles en output.</div>
            {:else}
              {#each Object.entries(stemsData.output) as [song, stems]}
                <div class="song-item">
                  <button
                    class="song-toggle"
                    onclick={() => toggleExpandedSong(song)}
                  >
                    <span class="toggle-icon">{expandedSongs[song] ? '▼' : '▶'}</span>
                    <span class="song-name">{song}</span>
                  </button>
                  {#if expandedSongs[song]}
                    <div class="stem-list">
                      {#each stems as stem}
                        <div class="stem-item">
                          <span class="stem-name">{stem}</span>
                          <button
                            class="btn-small"
                            onclick={() => handleImportOutput(song, stem)}
                            disabled={isProcessing}
                          >
                            Importar
                          </button>
                        </div>
                      {/each}
                    </div>
                  {/if}
                </div>
              {/each}
            {/if}
          </div>
        {:else if importTab === 'pitch'}
          <div class="import-section">
            {#if stemsLoading}
              <div class="loading">Cargando stems...</div>
            {:else if stemsData.pitch.length === 0}
              <div class="empty">No hay archivos de pitch disponibles.</div>
            {:else}
              {#each stemsData.pitch as stem}
                <div class="stem-item">
                  <span class="stem-name">{stem}</span>
                  <button
                    class="btn-small"
                    onclick={() => handleImportPitch(stem)}
                    disabled={isProcessing}
                  >
                    Importar
                  </button>
                </div>
              {/each}
            {/if}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<input
  bind:this={fileInput}
  type="file"
  accept="audio/*"
  onchange={handleFileSelect}
  class="file-input"
/>

<style>
  .daw-page {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .daw-page.import-open {
    flex-direction: row;
    gap: 0;
  }

  .daw-main {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    flex: 1;
    min-width: 0;
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
  .btn-primary,
  .btn-small {
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
  .btn-primary:hover:not(:disabled),
  .btn-small:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: var(--accent);
  }

  .btn:disabled,
  .btn-primary:disabled,
  .btn-small:disabled {
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

  .btn-small {
    padding: 0.35rem 0.6rem;
    font-size: 0.75rem;
  }

  .btn-small.active {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .import-btn {
    margin-left: auto;
  }

  .zoom-control {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--text-secondary);
    font-size: 0.8rem;
    font-weight: 600;
  }

  .zoom-control input[type='range'] {
    width: 140px;
    accent-color: var(--accent);
  }

  .zoom-value {
    min-width: 3.5rem;
    text-align: right;
    color: var(--text-primary);
  }

  .tracks-wrap {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    flex: 1;
    color: var(--text-secondary);
    text-align: center;
  }

  .track {
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 0.8rem;
    background: var(--bg);
    transition: border-color 0.15s;
    cursor: pointer;
  }

  .track.active {
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  .track-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 0.6rem;
    flex-wrap: wrap;
  }

  .track-info {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    min-width: 0;
  }

  .track-name {
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 260px;
  }

  .track-size {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .track-controls {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .volume-control input[type='range'] {
    width: 80px;
    accent-color: var(--accent);
  }

  .track-waveform {
    width: 100%;
    height: 140px;
    background: var(--bg-surface);
    border-radius: 8px;
    overflow: hidden;
  }

  .file-input {
    display: none;
  }

  .import-panel {
    width: 360px;
    flex-shrink: 0;
    border-left: 1px solid var(--border);
    background: var(--bg-surface);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .import-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 1rem;
    border-bottom: 1px solid var(--border);
  }

  .import-header h3 {
    margin: 0;
    font-size: 1rem;
    color: var(--text-primary);
  }

  .import-tabs {
    display: flex;
    border-bottom: 1px solid var(--border);
  }

  .import-tab {
    flex: 1;
    padding: 0.7rem 0.4rem;
    background: var(--bg);
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-secondary);
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
  }

  .import-tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  .import-body {
    flex: 1;
    overflow-y: auto;
    padding: 1rem;
  }

  .import-section {
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
  }

  .upload-result {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.6rem;
    background: var(--accent-bg);
    border: 1px solid var(--accent-border);
    border-radius: 8px;
  }

  .upload-name {
    font-size: 0.85rem;
    color: var(--text-primary);
    word-break: break-all;
  }

  .upload-size {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .loading,
  .empty {
    padding: 1rem;
    text-align: center;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .song-item {
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
  }

  .song-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.6rem 0.8rem;
    background: var(--bg);
    border: none;
    color: var(--text-primary);
    font-weight: 600;
    cursor: pointer;
    text-align: left;
  }

  .toggle-icon {
    font-size: 0.7rem;
    color: var(--text-secondary);
  }

  .song-name {
    flex: 1;
  }

  .stem-list {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    padding: 0.4rem;
    background: var(--bg-surface);
  }

  .stem-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    background: var(--bg);
  }

  .stem-name {
    font-size: 0.85rem;
    color: var(--text-primary);
    word-break: break-all;
  }

  :global(.tempo-grid-overlay) {
    pointer-events: none;
  }
</style>
