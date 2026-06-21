<script lang="ts">
  import { onDestroy } from 'svelte';
  import WaveSurfer from 'wavesurfer.js';
  import Spectrogram from 'wavesurfer.js/dist/plugins/spectrogram.js';

  type DetectedKey = {
    key: string;
    scale: string;
    strength: number;
  };

  declare global {
    interface Window {
      __essentia?: any;
    }
  }

  let audioSrc = $state<string>('');
  let isPlaying = $state(false);
  let loading = $state(false);
  let detectedKey = $state<DetectedKey | null>(null);
  let error = $state('');
  let spectrogramVisible = $state(true);

  let fileInput: HTMLInputElement | null = $state(null);
  let waveformContainer: HTMLDivElement | null = $state(null);
  let ws: WaveSurfer | null = $state(null);

  async function getEssentia() {
    if (!window.__essentia) {
      const { Essentia, EssentiaWASM } = await import('essentia.js');
      window.__essentia = new Essentia(EssentiaWASM);
    }
    return window.__essentia;
  }

  function destroyWavesurfer() {
    if (ws) {
      ws.destroy();
      ws = null;
    }
  }

  function initWavesurfer(url: string) {
    destroyWavesurfer();
    if (!waveformContainer) return;

    const plugins = spectrogramVisible
      ? [
          Spectrogram.create({
            labels: true,
            height: 200,
            splitChannels: false,
          }),
        ]
      : [];

    const instance = WaveSurfer.create({
      container: waveformContainer,
      waveColor: '#4a4a7a',
      progressColor: '#7a7aba',
      url,
      plugins,
    });

    instance.on('play', () => {
      isPlaying = true;
    });
    instance.on('pause', () => {
      isPlaying = false;
    });
    instance.on('error', (err: any) => {
      error = `Error del reproductor: ${err?.message || err || 'desconocido'}`;
    });

    ws = instance;
  }

  function handleFileSelect(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;

    error = '';
    detectedKey = null;
    const url = URL.createObjectURL(file);
    if (audioSrc) {
      URL.revokeObjectURL(audioSrc);
    }
    audioSrc = url;
    initWavesurfer(url);
    input.value = '';
  }

  function togglePlay() {
    if (!ws) return;
    ws.playPause();
  }

  async function detectKey() {
    if (!audioSrc) {
      error = 'Carga un archivo de audio primero';
      return;
    }
    loading = true;
    error = '';
    detectedKey = null;

    try {
      const audioCtx = new AudioContext();
      const response = await fetch(audioSrc);
      const arrayBuffer = await response.arrayBuffer();
      const audioBuffer = await audioCtx.decodeAudioData(arrayBuffer);
      const channelData = audioBuffer.getChannelData(0);
      const essentia = await getEssentia();
      const vector = essentia.arrayToVector(channelData);
      const result = essentia.KeyExtractor(vector);
      detectedKey = {
        key: String(result.key),
        scale: String(result.scale),
        strength: Number(result.strength),
      };
      await audioCtx.close();
    } catch (err: any) {
      error = err?.message || 'Error al detectar la tonalidad';
    } finally {
      loading = false;
    }
  }

  function toggleSpectrogram() {
    spectrogramVisible = !spectrogramVisible;
    if (audioSrc) {
      initWavesurfer(audioSrc);
    }
  }

  onDestroy(() => {
    destroyWavesurfer();
    if (audioSrc) {
      URL.revokeObjectURL(audioSrc);
    }
  });
</script>

<section class="spectrogram-page">
  <header class="page-header">
    <h2>Espectrograma + Key Detection</h2>
    <div class="header-actions">
      <button class="btn-primary" onclick={() => fileInput?.click()}>
        Cargar audio
      </button>
      <input
        bind:this={fileInput}
        type="file"
        accept="audio/*"
        onchange={handleFileSelect}
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

  {#if audioSrc}
    <div class="player-card">
      <div class="waveform-wrap">
        <div bind:this={waveformContainer} class="waveform"></div>
      </div>

      <div class="player-controls">
        <button class="btn" onclick={togglePlay}>
          {isPlaying ? 'Pausa' : 'Play'}
        </button>
        <button class="btn" onclick={toggleSpectrogram}>
          {spectrogramVisible ? 'Ocultar espectrograma' : 'Mostrar espectrograma'}
        </button>
      </div>
    </div>

    <div class="key-section">
      <button class="btn-primary" onclick={detectKey} disabled={loading}>
        {loading ? 'Detectando…' : 'Detectar tonalidad'}
      </button>

      {#if detectedKey}
        <div class="key-result">
          <div class="key-circle">
            <span class="key-note">{detectedKey.key}</span>
            <span class="key-scale">{detectedKey.scale}</span>
          </div>
          <div class="key-meta">
            <span class="key-label">Tonalidad detectada</span>
            <span class="key-confidence">
              Confianza: {(detectedKey.strength * 100).toFixed(1)}%
            </span>
          </div>
        </div>
      {/if}
    </div>
  {:else}
    <div class="empty-state">
      <span class="empty-icon">📊</span>
      <p class="empty-title">Carga un archivo de audio para ver el espectrograma</p>
      <p class="empty-hint">Soporta la mayoría de formatos de audio</p>
    </div>
  {/if}
</section>

<style>
  .spectrogram-page {
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

  .btn,
  .btn-primary {
    padding: 0.55rem 0.9rem;
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

  .btn:disabled,
  .btn-primary:disabled {
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

  .player-card {
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
    padding: 1rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
  }

  .waveform-wrap {
    width: 100%;
    min-height: 320px;
    background: #1a1a2e;
    border: 1px solid var(--border);
    border-radius: 10px;
    overflow: hidden;
  }

  .waveform {
    width: 100%;
    min-height: 320px;
  }

  .player-controls {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .key-section {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    padding: 1rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
  }

  .key-result {
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .key-circle {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    width: 120px;
    height: 120px;
    border-radius: 50%;
    background: var(--accent-bg);
    border: 2px solid var(--accent-border);
    color: var(--text-primary);
  }

  .key-note {
    font-size: 2.5rem;
    font-weight: 800;
    line-height: 1;
  }

  .key-scale {
    font-size: 0.85rem;
    text-transform: lowercase;
    color: var(--text-secondary);
  }

  .key-meta {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .key-label {
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .key-confidence {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    flex: 1;
    min-height: 280px;
    border: 2px dashed var(--border);
    border-radius: 12px;
    background: var(--bg-primary);
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    text-align: center;
  }

  .empty-state:hover {
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
