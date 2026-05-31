<script lang="ts">
  import { getGpuInfo, type GpuInfo, type QueueJob } from './api';

  let {
    disabled = false,
    presets = {} as Record<string, any>,
    queueJobs = [] as QueueJob[],
    onstart,
    onviperxonly,
    ondemucsonly,
    onpitch,
  }: {
    disabled?: boolean;
    presets?: Record<string, any>;
    queueJobs?: QueueJob[];
    onstart?: (config: any) => void;
    onviperxonly?: (config: any) => void;
    ondemucsonly?: (config: any) => void;
    onpitch?: (pitch: number) => void;
  } = $props();

  let selectedPreset = $state('');
  let viperxEnabled = $state(true);
  let viperxKeep = $state<'both' | 'vocals' | 'instrumental'>('both');
  let demucsEnabled = $state(true);
  let demucsKeep = $state({ drums: true, bass: true, other: true, vocals: true });
  let pitch = $state(0);
  let gpuInfo = $state<GpuInfo | null>(null);

  const presetKeys = $derived(Object.keys(presets));

  function handlePresetChange(e: Event) {
    const val = (e.target as HTMLSelectElement).value;
    selectedPreset = val;
    if (val && presets[val]) {
      const p = presets[val];
      viperxEnabled = !!p.vocalModel;
      demucsEnabled = !!p.stemModel;
    }
  }

  function buildConfig() {
    const dk: string[] = [];
    if (demucsKeep.drums) dk.push('drums');
    if (demucsKeep.bass) dk.push('bass');
    if (demucsKeep.other) dk.push('other');
    if (demucsKeep.vocals) dk.push('vocals');
    return {
      viperx: viperxEnabled,
      viperxKeep: viperxEnabled ? viperxKeep : undefined,
      demucs: demucsEnabled,
      demucsKeep: demucsEnabled ? dk : undefined,
      preset: selectedPreset || undefined,
    };
  }

  function handleStartAll() {
    onstart?.(buildConfig());
  }

  function handleViperxOnly() {
    onviperxonly?.({ ...buildConfig(), viperx: true });
  }

  function handleDemucsOnly() {
    ondemucsonly?.({ ...buildConfig(), demucs: true });
  }

  function handlePitchInput(e: Event) {
    const val = parseInt((e.target as HTMLInputElement).value);
    pitch = val;
    onpitch?.(val);
  }

  // Poll GPU info every 5s
  $effect(() => {
    let timer: ReturnType<typeof setInterval>;
    function poll() {
      getGpuInfo()
        .then((info) => (gpuInfo = info))
        .catch(() => {});
    }
    poll();
    timer = setInterval(poll, 5000);
    return () => clearInterval(timer);
  });

  function queueStatusEmoji(status: string): string {
    switch (status) {
      case 'waiting': return '⏳';
      case 'processing': return '⚙️';
      case 'done': return '✅';
      case 'error': return '❌';
      default: return '❓';
    }
  }
</script>

<div class="pipeline-card">
  <!-- Preset selector -->
  <div class="section">
    <label class="label" for="preset-select">Preset</label>
    <select
      id="preset-select"
      class="select"
      bind:value={selectedPreset}
      onchange={handlePresetChange}
      disabled={disabled}
    >
      <option value="">-- Seleccionar preset --</option>
      {#each presetKeys as key}
        <option value={key}>{presets[key]?.name || key} — {presets[key]?.description || ''}</option>
      {/each}
    </select>
  </div>

  <!-- Step toggles -->
  <div class="section">
    <label class="step-row">
      <input
        type="checkbox"
        bind:checked={viperxEnabled}
        disabled={disabled}
      />
      <span class="step-label">ViperX (separación vocal)</span>
    </label>
    <div class="step-options" class:disabled={!viperxEnabled}>
      <select
        class="select small"
        bind:value={viperxKeep}
        disabled={disabled || !viperxEnabled}
      >
        <option value="both">Ambos</option>
        <option value="vocals">Solo vocales</option>
        <option value="instrumental">Solo instrumental</option>
      </select>
    </div>
  </div>

  <div class="section">
    <label class="step-row">
      <input
        type="checkbox"
        bind:checked={demucsEnabled}
        disabled={disabled}
      />
      <span class="step-label">Demucs (separación stems)</span>
    </label>
    <div class="step-options" class:disabled={!demucsEnabled}>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsKeep.drums} disabled={disabled || !demucsEnabled} />
        🥁 Drums
      </label>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsKeep.bass} disabled={disabled || !demucsEnabled} />
        🎸 Bass
      </label>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsKeep.other} disabled={disabled || !demucsEnabled} />
        🎹 Other
      </label>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsKeep.vocals} disabled={disabled || !demucsEnabled} />
        🎤 Vocals
      </label>
    </div>
  </div>

  <!-- Pitch -->
  <div class="section pitch-section">
    <label class="label">Pitch (semitones): <strong>{pitch}</strong></label>
    <input
      type="range"
      min="-12"
      max="12"
      step="1"
      value={pitch}
      oninput={handlePitchInput}
      disabled={disabled}
      class="slider"
    />
  </div>

  <!-- Action buttons -->
  <div class="actions">
    <button class="btn btn-primary" onclick={handleStartAll} disabled={disabled}>
      ▶ Ejecutar todo
    </button>
    <button class="btn btn-secondary" onclick={handleViperxOnly} disabled={disabled}>
      Solo ViperX
    </button>
    <button class="btn btn-secondary" onclick={handleDemucsOnly} disabled={disabled}>
      Solo Demucs
    </button>
  </div>

  <!-- GPU info -->
  <div class="gpu-info">
    {#if gpuInfo}
      GPU: {gpuInfo.name} | VRAM: {gpuInfo.vram_used_mb}/{gpuInfo.vram_total_mb} MB
    {:else}
      GPU: -- | VRAM: --/-- MB
    {/if}
  </div>

  <!-- Queue jobs -->
  {#if queueJobs.length > 0}
    <div class="queue-section">
      <h4 class="queue-title">📋 Cola de procesamiento</h4>
      {#each queueJobs as job (job.song)}
        <div
          class="queue-item"
          class:processing={job.status === 'processing'}
          class:done={job.status === 'done'}
          class:error={job.status === 'error'}
        >
          <span class="queue-emoji">{queueStatusEmoji(job.status)}</span>
          <span class="queue-song" title={job.song}>{job.song}</span>
          <span class="queue-badge queue-badge-{job.status}">{job.status}</span>
          {#if job.status === 'error' && job.error}
            <span class="queue-error-msg">{job.error}</span>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .pipeline-card {
    width: 100%;
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 12px;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .label {
    font-size: 0.85rem;
    font-weight: 600;
    color: #a0a0c0;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .select {
    background: #0a0a14;
    border: 1px solid #2a2a4a;
    border-radius: 6px;
    color: #e0e0e0;
    padding: 0.5rem 0.75rem;
    font-size: 0.9rem;
    outline: none;
    transition: border-color 0.15s;
  }
  .select:focus {
    border-color: #00d4ff;
  }
  .select.small {
    font-size: 0.8rem;
    padding: 0.35rem 0.5rem;
  }

  .step-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    font-size: 0.95rem;
    color: #e0e0e0;
  }
  .step-row input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 16px;
    height: 16px;
  }
  .step-label {
    font-weight: 500;
  }

  .step-options {
    margin-left: 1.5rem;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    transition: opacity 0.2s;
  }
  .step-options.disabled {
    opacity: 0.35;
    pointer-events: none;
  }

  .chip {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    background: #0a0a14;
    border: 1px solid #2a2a4a;
    border-radius: 20px;
    padding: 0.3rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
    color: #c0c0d0;
  }
  .chip input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 14px;
    height: 14px;
  }

  .pitch-section {
    flex-direction: column;
  }
  .slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 6px;
    background: #2a2a4a;
    border-radius: 3px;
    outline: none;
  }
  .slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #00d4ff;
    cursor: pointer;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .btn {
    padding: 0.6rem 1.2rem;
    border: none;
    border-radius: 8px;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, opacity 0.15s;
    flex: 1;
    min-width: 130px;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-primary {
    background: linear-gradient(135deg, #00d4ff, #0088cc);
    color: #0a0a14;
  }
  .btn-primary:hover:not(:disabled) {
    background: linear-gradient(135deg, #33ddff, #0099dd);
  }
  .btn-secondary {
    background: #2a2a4a;
    color: #e0e0e0;
    border: 1px solid #3a3a5a;
  }
  .btn-secondary:hover:not(:disabled) {
    background: #3a3a5a;
  }

  .gpu-info {
    font-size: 0.7rem;
    color: #606080;
    text-align: right;
    margin-top: -0.25rem;
  }

  /* ---- Queue section ---- */
  .queue-section {
    border-top: 1px solid #2a2a4a;
    padding-top: 0.75rem;
    margin-top: 0.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .queue-title {
    margin: 0;
    font-size: 0.8rem;
    font-weight: 600;
    color: #a0a0c0;
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }

  .queue-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    background: #0a0a14;
    border: 1px solid #1a1a2e;
    font-size: 0.8rem;
    flex-wrap: wrap;
  }

  .queue-item.processing {
    background: #1a2a3a;
    border-color: #00d4ff44;
    animation: pulse-border 1.5s ease-in-out infinite;
  }

  .queue-item.done {
    border-color: #1b3a1b;
  }

  .queue-item.error {
    border-color: #3a1b1b;
  }

  @keyframes pulse-border {
    0%, 100% { border-color: #00d4ff44; }
    50% { border-color: #00d4ff88; }
  }

  .queue-emoji {
    font-size: 1rem;
    flex-shrink: 0;
  }

  .queue-song {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    color: #e0e0e0;
    font-weight: 500;
  }

  .queue-badge {
    padding: 0.1rem 0.4rem;
    border-radius: 8px;
    font-size: 0.6rem;
    font-weight: 700;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .queue-badge-waiting { background: #2a2a3e; color: #888; }
  .queue-badge-processing { background: #1b2a3a; color: #64b5f6; }
  .queue-badge-done { background: #1b3a1b; color: #81c784; }
  .queue-badge-error { background: #3a1b1b; color: #e57373; }

  .queue-error-msg {
    width: 100%;
    font-size: 0.7rem;
    color: #e57373;
    padding: 0.25rem 0.5rem;
    background: #2a1a1a;
    border-radius: 4px;
    margin-top: 0.25rem;
    word-break: break-all;
  }
</style>
