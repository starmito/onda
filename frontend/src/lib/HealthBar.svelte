<script lang="ts">
  import { getHealth } from './api';
  import type { HealthResponse } from './api';

  type HealthStatus = { key: string; label: string; ok: boolean | null; detail: string };
  let indicators = $state<HealthStatus[]>([
    { key: 'be', label: 'BE', ok: null, detail: '' },
    { key: 'gpu', label: 'GPU', ok: null, detail: '' },
    { key: 'disk', label: 'Disk', ok: null, detail: '' },
    { key: 'docker', label: 'Docker', ok: null, detail: '' },
  ]);
  let tooltipIdx = $state<number | null>(null);
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function mapHealth(h: HealthResponse): void {
    indicators = [
      { key: 'be', label: 'BE', ok: h.status === 'ok', detail: `v${h.version} | ${h.container}` },
      { key: 'gpu', label: 'GPU', ok: h.gpu, detail: h.gpu_info ?? (h.gpu ? 'Available' : 'Not detected') },
      { key: 'disk', label: 'Disk', ok: h.disk ? !h.disk.includes('error') : null, detail: h.disk ?? 'Unknown' },
      { key: 'docker', label: 'Docker', ok: h.docker ? !h.docker.includes('error') : null, detail: h.docker ?? 'Unknown' },
    ];
  }

  async function poll() {
    try {
      const h = await getHealth();
      mapHealth(h);
    } catch {
      indicators = indicators.map((i) => ({ ...i, ok: i.ok === null ? false : i.ok }));
      indicators[0] = { ...indicators[0], ok: false, detail: 'Backend unreachable' };
    }
  }

  $effect(() => {
    poll();
    pollTimer = setInterval(poll, 15_000);
    return () => {
      if (pollTimer) clearInterval(pollTimer);
    };
  });
</script>

<div class="health-bar">
  {#each indicators as ind, i}
    <button
      class="health-dot"
      class:ok={ind.ok === true}
      class:err={ind.ok === false}
      class:unknown={ind.ok === null}
      onclick={() => (tooltipIdx = tooltipIdx === i ? null : i)}
      aria-label={`${ind.label}: ${ind.ok === true ? 'OK' : ind.ok === false ? 'Error' : 'Unknown'}`}
    >
      <span class="dot"></span>
      <span class="label">{ind.label}</span>
    </button>
  {/each}
</div>

{#if tooltipIdx !== null}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="tooltip-overlay" role="button" tabindex="0" onclick={() => (tooltipIdx = null)} onkeydown={(e) => { if (e.key === 'Escape') tooltipIdx = null; }}>
    <div class="tooltip-box">
      <strong>{indicators[tooltipIdx].label}</strong>
      <p>{indicators[tooltipIdx].detail || 'No data'}</p>
    </div>
  </div>
{/if}

<style>
  .health-bar {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }

  .health-dot {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    background: none;
    border: 1px solid #333;
    border-radius: 12px;
    padding: 0.2rem 0.5rem;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
  }
  .health-dot:hover {
    border-color: #555;
    background: #1a1a2e;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    display: inline-block;
    background: #555;
    transition: background 0.3s;
  }
  .ok .dot { background: #4caf50; box-shadow: 0 0 6px rgba(76, 175, 80, 0.5); }
  .err .dot { background: #f44336; box-shadow: 0 0 6px rgba(244, 67, 54, 0.5); }
  .unknown .dot { background: #ff9800; }

  .label {
    font-size: 0.7rem;
    color: #aaa;
    font-weight: 600;
  }
  .ok .label { color: #4caf50; }
  .err .label { color: #f44336; }

  .tooltip-overlay {
    position: fixed;
    inset: 0;
    z-index: 100;
  }

  .tooltip-box {
    position: absolute;
    top: 3rem;
    right: 1.5rem;
    background: #1a1a2e;
    border: 1px solid #444;
    border-radius: 8px;
    padding: 0.75rem 1rem;
    min-width: 180px;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.5);
    animation: fadeIn 0.15s ease;
  }

  .tooltip-box strong {
    color: #00d4ff;
    font-size: 0.85rem;
  }

  .tooltip-box p {
    margin: 0.35rem 0 0;
    color: #ccc;
    font-size: 0.8rem;
    word-break: break-word;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(-4px); }
    to { opacity: 1; transform: translateY(0); }
  }
</style>
