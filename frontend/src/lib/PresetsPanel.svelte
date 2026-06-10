<script lang="ts">
  let {
    presets = [] as {name: string, config: any}[],
    selectedPreset = '',
    onSelectPreset = (name: string) => {},
    hasFiles = false,
    onExecute = () => {},
    disabled = false,
    progress = 0,
    status = 'idle',
    step = '',
    song = '',
    eta = '',
    device = '',
  } = $props();
</script>

<section class="presets-section">
  <h3 class="presets-title">🎛 Presets</h3>
  
  <select class="preset-select-large" value={selectedPreset}
    onchange={(e) => onSelectPreset((e.target as HTMLSelectElement).value)}
    disabled={disabled}>
    <option value="">-- Sin preset --</option>
    {#each presets as p}
      <option value={p.name}>{p.name}</option>
    {/each}
  </select>

  <button class="btn-execute-large" onclick={onExecute} disabled={disabled || !hasFiles || !selectedPreset}>
    ▶ Ejecutar
  </button>

  {#if status === 'running'}
    <div class="progress-card">
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
          <span class="progress-device">{device === 'cuda' || device === 'gpu' ? 'Ejecutando en GPU' : 'Ejecutando en CPU'}</span>
        {/if}
      </div>
    </div>
  {/if}
</section>

<style>
  .presets-section { width: 100%; box-sizing: border-box; background: #1a1a2e; border: 1px solid #2a2a4a; border-radius: 12px; padding: 20px; margin: 12px 0; }
  .presets-title { margin: 0 0 16px 0; color: var(--accent); font-size: 1rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; }
  .preset-select-large { width: 100%; padding: 14px 16px; background: #0a0a14; border: 1px solid #333; border-radius: 8px; color: #eee; font-size: 16px; cursor: pointer; margin-bottom: 12px; }
  .preset-select-large:focus { outline: none; border-color: var(--accent); }
  .btn-execute-large { width: 100%; padding: 14px; background: var(--accent); color: #fff; border: none; border-radius: 8px; font-size: 17px; font-weight: bold; cursor: pointer; margin-bottom: 12px; transition: background 0.2s; }
  .btn-execute-large:hover { background: #7c6ae8; }
  .btn-execute-large:disabled { opacity: 0.3; cursor: not-allowed; }
  .progress-card { background: #0a0a14; border-radius: 8px; padding: 14px; }
  .progress-header { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; }
  .progress-status { font-weight: bold; color: var(--accent-light); text-transform: uppercase; font-size: 13px; }
  .progress-step { color: #aaa; font-size: 13px; }
  .progress-bar-wrap { height: 8px; background: #1a1a2e; border-radius: 4px; margin-bottom: 8px; overflow: hidden; }
  .progress-bar-fill { height: 100%; background: linear-gradient(90deg, var(--accent), #4caf50); border-radius: 4px; transition: width 0.3s ease; }
  .progress-meta { display: flex; gap: 16px; flex-wrap: wrap; align-items: center; font-size: 12px; }
  .progress-pct { font-weight: bold; color: #4caf50; font-size: 16px; }
  .progress-song { color: #ccc; }
  .progress-eta { color: #ff9800; }
  .progress-device { color: #aaa; font-size: 11px; background: rgba(255,255,255,0.05); padding: 2px 8px; border-radius: 4px; }
</style>
