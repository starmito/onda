<script lang="ts">
  import { getHealth } from './api';
  import type { HealthResponse } from './api';

  interface Indicator {
    key: string;
    label: string;
    ok: boolean | null;
    detail: string;
  }

  let indicators: Indicator[] = $state([
    { key: 'backend', label: 'BE', ok: null, detail: '' },
    { key: 'gpu', label: 'GPU', ok: null, detail: '' },
    { key: 'disk', label: 'Disk', ok: null, detail: '' },
    { key: 'docker', label: 'Docker', ok: null, detail: '' },
  ]);

  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function dispatchBackendStatus(ok: boolean | null, detail: string): void {
    window.dispatchEvent(
      new CustomEvent('backend-status-change', {
        detail: { ok, detail },
      })
    );
  }

  function mapHealth(h: HealthResponse): void {
    indicators = [
      { key: 'backend', label: 'BE', ok: h.backend.ok, detail: h.backend.detail },
      { key: 'gpu', label: 'GPU', ok: h.gpu.ok, detail: h.gpu.detail },
      { key: 'disk', label: 'Disk', ok: h.disk.ok, detail: h.disk.detail },
      { key: 'docker', label: 'Docker', ok: h.docker.ok, detail: h.docker.detail },
    ];
    dispatchBackendStatus(h.backend.ok, h.backend.detail);
  }

  async function poll(): Promise<void> {
    try {
      const h = await getHealth();
      mapHealth(h);
    } catch {
      // Keep previous ok values, but mark backend as unreachable
      indicators = indicators.map((i) =>
        i.key === 'backend'
          ? { ...i, ok: false, detail: 'Backend unreachable' }
          : { ...i }
      );
      dispatchBackendStatus(false, 'Backend unreachable');
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
  {#each indicators as ind (ind.key)}
    <span
      class="health-dot"
      class:ok={ind.ok === true}
      class:err={ind.ok === false}
      class:unknown={ind.ok === null}
      title={ind.detail || 'No data'}
      role="status"
      aria-label="{ind.label}: {ind.ok === true ? 'OK' : ind.ok === false ? 'Error' : 'Unknown'}"
    >
      <span class="dot"></span>
      <span class="label">{ind.label}</span>
    </span>
  {/each}
</div>

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
    cursor: default;
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
  .ok .dot { background: #22c55e; box-shadow: 0 0 6px rgba(34, 197, 94, 0.5); }
  .err .dot { background: #ef4444; box-shadow: 0 0 6px rgba(239, 68, 68, 0.5); }
  .unknown .dot { background: #f59e0b; }

  .label {
    font-size: 0.7rem;
    color: #aaa;
    font-weight: 600;
  }
  .ok .label { color: #22c55e; }
  .err .label { color: #ef4444; }
</style>
