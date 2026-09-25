<script lang="ts">
  import ProgressPanel from './ProgressPanel.svelte';
  import type { QueueJobStep } from './api';

  let {
    presets = [] as {name: string, config: any}[],
    selectedPreset = '',
    onSelectPreset = (name: string) => {},
    hasFiles = false,
    onExecute = () => {},
    onCancel = () => {},
    onForce = () => {},
    disabled = false,
    errorMessage = '',
    progress = 0,
    status = 'idle',
    step = '',
    song = '',
    eta = '',
    device = '',
    model = '',
    flags = '',
    steps = [] as QueueJobStep[],
    jobs = [] as import('./api').QueueJob[],
  }: {
    presets?: {name: string, config: any}[];
    selectedPreset?: string;
    onSelectPreset?: (name: string) => void;
    hasFiles?: boolean;
    onExecute?: () => void;
    onCancel?: () => void;
    onForce?: () => void;
    disabled?: boolean;
    errorMessage?: string;
    progress?: number;
    status?: string;
    step?: string;
    song?: string;
    eta?: string;
    device?: string;
    model?: string;
    flags?: string;
    steps?: QueueJobStep[];
    jobs?: import('./api').QueueJob[];
  } = $props();
</script>

<section class="presets-section">
  <h3 class="presets-title">🎛 Presets</h3>
  
  <select class="preset-select-large" value={selectedPreset}
    onchange={(e) => onSelectPreset((e.target as HTMLSelectElement).value)}
    disabled={disabled}>
    <option value="">-- Sin preset --</option>
    {#each presets as p}
      <option value={p.name}>{p.name}</option>
    {/each}
  </select>

  <button class="btn-execute-large" onclick={onExecute} disabled={disabled || !hasFiles}>
    ▶ Ejecutar
  </button>

  {#if errorMessage}
    <p class="execute-error" role="alert">{errorMessage}</p>
  {/if}

  {#if status === 'running'}
    <button
      class="btn-stop"
      onclick={onCancel}
      title="Cancela el proceso en curso y limpia la cola de espera"
      aria-label="Cancela el proceso en curso y limpia la cola de espera"
    >
      ⏹ Detener
    </button>
    <span class="stop-hint">Cancela el proceso en curso y limpia la cola de espera</span>

    <div class="progress-card">
      <ProgressPanel
        {status}
        {step}
        {song}
        {eta}
        {device}
        {model}
        {flags}
        {progress}
        {steps}
        {jobs}
      />
    </div>
  {/if}
</section>

<style>
  .presets-section { width: 100%; box-sizing: border-box; background: var(--bg-surface); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin: 12px 0; }
  .presets-title { margin: 0 0 16px 0; color: var(--accent); font-size: 1rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; }
  .preset-select-large { width: 100%; padding: 14px 16px; background: var(--bg-primary); border: 1px solid var(--border); border-radius: 8px; color: var(--text-primary); font-size: 16px; cursor: pointer; margin-bottom: 12px; }
  .preset-select-large:focus { outline: none; border-color: var(--accent); }
  .btn-execute-large { width: 100%; padding: 14px; background: var(--accent); color: #fff; border: none; border-radius: 8px; font-size: 17px; font-weight: bold; cursor: pointer; margin-bottom: 12px; transition: background 0.2s; }
  .btn-execute-large:hover { background: var(--accent-light); }
  .btn-execute-large:disabled { opacity: 0.3; cursor: not-allowed; }

  .execute-error {
    margin: -4px 0 12px;
    padding: 10px 12px;
    background: rgba(244, 67, 54, 0.12);
    border: 1px solid rgba(244, 67, 54, 0.35);
    border-radius: 8px;
    color: #ef5350;
    font-size: 0.85rem;
    font-weight: 600;
    text-align: center;
  }

  .btn-stop { width: 100%; padding: 12px; background: #4a1a1a; color: #e57373; border: 1px solid #6a2a2a; border-radius: 8px; font-size: 15px; font-weight: bold; cursor: pointer; margin-bottom: 8px; transition: background 0.2s; }
  .btn-stop:hover { background: #5a2a2a; }
  .stop-hint { display: block; font-size: 0.75rem; color: var(--text-muted); margin-bottom: 12px; text-align: center; }

  .progress-card { background: var(--bg-primary); border-radius: 8px; padding: 14px; }
</style>
