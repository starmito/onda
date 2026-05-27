<script lang="ts">
  import { getHealth, restartBackend, stopBackend, startBackend, type HealthResponse } from './api';

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
      // Re-poll immediately
      getHealth()
        .then((h) => (health = h))
        .catch(() => {});
    } catch (e: any) {
      console.error('Restart failed:', e.message);
    }
  }

  async function handleStop() {
    try {
      await stopBackend();
      getHealth()
        .then((h) => (health = h))
        .catch(() => {});
    } catch (e: any) {
      console.error('Stop failed:', e.message);
    }
  }

  async function handleStart() {
    try {
      await startBackend();
      getHealth()
        .then((h) => (health = h))
        .catch(() => {});
    } catch (e: any) {
      console.error('Start failed:', e.message);
    }
  }

  const indicators = $derived([
    { label: 'API', ok: health?.backend?.ok ?? false, detail: '' },
    {
      label: 'GPU',
      ok: health?.gpu?.ok ?? false,
      detail: health?.gpu?.detail || '',
    },
    { label: 'Docker', ok: health?.docker?.ok ?? false, detail: '' },
    {
      label: 'Disco',
      ok: health?.disk?.ok ?? false,
      detail: health?.disk?.detail || '',
    },
  ]);
</script>

<div class="status-bar">
  <div class="indicators">
    {#each indicators as ind}
      <div class="indicator" class:green={ind.ok} class:red={!ind.ok}>
        <span class="dot"></span>
        <span class="ind-label">{ind.label}</span>
        {#if ind.detail}
          <span class="ind-detail">{ind.detail}</span>
        {/if}
      </div>
    {/each}
  </div>

  <div class="actions">
    <button class="btn restart" onclick={handleRestart}>Reiniciar</button>
    <button class="btn stop" onclick={handleStop}>Parar</button>
    <button class="btn start" onclick={handleStart}>Iniciar</button>
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
    padding: 0 1.5rem;
    z-index: 900;
  }

  .indicators {
    display: flex;
    gap: 1rem;
    align-items: center;
  }

  .indicator {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.75rem;
    color: #888;
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
    color: #c0c0d0;
  }
  .indicator.green .ind-label {
    color: #81c784;
  }
  .indicator.red .ind-label {
    color: #e57373;
  }

  .ind-detail {
    color: #606080;
    font-size: 0.7rem;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
  }

  .btn {
    padding: 0.25rem 0.7rem;
    border: 1px solid #2a2a4a;
    border-radius: 4px;
    background: #1a1a2e;
    color: #c0c0d0;
    font-size: 0.7rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }
  .btn:hover {
    background: #2a2a4a;
    border-color: #3a3a5a;
  }
  .btn.restart:hover {
    border-color: #ff9800;
    color: #ff9800;
  }
  .btn.stop:hover {
    border-color: #f44336;
    color: #f44336;
  }
  .btn.start:hover {
    border-color: #4caf50;
    color: #4caf50;
  }
</style>
