<script lang="ts">
  import { onDestroy } from 'svelte';
  import { uploadPitchAudio, pitchInputDownloadUrl, deletePitchUpload } from './api';
  import { IconUpload } from './icons';

  // Each uploaded file becomes a standalone player
  interface PitchPlayer {
    id: string;
    name: string;
    status: 'uploading' | 'ready' | 'error';
    errorMsg?: string;
    // Audio state
    audioCtx: AudioContext | null;
    playing: boolean;
    paused: boolean;
    currentTime: number;
    duration: number;
    seekValue: number;
    sourceNode: AudioBufferSourceNode | null;
    gainNode: GainNode | null;
    buffer: AudioBuffer | null;
    startTime: number;
    pauseOffset: number;
    animFrame: number | null;
    loaded: boolean;
    volume: number;
  }

  let pitchPlayers = $state<PitchPlayer[]>([]);
  let dragCounter = $state(0);
  let toast = $state<{ message: string; type: 'success' | 'error' } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  function showToast(message: string, type: 'success' | 'error') {
    toast = { message, type };
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = null; }, 3000);
  }

  function accent(): string {
    if (typeof document === 'undefined') return '#6c5ce7';
    const c = getComputedStyle(document.body).getPropertyValue('--accent').trim();
    return c || '#6c5ce7';
  }

  function newPlayer(id: string, name: string): PitchPlayer {
    return {
      id, name,
      status: 'uploading',
      audioCtx: null, playing: false, paused: false,
      currentTime: 0, duration: 0, seekValue: 0,
      sourceNode: null, gainNode: null, buffer: null,
      startTime: 0, pauseOffset: 0, animFrame: null,
      loaded: false, volume: 100,
    };
  }

  // ---- Upload ----

  function handleDropZoneFile(f: File) {
    const id = crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
    const pf = newPlayer(id, f.name);
    pitchPlayers = [...pitchPlayers, pf];

    uploadPitchAudio(f).then(() => {
      pitchPlayers = pitchPlayers.map(p => p.id === id ? { ...p, status: 'ready' as const } : p);
    }).catch((err) => {
      pitchPlayers = pitchPlayers.map(p => p.id === id ? { ...p, status: 'error' as const, errorMsg: err.message } : p);
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

  // ---- Player ----

  function getCtx(p: PitchPlayer): AudioContext {
    if (!p.audioCtx) p.audioCtx = new AudioContext();
    return p.audioCtx;
  }

  async function loadBuffer(p: PitchPlayer) {
    if (p.loaded && p.buffer) return;
    const ctx = getCtx(p);
    const url = pitchInputDownloadUrl(p.name);
    const resp = await fetch(url);
    const arrayBuf = await resp.arrayBuffer();
    const audioBuf = await ctx.decodeAudioData(arrayBuf);
    p.buffer = audioBuf;
    p.duration = audioBuf.duration;
    p.loaded = true;
  }

  function stopSource(p: PitchPlayer) {
    if (p.sourceNode) {
      try { p.sourceNode.stop(); } catch { /* already stopped */ }
      p.sourceNode.disconnect();
      p.sourceNode = null;
    }
    if (p.gainNode) {
      p.gainNode.disconnect();
      p.gainNode = null;
    }
    if (p.animFrame) {
      cancelAnimationFrame(p.animFrame);
      p.animFrame = null;
    }
  }

  function cleanupPlayer(p: PitchPlayer) {
    stopSource(p);
    p.audioCtx?.close();
    p.audioCtx = null;
  }

  function getPlayerById(id: string): PitchPlayer | undefined {
    return pitchPlayers.find(p => p.id === id);
  }

  function startTimer(p: PitchPlayer) {
    function tick() {
      const pl = getPlayerById(p.id);
      if (!pl || !pl.playing || pl.paused) return;
      pl.currentTime = pl.audioCtx!.currentTime - pl.startTime;
      pl.seekValue = pl.currentTime;
      if (pl.currentTime >= pl.duration) {
        stopPlayer(p.id);
        return;
      }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }

  async function togglePlay(id: string) {
    const p = getPlayerById(id);
    if (!p) return;
    if (p.playing && !p.paused) { pausePlayer(id); return; }
    if (p.paused) { resumePlayer(id); return; }
    // Start fresh
    const ctx = getCtx(p);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadBuffer(p);
    stopSource(p);
    const now = ctx.currentTime;
    p.startTime = now;
    const gain = ctx.createGain();
    gain.gain.value = p.volume / 100;
    gain.connect(ctx.destination);
    const src = ctx.createBufferSource();
    src.buffer = p.buffer;
    src.connect(gain);
    src.start(0);
    p.sourceNode = src;
    p.gainNode = gain;
    p.playing = true;
    p.paused = false;
    startTimer(p);
  }

  function pausePlayer(id: string) {
    const p = getPlayerById(id);
    if (!p || !p.playing || p.paused) return;
    p.pauseOffset = p.audioCtx!.currentTime - p.startTime;
    p.audioCtx!.suspend();
    p.paused = true;
  }

  function resumePlayer(id: string) {
    const p = getPlayerById(id);
    if (!p || !p.playing || !p.paused) return;
    p.audioCtx!.resume();
    p.paused = false;
    startTimer(p);
  }

  function stopPlayer(id: string) {
    const p = getPlayerById(id);
    if (!p) return;
    stopSource(p);
    p.audioCtx?.suspend();
    p.playing = false;
    p.paused = false;
    p.currentTime = 0;
    p.seekValue = 0;
    p.pauseOffset = 0;
  }

  function handleSeekInput(e: Event, id: string) {
    const p = getPlayerById(id);
    if (!p) return;
    p.seekValue = parseFloat((e.target as HTMLInputElement).value);
  }

  async function handleSeekChange(e: Event, id: string) {
    const p = getPlayerById(id);
    if (!p || !p.buffer) return;
    const seekTo = parseFloat((e.target as HTMLInputElement).value);
    const wasPlaying = p.playing && !p.paused;
    stopSource(p);
    const ctx = getCtx(p);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadBuffer(p);
    const now = ctx.currentTime;
    p.startTime = now - seekTo;
    p.pauseOffset = seekTo;
    p.currentTime = seekTo;
    p.seekValue = seekTo;
    if (!wasPlaying) return;
    const gain = ctx.createGain();
    gain.gain.value = p.volume / 100;
    gain.connect(ctx.destination);
    const src = ctx.createBufferSource();
    src.buffer = p.buffer;
    src.connect(gain);
    src.start(0, seekTo);
    p.sourceNode = src;
    p.gainNode = gain;
    p.playing = true;
    p.paused = false;
    startTimer(p);
  }

  function handleVolume(e: Event, id: string) {
    const p = getPlayerById(id);
    if (!p) return;
    p.volume = parseInt((e.target as HTMLInputElement).value);
    if (p.gainNode) p.gainNode.gain.value = p.volume / 100;
  }

  // ---- Delete ----

  async function handleDelete(id: string) {
    const p = getPlayerById(id);
    if (!p) return;
    if (!confirm(`Eliminar "${p.name}"?`)) return;
    try {
      await deletePitchUpload(p.name);
    } catch (err) {
      // If backend delete fails, still remove from UI
      console.error('Failed to delete from server:', err);
    }
    cleanupPlayer(p);
    pitchPlayers = pitchPlayers.filter(pl => pl.id !== id);
    showToast(`"${p.name}" eliminado`, 'success');
  }

  // ---- Waveform ----

  let waveformCanvases = $state<Record<string, HTMLCanvasElement>>({});

  async function drawWaveform(canvas: HTMLCanvasElement, id: string) {
    const p = getPlayerById(id);
    if (!p) return;
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
    canvas.width = canvas.clientWidth * dpr;
    canvas.height = canvas.clientHeight * dpr;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const w = canvas.width, h = canvas.height;
    ctx.clearRect(0, 0, w, h);
    try {
      const url = pitchInputDownloadUrl(p.name);
      const resp = await fetch(url);
      const arrayBuf = await resp.arrayBuffer();
      const audioCtx = new OfflineAudioContext(1, 1, 44100);
      const audioBuf = await audioCtx.decodeAudioData(arrayBuf);
      const channel = audioBuf.getChannelData(0);
      const step = Math.max(1, Math.floor(channel.length / w));
      for (let i = 0; i < w; i++) {
        let max = 0;
        const start = i * step;
        const end = Math.min(start + step, channel.length);
        for (let j = start; j < end; j++) max = Math.max(max, Math.abs(channel[j]));
        const barH = Math.max(1, max * h);
        ctx.fillStyle = accent();
        ctx.fillRect(i, (h - barH) / 2, 1, barH);
      }
      audioCtx.close();
    } catch {
      // Fallback: deterministic bars
      let hash = 0;
      const key = p.name;
      for (let i = 0; i < key.length; i++) { hash = ((hash << 5) - hash) + key.charCodeAt(i); hash |= 0; }
      ctx.fillStyle = accent();
      const barCount = 40, barWidth = w / barCount;
      for (let i = 0; i < barCount; i++) {
        const hVal = ((Math.abs(hash + i * 31) % 80) / 100) * h * 0.8 + h * 0.1;
        ctx.fillRect(i * barWidth + 1, (h - hVal) / 2, barWidth - 2, hVal);
      }
    }
  }

  function waveformAction(node: HTMLCanvasElement, id: string) {
    waveformCanvases[id] = node;
    drawWaveform(node, id);
  }

  // ---- Format ----

  function fmtTime(sec: number | undefined): string {
    if (sec == null || !isFinite(sec)) return '0:00';
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  // ---- Cleanup ----

  onDestroy(() => {
    if (toastTimer) clearTimeout(toastTimer);
    for (const p of pitchPlayers) cleanupPlayer(p);
  });
</script>

<div class="pitch-page">
  <!-- Dropzone -->
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
  </section>

  <!-- Uploaded files with players -->
  {#if pitchPlayers.length > 0}
    <section class="pitch-players-section">
      <h3 class="section-title">Archivos subidos ({pitchPlayers.length})</h3>
      <div class="pitch-players-list">
        {#each pitchPlayers as p (p.id)}
          <div class="song-group" class:loading={p.status === 'uploading'}>
            {#if p.status === 'uploading'}
              <div class="song-header">
                <h3 class="song-name">📤 {p.name}</h3>
                <span class="upload-status">Subiendo…</span>
              </div>
            {:else if p.status === 'error'}
              <div class="song-header">
                <h3 class="song-name error">⚠️ {p.name}</h3>
                <span class="upload-status error">Error: {p.errorMsg}</span>
                <button class="song-btn delete-btn" onclick={() => handleDelete(p.id)} title="Eliminar">🗑</button>
              </div>
            {:else}
              <!-- Player header -->
              <div class="song-header">
                <h3 class="song-name">🎵 {p.name}</h3>
                <div class="playback-controls">
                  <button class="ctrl-btn play-btn" onclick={() => togglePlay(p.id)}
                    disabled={p.playing && !p.paused} title={p.playing && !p.paused ? 'Reproduciendo' : 'Reproducir'}>▶</button>
                  <button class="ctrl-btn pause-btn" onclick={() => pausePlayer(p.id)}
                    disabled={!p.playing || p.paused} title="Pausa">⏸</button>
                  <button class="ctrl-btn stop-btn" onclick={() => stopPlayer(p.id)}
                    disabled={!p.playing && !p.paused} title="Parar">⏹</button>
                </div>
                <div class="seek-area">
                  <input type="range" min="0" max={p.duration || 100} step="0.1"
                    value={p.seekValue || 0}
                    disabled={!p.loaded}
                    oninput={(e) => handleSeekInput(e, p.id)}
                    onchange={(e) => handleSeekChange(e, p.id)}
                    class="seek-slider" title="Buscar" />
                  <span class="time-display">{fmtTime(p.currentTime)} / {fmtTime(p.duration)}</span>
                </div>
                <div class="song-actions">
                  <a class="song-btn export-btn" href={pitchInputDownloadUrl(p.name)} download={p.name} title="Descargar">⬇</a>
                  <button class="song-btn delete-btn" onclick={() => handleDelete(p.id)} title="Eliminar">🗑</button>
                </div>
              </div>
              <!-- Waveform -->
              <div class="waveform-row">
                <canvas class="waveform-mini" width="200" height="48"
                  use:waveformAction={p.id}></canvas>
              </div>
              <!-- Volume -->
              <div class="stem-controls">
                <div class="vol-slider-wrap">
                  <label class="vol-label-small">Vol:</label>
                  <input type="range" min="0" max="100" value={p.volume}
                    oninput={(e) => handleVolume(e, p.id)} class="vol-slider" title="Volumen" />
                  <span class="vol-label">{p.volume}</span>
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </section>
  {/if}
</div>

{#if toast}
  <div class="toast {toast.type}">{toast.message}</div>
{/if}

<style>
  .pitch-page {
    width: 100%;
    max-width: 900px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
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

  /* ---- Dropzone ---- */

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

  /* ---- Player cards (same style as ResultsPanel) ---- */

  .pitch-players-section {
    width: 100%;
  }

  .pitch-players-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .song-group {
    background: var(--bg-surface);
    border-radius: 8px;
    padding: 0.75rem 1rem;
    animation: fadeIn 0.3s ease;
    border: 1px solid var(--border);
  }
  .song-group.loading {
    opacity: 0.7;
  }

  .song-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid var(--border);
    margin-bottom: 0.5rem;
    flex-wrap: wrap;
  }

  .song-name {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--accent-light);
    word-break: break-word;
    flex-shrink: 0;
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .song-name.error {
    color: #e57373;
  }

  .upload-status {
    font-size: 0.75rem;
    color: var(--text-muted);
    font-style: italic;
  }
  .upload-status.error {
    color: #e57373;
    font-style: normal;
  }

  /* ---- Transport buttons ---- */

  .playback-controls {
    display: flex;
    gap: 0.25rem;
    flex-shrink: 0;
  }

  .ctrl-btn {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: none;
    font-size: 0.85rem;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.2s, transform 0.1s, opacity 0.2s;
    flex-shrink: 0;
    padding: 0;
  }
  .ctrl-btn:active:not(:disabled) { transform: scale(0.95); }
  .ctrl-btn:disabled { opacity: 0.35; cursor: not-allowed; }

  .play-btn { background: var(--accent); color: #fff; }
  .play-btn:not(:disabled):hover { background: var(--accent-light); }
  .pause-btn { background: #ff9800; color: var(--text-primary); }
  .pause-btn:not(:disabled):hover { background: #e68900; }
  .stop-btn { background: #f44336; color: #fff; }
  .stop-btn:not(:disabled):hover { background: #d32f2f; }

  /* ---- Seek ---- */

  .seek-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 80px;
    max-width: 200px;
  }

  .seek-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 4px;
    border-radius: 2px;
    background: var(--bg-hover);
    outline: none;
    cursor: pointer;
  }
  .seek-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: 2px solid #0a0a14;
  }
  .seek-slider:disabled { opacity: 0.4; cursor: not-allowed; }

  .time-display {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
    text-align: right;
    white-space: nowrap;
  }

  /* ---- Song actions ---- */

  .song-actions {
    display: flex;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .song-btn {
    padding: 0.3rem 0.6rem;
    border-radius: 5px;
    border: 1px solid var(--border-light);
    background: var(--bg-hover);
    color: var(--text-secondary);
    font-size: 0.75rem;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
    white-space: nowrap;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
  }
  .song-btn:hover { background: #333355; border-color: var(--text-muted); }
  .export-btn:hover { color: var(--accent); border-color: var(--accent); }
  .delete-btn:hover { color: #f44336; border-color: #f44336; }

  /* ---- Waveform ---- */

  .waveform-row {
    width: 100%;
    margin-bottom: 0.4rem;
  }

  .waveform-mini {
    border-radius: 3px;
    flex-shrink: 0;
    display: block;
    width: 100%;
    height: 48px;
    background: #0a0a14;
  }

  /* ---- Stem controls ---- */

  .stem-controls {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }

  .vol-slider-wrap {
    display: flex;
    align-items: center;
    gap: 0.3rem;
  }

  .vol-label-small {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .vol-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 80px;
    height: 4px;
    border-radius: 2px;
    background: var(--bg-hover);
    outline: none;
    cursor: pointer;
  }
  .vol-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: 2px solid #0a0a14;
  }

  .vol-label {
    font-size: 0.7rem;
    color: var(--text-muted);
    min-width: 2em;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  /* ---- Toast ---- */

  .toast {
    position: fixed;
    bottom: 1rem;
    right: 1rem;
    padding: 0.6rem 1rem;
    border-radius: 8px;
    font-size: 0.85rem;
    z-index: 9999;
    animation: fadeIn 0.2s ease;
  }
  .toast.success { background: #2e7d32; color: #fff; }
  .toast.error { background: #c62828; color: #fff; }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }
</style>
