<script lang="ts">
  import { getHealth, restartBackend, stopBackend, startBackend, type HealthResponse, type VersionMismatchItem } from './api';

  let health = $state<HealthResponse | null>(null);

  // Poll health every 10s
  $effect(() => {
    let timer: ReturnType<typeof setInterval>;
    function poll() {
      getHealth()
        .then((h) => (health = h))
        .catch(() => (health = null));
    }
    poll();
    timer = setInterval(poll, 10000);
    return () => clearInterval(timer);
  });

  async function handleRestart() {
    try {
      await restartBackend();
      getHealth().then((h) => (health = h)).catch(() => {});
    } catch (e: any) {
      console.error('Restart failed:', e.message);
    }
  }

  async function handleStop() {
    try {
      await stopBackend();
      getHealth().then((h) => (health = h)).catch(() => {});
    } catch (e: any) {
      console.error('Stop failed:', e.message);
    }
  }

  async function handleStart() {
    try {
      await startBackend();
      getHealth().then((h) => (health = h)).catch(() => {});
    } catch (e: any) {
      console.error('Start failed:', e.message);
    }
  }

  // ── App components (first) ──
  const appIndicators = $derived([
    { label: 'Backend', ok: health?.backend?.ok ?? false, version: health?.backend?.version || '', detail: health?.backend?.detail || '' },
    { label: 'Frontend', ok: health?.frontend?.ok ?? false, version: health?.frontend?.version || '', detail: health?.frontend?.detail || '' },
    { label: 'Pipeline', ok: health?.pipeline?.ok ?? false, version: health?.pipeline?.version || '', detail: health?.pipeline?.detail || '' },
  ]);

  // ── Infra components (after separator) ──
  const infraIndicators = $derived([
    { label: 'GPU', ok: health?.gpu?.ok ?? false, version: '', detail: health?.gpu?.detail || '' },
    { label: 'Disco', ok: health?.disk?.ok ?? false, version: '', detail: health?.disk?.detail || '' },
    { label: 'Docker', ok: health?.docker?.ok ?? false, version: '', detail: health?.docker?.detail || '' },
  ]);

  const mismatch = $derived<VersionMismatchItem[] | null>(
    health?.version_mismatch?.ok === false ? (health.version_mismatch.detail || []) : null
  );

  const appVersion = $derived(health?.version || '');
</script>

<div class="status-bar">
  <div class="indicators">
    {#each appIndicators as ind}
      <div class="indicator" class:green={ind.ok} class:red={!ind.ok}>
        <span class="dot"></span>
        <span class="ind-label">{ind.label}</span>
        {#if ind.version}
          <span class="ind-version">{ind.version}</span>
        {/if}
        {#if ind.detail}
          <span class="ind-detail">{ind.detail}</span>
        {/if}
      </div>
    {/each}

    <span class="separator">|</span>

    {#each infraIndicators as ind}
      <div class="indicator" class:green={ind.ok} class:red={!ind.ok}>
        <span class="dot"></span>
        <span class="ind-label">{ind.label}</span>
        {#if ind.detail}
          <span class="ind-detail">{ind.detail}</span>
        {/if}
      </div>
    {/each}

    {#if appVersion}
      <span class="app-version">{appVersion}</span>
    {/if}
  </div>

  <div class="right">
    {#if mismatch}
      <div class="mismatch-warning" title={mismatch.map(m => `${m.component}: espera ${m.expected}, tiene ${m.actual}`).join('\n')}>
        ⚠️ Version mismatch
        <span class="mismatch-tooltip">
          {#each mismatch as m}
            <div>{m.component}: espera <code>{m.expected}</code>, tiene <code>{m.actual}</code></div>
          {/each}
        </span>
      </div>
    {/if}

    <div class="actions">
      <button class="btn restart" onclick={handleRestart}>Reiniciar</button>
      <button class="btn stop" onclick={handleStop}>Parar</button>
      <button class="btn start" onclick={handleStart}>Iniciar</button>
    </div>
  </div>
</div>

<style>
  .status-bar {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    height: 40px;
    background: #111122;
    border-top: 1px solid #1a1a2e;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 1rem;
    z-index: 900;
    font-size: 0.75rem;
  }

  .indicators {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    flex-shrink: 0;
  }

  .indicator {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    color: var(--text-secondary);
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #f44336;
    flex-shrink: 0;
  }
  .indicator.green .dot {
    background: #4caf50;
  }
  .indicator.red .dot {
    background: #f44336;
  }

  .ind-label {
    font-weight: 600;
    color: var(--text-primary);
  }
  .indicator.green .ind-label {
    color: #81c784;
  }
  .indicator.red .ind-label {
    color: #e57373;
  }

  .ind-version {
    color: var(--text-muted);
    font-size: 0.7rem;
  }

  .ind-detail {
    color: var(--text-muted);
    font-size: 0.7rem;
  }

  .separator {
    color: #2a2a4a;
    font-size: 0.85rem;
    margin: 0 0.2rem;
  }

  .app-version {
    color: #4a4a6a;
    font-size: 0.65rem;
    margin-left: 0.5rem;
    font-style: italic;
  }

  .right {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .mismatch-warning {
    color: #ff9800;
    font-weight: 600;
    font-size: 0.7rem;
    cursor: help;
    position: relative;
    white-space: nowrap;
  }

  .mismatch-tooltip {
    display: none;
    position: absolute;
    bottom: 100%;
    right: 0;
    margin-bottom: 6px;
    background: var(--bg-surface);
    border: 1px solid #ff9800;
    border-radius: 4px;
    padding: 0.5rem 0.75rem;
    color: var(--text-primary);
    font-weight: 400;
    font-size: 0.7rem;
    white-space: nowrap;
    z-index: 1000;
  }
  .mismatch-warning:hover .mismatch-tooltip {
    display: block;
  }
  .mismatch-tooltip div {
    padding: 2px 0;
  }
  .mismatch-tooltip code {
    color: #ff9800;
    background: #0a0a1a;
    padding: 1px 4px;
    border-radius: 2px;
  }

  .actions {
    display: flex;
    gap: 0.4rem;
  }

  .btn {
    padding: 0.2rem 0.6rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.65rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }
  .btn:hover {
    background: #2a2a4a;
    border-color: #3a3a5a;
  }
  .btn.restart:hover { border-color: #ff9800; color: #ff9800; }
  .btn.stop:hover    { border-color: #f44336; color: #f44336; }
  .btn.start:hover   { border-color: #4caf50; color: #4caf50; }
</style>
