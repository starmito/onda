<script lang="ts">
  import type { QueueJob, QueueJobStep } from './api';
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
    jobs = [] as QueueJob[],
    gpuInfo = null as { total_mb: number; free_mb: number; name?: string } | null,
  } = $props();

  const STATUS_LABELS: Record<string, string> = {
    queued: 'Pendiente',
    running: 'En curso',
    done: 'Terminado',
    failed: 'Fallo',
  };

  const JOB_STATUS_LABELS: Record<QueueJob['status'], string> = {
    waiting: 'Pendiente',
    processing: 'En curso',
    done: 'Terminado',
    error: 'Fallo',
    blocked_no_gpu: 'Bloqueado',
  };

  const hasJobs = $derived(jobs && jobs.length > 0);
  const hasSteps = $derived(steps && steps.length > 0);
  const completedSongs = $derived(jobs.filter((j: QueueJob) => j.status === 'done').length);
  const totalSongs = $derived(jobs.length);
  const globalPct = $derived.by(() => {
    const pct = Math.min(100, Math.round(progress * 100));
    // Keep the honest 99% cap while any queued song has not finished yet.
    if (hasJobs && completedSongs < totalSongs && pct >= 100) {
      return 99;
    }
    return pct;
  });

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
    if (step.status === 'queued') return 0;
    return Math.round(step.progress ?? 0);
  }

  function jobStatusClass(jobStatus: QueueJob['status']): string {
    switch (jobStatus) {
      case 'done': return 'job-row job-done';
      case 'error': return 'job-row job-error';
      case 'processing': return 'job-row job-running';
      default: return 'job-row job-pending';
    }
  }

  function jobBadgeClass(jobStatus: QueueJob['status']): string {
    switch (jobStatus) {
      case 'done': return 'badge badge-green';
      case 'error': return 'badge badge-red';
      case 'processing': return 'badge badge-yellow';
      default: return 'badge';
    }
  }

  function jobStepText(job: QueueJob): string {
    if (job.status === 'done') return 'Completado';
    if (job.status === 'error') return job.step_name || 'Error';
    if (job.current_step != null && job.total_steps != null && job.total_steps > 0) {
      const name = job.step_name ? ` (${job.step_name})` : '';
      return `Paso ${job.current_step}/${job.total_steps}${name}`;
    }
    if (job.step_name) return job.step_name;
    return '';
  }

  function jobDeviceLabel(job: QueueJob): string {
    if (job.ran_on_cpu || job.device === 'cpu') return 'CPU';
    if (job.device === 'cuda' || job.device === 'gpu') return 'GPU';
    return job.device || '';
  }
</script>

<div class="progress-panel">
  {#if hasJobs}
    <!-- Global proportional bar -->
    <div class="global-summary" data-testid="global-progress">
      <div class="progress-header">
        <span class="progress-status">Progreso global</span>
        <span class="progress-count">{completedSongs}/{totalSongs} canciones</span>
      </div>
      <div class="progress-bar-wrap">
        <div class="progress-bar-fill" style="width: {globalPct}%"></div>
      </div>
      <div class="progress-meta">
        <span class="progress-pct">{globalPct}%</span>
      </div>
    </div>

    <!-- One bar per song -->
    <div class="jobs-list" data-testid="jobs-list">
      {#each jobs as job, index (job.song || index)}
        <div class={jobStatusClass(job.status)} data-testid="job-row">
          <div class="job-info">
            <span class="job-name">{job.song}</span>
            <span class={jobBadgeClass(job.status)}>{JOB_STATUS_LABELS[job.status] || job.status}</span>
          </div>
          <div class="job-bar-wrap">
            <div
              class="job-bar-fill"
              class:done={job.status === 'done'}
              class:error={job.status === 'error'}
              class:pending={job.status === 'waiting'}
              style="width: {Math.round(job.progress ?? 0)}%"
            ></div>
          </div>
          <div class="job-meta">
            <span class="job-pct">{Math.round(job.progress ?? 0)}%</span>
            {#if jobStepText(job)}<span class="job-step">{jobStepText(job)}</span>{/if}
            {#if job.eta && formatEta(job.eta)}<span class="job-eta">⏱ {formatEta(job.eta)}</span>{/if}
            {#if jobDeviceLabel(job)}
              <span class="job-device" class:cpu={job.ran_on_cpu || job.device === 'cpu'}>
                {jobDeviceLabel(job)}
              </span>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}

  {#if hasSteps && !hasJobs}
    <div class="steps-list" data-testid="steps-list">
      {#each steps as s, index (s.id || index)}
        <div class={stepStatusClass(s.status)} data-testid="step-row">
          <div class="step-info">
            <span class="step-name">
              {#if s.name}
                {s.name}
              {:else if s.id && !s.id.startsWith('step-')}
                Paso {index + 1} ({s.id})
              {:else}
                Paso {index + 1}
              {/if}
            </span>
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

  {#if !hasJobs}
    <div class="global-summary">
      <div class="progress-header">
        <span class="progress-status">{progress >= 1 && status === 'running' ? 'Finalizando' : STATUS_LABELS[status] || status}</span>
        {#if step}<span class="progress-step">{step}</span>{/if}
      </div>
      <div class="progress-bar-wrap">
        <div class="progress-bar-fill" style="width: {globalPct}%"></div>
      </div>
      <div class="progress-meta">
        <span class="progress-pct">{globalPct}%</span>
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
  {/if}
</div>

<style>
  .progress-panel {
    width: 100%;
  }

  .steps-list,
  .jobs-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }

  .step-row,
  .job-row {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0.6rem 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    transition: border-color 0.2s, background 0.2s;
  }

  .step-row.step-running,
  .job-row.job-running {
    border-color: var(--accent);
    background: var(--accent-subtle);
  }

  .step-row.step-done,
  .job-row.job-done {
    border-color: rgba(76, 175, 80, 0.4);
  }

  .step-row.step-failed,
  .job-row.job-error {
    border-color: rgba(244, 67, 54, 0.4);
  }

  .step-row.step-pending,
  .job-row.job-pending {
    opacity: 0.85;
  }

  .step-info,
  .job-info {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
  }

  .step-name,
  .job-name {
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

  .step-bar-wrap,
  .job-bar-wrap {
    width: 100%;
    height: 6px;
    background: var(--bg-hover);
    border-radius: 3px;
    overflow: hidden;
  }

  .step-bar-fill,
  .job-bar-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), #4caf50);
    border-radius: 3px;
    transition: width 0.3s ease;
  }
  .step-bar-fill.done,
  .job-bar-fill.done { background: #4caf50; }
  .step-bar-fill.failed,
  .job-bar-fill.error { background: #e57373; }
  .step-bar-fill.pending,
  .job-bar-fill.pending { background: #555; }

  .step-meta,
  .job-meta {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    font-size: 0.75rem;
    color: var(--text-secondary);
    flex-wrap: wrap;
  }

  .step-pct,
  .job-pct {
    font-weight: 700;
    color: var(--accent-light);
  }

  .step-eta,
  .job-eta {
    color: #ff9800;
  }

  .job-device {
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: rgba(128,128,128,0.1);
    font-size: 0.65rem;
    text-transform: uppercase;
  }
  .job-device.cpu {
    color: #ffb74d;
    background: rgba(255, 152, 0, 0.12);
    border: 1px solid rgba(255, 152, 0, 0.25);
  }

  .global-summary {
    width: 100%;
    margin-bottom: 1rem;
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
  .progress-count {
    color: var(--text-secondary);
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
