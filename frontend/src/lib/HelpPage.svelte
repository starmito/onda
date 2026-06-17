<script lang="ts">
  import { onMount } from 'svelte';
  import { IconOnda, IconModel, IconUpload, IconLogs, IconHelp, IconSettings } from './icons';
  const API_BASE = '';

  interface ServiceInfo {
    name: string;
    version: string;
    status: 'ok' | 'error' | 'loading';
    icon: string;
  }

  let version = $state('');
  let services = $state<ServiceInfo[]>([
    { name: 'Backend', version: '', status: 'loading', icon: IconSettings },
    { name: 'Frontend', version: '', status: 'loading', icon: IconUpload },
    { name: 'Pipeline', version: '', status: 'loading', icon: IconLogs },
    { name: 'GPU', version: '', status: 'loading', icon: IconModel },
    { name: 'Disco', version: '', status: 'loading', icon: IconHelp },
    { name: 'Runtime', version: '', status: 'loading', icon: IconSettings },
  ]);

  let starting = $state(false);
  let stopping = $state(false);
  let restarting = $state(false);

  onMount(() => {
    fetch(`${API_BASE}/api/health`)
      .then(r => r.json())
      .then(d => {
        version = d.version || '';
        const gpuDetail = d.gpu?.detail || '';
        const gpuName = gpuDetail.split(',')[0] || '—';
        const diskDetail = d.disk?.detail || '?';
        const runtime = d.gpu?.type === 'rocm' ? 'ROCm (AMD)' : d.gpu?.type === 'cuda' ? 'CUDA (NVIDIA)' : d.gpu?.ok ? 'CUDA (NVIDIA)' : 'CPU';
        
        services = services.map(s => {
          if (s.name === 'Backend') 
            return { ...s, version: d.backend?.version || '', status: d.backend?.ok ? 'ok' : 'error' };
          if (s.name === 'Frontend') 
            return { ...s, version: d.frontend?.version || '', status: d.frontend?.ok ? 'ok' : 'error' };
          if (s.name === 'Pipeline') 
            return { ...s, version: d.pipeline?.version || '', status: d.pipeline?.ok ? 'ok' : 'error' };
          if (s.name === 'GPU') 
            return { ...s, version: gpuName, status: d.gpu?.ok ? 'ok' : 'error' };
          if (s.name === 'Disco') 
            return { ...s, version: diskDetail, status: d.disk?.ok ? 'ok' : 'error' };
          if (s.name === 'Runtime') 
            return { ...s, version: runtime, status: 'ok' };
          return s;
        });
      })
      .catch(() => {
        version = '?';
        services = services.map(s => ({ ...s, status: 'error' }));
      });
  });

  async function handleStart() {
    starting = true;
    try {
      await fetch(`${API_BASE}/api/backend/start`, { method: 'POST' });
    } catch {}
    setTimeout(() => { starting = false; window.location.reload(); }, 1500);
  }

  async function handleStop() {
    stopping = true;
    try {
      await fetch(`${API_BASE}/api/backend/stop`, { method: 'POST' });
    } catch {}
    setTimeout(() => { stopping = false; window.location.reload(); }, 1500);
  }

  async function handleRestart() {
    restarting = true;
    try {
      await fetch(`${API_BASE}/api/backend/restart`, { method: 'POST' });
      setTimeout(() => window.location.reload(), 2000);
    } catch {
      restarting = false;
    }
  }
</script>

<div class="help-page">
  <!-- Hero section -->
  <div class="hero">
    <div class="hero-icon">{@html IconOnda}</div>
    <h1 class="hero-title">Onda</h1>
    <span class="hero-version">v{version}</span>
  </div>

  <!-- Services section -->
  <div class="services">
    <h2 class="section-title">Estado de servicios</h2>
    {#each services as svc}
      <div class="service-row">
        <span class="service-icon">{@html svc.icon}</span>
        <span class="service-name">{svc.name}</span>
        <span class="service-version">{svc.version || '—'}</span>
        <span class="service-status" class:status-ok={svc.status === 'ok'} class:status-error={svc.status === 'error'} class:status-loading={svc.status === 'loading'}>
          {svc.status === 'ok' ? '✓' : svc.status === 'error' ? '✗' : '⋯'}
        </span>
      </div>
    {/each}
  </div>

  <!-- Control buttons -->
  <div class="controls">
    <h2 class="section-title">Control del servicio</h2>
    <div class="control-buttons">
      <button class="ctrl-btn ctrl-start" onclick={handleStart}>
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="5,3 19,12 5,21"/></svg>
        Iniciar
      </button>
      <button class="ctrl-btn ctrl-stop" onclick={handleStop}>
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
        Detener
      </button>
      <button class="ctrl-btn ctrl-restart" onclick={handleRestart} disabled={restarting}>
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23,4 23,10 17,10"/><polyline points="1,20 1,14 7,14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>
        Reiniciar
      </button>
    </div>
  </div>
</div>

<style>
  .help-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2rem;
    padding: 2rem;
    width: 100%;
    max-width: 500px;
    margin: 0 auto;
    box-sizing: border-box;
  }

  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
  }

  .hero-icon :global(svg) {
    width: 80px;
    height: 80px;
    stroke: var(--accent);
    opacity: 0.8;
  }

  .hero-title {
    margin: 0;
    font-size: 2rem;
    font-weight: 700;
    color: var(--text-primary);
  }

  .hero-version {
    font-size: 0.9rem;
    color: var(--text-secondary);
    font-weight: 500;
  }

  .services, .controls {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .section-title {
    margin: 0 0 0.5rem;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 1px;
  }

  .service-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  .service-icon :global(svg) {
    width: 20px;
    height: 20px;
    stroke: var(--text-secondary);
  }

  .service-name {
    flex: 0 0 80px;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .service-version {
    flex: 1;
    font-size: 0.8rem;
    color: var(--text-secondary);
    text-align: right;
    font-family: monospace;
  }

  .service-status {
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.85rem;
    font-weight: bold;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .status-ok { background: rgba(76, 175, 80, 0.15); color: #4caf50; }
  .status-error { background: rgba(244, 67, 54, 0.15); color: #f44336; }
  .status-loading { background: rgba(255, 152, 0, 0.15); color: #ff9800; }

  /* Control buttons */
  .control-buttons {
    display: flex;
    gap: 10px;
  }

  .ctrl-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .ctrl-btn:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: var(--accent);
  }

  .ctrl-start:hover:not(:disabled) {
    background: rgba(76, 175, 80, 0.15);
    border-color: #4caf50;
  }
  .ctrl-start :global(svg) { stroke: #4caf50; }

  .ctrl-stop:hover:not(:disabled) {
    background: rgba(244, 67, 54, 0.15);
    border-color: #f44336;
  }
  .ctrl-stop :global(svg) { stroke: #f44336; }

  .ctrl-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .ctrl-btn :global(svg) {
    stroke: var(--accent);
  }
</style>
