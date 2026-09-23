<script lang="ts">
  import type { QueueJobStep } from './api';
  import { formatEta } from './time';

  let {
    status = '',
    step = '',
    song = '',
    eta = '',
    device = '',
    model = '',
    flags = '',
    progress = 0,
    steps = [] as QueueJobStep[],
    gpuInfo = null as { total_mb: number; free_mb: number; name?: string } | null,
  } = $props();

  const STATUS_LABELS: Record<string, string> = {
    queued: 'Pendiente',
    running: 'En curso',
    done: 'Terminado',
    failed: 'Fallo',
  };

  const hasSteps = $derived(steps && steps.length > 0);

  function stepStatusClass(stepStatus: string): string {
    switch (stepStatus) {
      case 'done': return 'step-row step-done';
      case 'failed': return 'step-row step-failed';
      case 'running': return 'step-row step-running';
      default: return 'step-row step-pending';
    }
  }

  function badgeClass(stepStatus: string): string {
    switch (stepStatus) {
      case 'done': return 'badge badge-green';
      case 'failed': return 'badge badge-red';
      case 'running': return 'badge badge-yellow';
      default: return 'badge';
    }
  }

  function stepProgress(step: QueueJobStep): number {
    // Pending steps are always shown at 0 so we never fake progress.
    if (step.status === 'queued') return 0;
    return Math.round(step.progress ?? 0);
  }
</script>

<div class="progress-panel">
  {#if hasSteps}
    <div class="steps-list" data-testid="steps-list">
      {#each steps as s, index (s.id || index)}
        <div class={stepStatusClass(s.status)} data-testid="step-row">
          <div class="step-info">
            <span class="step-name">{s.name || s.id || `Paso ${index + 1}`}</span>
            <span class={badgeClass(s.status)}>{STATUS_LABELS[s.status] || s.status}</span>
          </div>
          <div class="step-bar-wrap">
            <div
              class="step-bar-fill"
              class:done={s.status === 'done'}
              class:failed={s.status === 'failed'}
              class:pending={s.status === 'queued'}
              style="width: {stepProgress(s)}%"
            ></div>
          </div>
          <div class="step-meta">
            <span class="step-pct">{stepProgress(s)}%</span>
            {#if s.eta}<span class="step-eta">⏱ {formatEta(s.eta)}</span>{/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <div class="global-summary">
    <div class="progress-header">
      <span class="progress-status">{status}</span>
      {#if step}<span class="progress-step">{step}</span>{/if}
    </div>
    <div class="progress-bar-wrap">
      <div class="progress-bar-fill" style="width: {progress * 100}%"></div>
    </div>
    <div class="progress-meta">
      <span class="progress-pct">{Math.round(progress * 100)}%</span>
      {#if song}<span class="progress-song">{song}</span>{/if}
      {#if eta}<span class="progress-eta">⏱ {eta}</span>{/if}
      {#if device}
        <span class="progress-device" class:cpu={device !== 'cuda' && device !== 'gpu'}>
          {device === 'cuda' || device === 'gpu' ? 'GPU' : '⚠️ CPU'}
        </span>
      {/if}
      {#if model}<span class="progress-model" title="Modelo en uso">model: {model}</span>{/if}
      {#if flags}<span class="progress-flags" title={flags}>flags: {flags}</span>{/if}
      {#if gpuInfo}
        <span class="progress-gpu" class:vram-low={gpuInfo.free_mb < 2000}>
          GPU: {gpuInfo.free_mb}/{gpuInfo.total_mb} MB libres
        </span>
      {/if}
    </div>
  </div>
</div>

<style>
  .progress-panel {
    width: 100%;
  }

  .steps-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }

  .step-row {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0.6rem 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    transition: border-color 0.2s, background 0.2s;
  }

  .step-row.step-running {
    border-color: var(--accent);
    background: var(--accent-subtle);
  }

  .step-row.step-done {
    border-color: rgba(76, 175, 80, 0.4);
  }

  .step-row.step-failed {
    border-color: rgba(244, 67, 54, 0.4);
  }

  .step-row.step-pending {
    opacity: 0.85;
  }

  .step-info {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
  }

  .step-name {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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

  .step-bar-wrap {
    width: 100%;
    height: 6px;
    background: var(--bg-hover);
    border-radius: 3px;
    overflow: hidden;
  }

  .step-bar-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), #4caf50);
    border-radius: 3px;
    transition: width 0.3s ease;
  }
  .step-bar-fill.done { background: #4caf50; }
  .step-bar-fill.failed { background: #e57373; }
  .step-bar-fill.pending { background: #555; }

  .step-meta {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .step-pct {
    font-weight: 700;
    color: var(--accent-light);
  }

  .step-eta {
    color: #ff9800;
  }

  .global-summary {
    width: 100%;
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
    width: 100%;
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
  .progress-device.cpu {
    color: #ffb74d;
    background: rgba(255, 152, 0, 0.12);
    border: 1px solid rgba(255, 152, 0, 0.25);
  }
  .progress-model {
    color: var(--accent-light);
    font-size: 11px;
    background: rgba(128,128,128,0.1);
    padding: 2px 8px;
    border-radius: 4px;
  }
  .progress-flags {
    color: var(--text-secondary);
    font-size: 11px;
    background: rgba(128,128,128,0.1);
    padding: 2px 8px;
    border-radius: 4px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .progress-gpu {
    color: var(--text-secondary);
    font-size: 11px;
    background: rgba(128,128,128,0.1);
    padding: 2px 8px;
    border-radius: 4px;
  }
  .progress-gpu.vram-low {
    background: rgba(244, 67, 54, 0.15);
    color: #ef5350;
  }
</style>
