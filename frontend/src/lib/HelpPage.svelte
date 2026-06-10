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

  onMount(() => {
    fetch(`${API_BASE}/api/health`)
      .then(r => r.json())
      .then(d => {
        version = d.version || '';
        // Extract GPU name from detail string
        const gpuDetail = d.gpu?.detail || '';
        const gpuName = gpuDetail.split(',')[0] || '—';
        // Extract disk free space from detail
        const diskDetail = d.disk?.detail || '?';
        // Determine runtime
        const runtime = d.gpu?.ok ? 'CUDA (NVIDIA)' : 'CPU';
        
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

  .services {
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

  .status-ok {
    background: rgba(76, 175, 80, 0.15);
    color: #4caf50;
  }

  .status-error {
    background: rgba(244, 67, 54, 0.15);
    color: #f44336;
  }

  .status-loading {
    background: rgba(255, 152, 0, 0.15);
    color: #ff9800;
  }
</style>
