<script lang="ts">
  import { startBackend, restartBackend, stopBackend } from './api';
  import type { BackendActionResponse } from './api';

  type BackendState = 'running' | 'stopped' | 'loading' | 'unknown';

  let status: BackendState = $state('unknown');
  let statusDetail: string = $state('');

  function setStatusFromEvent(e: Event): void {
    const detail = (e as CustomEvent).detail as { ok: boolean | null; detail: string } | undefined;
    if (!detail) return;
    if (detail.ok === true) {
      status = 'running';
    } else if (detail.ok === false) {
      status = 'stopped';
    } else {
      status = 'unknown';
    }
    statusDetail = detail.detail || '';
  }

  async function doAction(
    action: 'start' | 'restart' | 'stop',
    fn: () => Promise<BackendActionResponse>
  ): Promise<void> {
    status = 'loading';
    statusDetail = '...';
    try {
      const res = await fn();
      status = res.ok ? 'running' : 'stopped';
      statusDetail = res.detail || '';
    } catch (err: any) {
      status = 'stopped';
      statusDetail = err.message || 'Request failed';
    }
  }

  $effect(() => {
    window.addEventListener('backend-status-change', setStatusFromEvent);
    return () => {
      window.removeEventListener('backend-status-change', setStatusFromEvent);
    };
  });
</script>

<div class="backend-controls">
  <button
    class="ctrl-btn start"
    onclick={() => doAction('start', startBackend)}
    disabled={status === 'loading'}
    aria-label="Start backend"
  >
    ▶ Start
  </button>

  <button
    class="ctrl-btn restart"
    onclick={() => doAction('restart', restartBackend)}
    disabled={status === 'loading'}
    aria-label="Restart backend"
  >
    🔄 Restart
  </button>

  <button
    class="ctrl-btn stop"
    onclick={() => doAction('stop', stopBackend)}
    disabled={status === 'loading'}
    aria-label="Stop backend"
  >
    ⏹ Stop
  </button>

  <span class="status-text" class:running={status === 'running'} class:stopped={status === 'stopped'} class:loading={status === 'loading'} class:unknown={status === 'unknown'}>
    {status === 'running' ? 'Running' : status === 'stopped' ? 'Stopped' : status === 'unknown' ? 'Unknown' : '...'}
  </span>
</div>

<style>
  .backend-controls {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
  }

  .ctrl-btn {
    padding: 0.25rem 0.6rem;
    border: none;
    border-radius: 6px;
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
    color: #0a0a14;
    transition: opacity 0.15s, transform 0.1s;
    white-space: nowrap;
  }
  .ctrl-btn:active {
    transform: scale(0.96);
  }
  .ctrl-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
    transform: none;
  }

  .ctrl-btn.start  { background: #00d4ff; }
  .ctrl-btn.restart { background: #f59e0b; }
  .ctrl-btn.stop   { background: #ef4444; color: #fff; }

  .status-text {
    font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
    font-size: 0.72rem;
    font-weight: 600;
    padding: 0.15rem 0.5rem;
    border-radius: 4px;
    background: #1a1a2e;
    border: 1px solid #333;
  }
  .status-text.running { color: #22c55e; }
  .status-text.stopped { color: #ef4444; }
  .status-text.loading { color: #f59e0b; }
  .status-text.unknown { color: #f59e0b; }
</style>
