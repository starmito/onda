<script lang="ts">
  import { getGpuInfo } from './api';
  import type { GpuInfo } from './api';

  let gpuInfo = $state<GpuInfo | null>(null);
  let error = $state('');
  let polling = $state(false);

  function formatMb(mb: number): string {
    if (mb >= 1024) {
      return (mb / 1024).toFixed(1) + ' GB';
    }
    return mb + ' MB';
  }

  function vramPercent(): number {
    if (!gpuInfo || gpuInfo.vram_total_mb === 0) return 0;
    return Math.round((gpuInfo.vram_used_mb / gpuInfo.vram_total_mb) * 100);
  }

  function barColor(percent: number): string {
    if (percent < 50) return '#4caf50';
    if (percent < 80) return '#ffc107';
    return '#f44336';
  }

  async function fetchGpu() {
    try {
      gpuInfo = await getGpuInfo();
      error = '';
    } catch (err: any) {
      error = err.message || 'GPU info unavailable';
      gpuInfo = null;
    }
  }

  // Polling: fetch immediately, then every 5s
  $effect(() => {
    fetchGpu();
    polling = true;
    const interval = setInterval(fetchGpu, 5000);
    return () => {
      clearInterval(interval);
      polling = false;
    };
  });
</script>

<div class="gpu-monitor">
  <h3 class="gpu-title">🖥️ GPU Monitor</h3>

  {#if error}
    <p class="gpu-error">⚠️ {error}</p>
  {:else if !gpuInfo}
    <p class="gpu-loading">Cargando GPU info...</p>
  {:else if !gpuInfo.ok}
    <p class="gpu-error">⚠️ GPU no disponible</p>
  {:else}
    <div class="vram-section">
      <div class="vram-bar-bg">
        <div
          class="vram-bar-fill"
          style="width: {vramPercent()}%; background-color: {barColor(vramPercent())}"
        ></div>
      </div>
      <div class="vram-label">
        {formatMb(gpuInfo.vram_used_mb)} / {formatMb(gpuInfo.vram_total_mb)}
      </div>
    </div>

    <div class="gpu-stats">
      <div class="stat">
        <span class="stat-label">Utilización:</span>
        <span class="stat-value">{vramPercent()}%</span>
      </div>
      <div class="stat">
        <span class="stat-label">Temp:</span>
        <span class="stat-value">{gpuInfo.temperature_c}°C</span>
      </div>
      <div class="stat">
        <span class="stat-label">Runtime:</span>
        <span class="stat-value">{gpuInfo.runtime}</span>
      </div>
    </div>
  {/if}
</div>

<style>
  .gpu-monitor {
    background: #1a1a2e;
    border-radius: 8px;
    padding: 0.75rem 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 220px;
  }

  .gpu-title {
    margin: 0;
    font-size: 0.85rem;
    font-weight: 600;
    color: #e0e0e0;
  }

  .gpu-error {
    margin: 0;
    font-size: 0.8rem;
    color: #f4a236;
  }

  .gpu-loading {
    margin: 0;
    font-size: 0.8rem;
    color: #888;
  }

  .vram-section {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .vram-bar-bg {
    width: 100%;
    height: 10px;
    background: #111;
    border-radius: 5px;
    overflow: hidden;
  }

  .vram-bar-fill {
    height: 100%;
    border-radius: 5px;
    transition: width 0.6s ease, background-color 0.6s ease;
    min-width: 2px;
    background: linear-gradient(
      90deg,
      #4caf50 0%,
      #ffc107 60%,
      #f44336 100%
    );
    /* overridden by inline style for exact percent */
  }

  .vram-label {
    font-size: 0.75rem;
    color: #aaa;
    text-align: right;
    font-family: 'Courier New', monospace;
  }

  .gpu-stats {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }

  .stat {
    display: flex;
    gap: 0.3rem;
    font-size: 0.78rem;
  }

  .stat-label {
    color: #888;
  }

  .stat-value {
    color: #00d4ff;
    font-weight: 500;
  }
</style>
