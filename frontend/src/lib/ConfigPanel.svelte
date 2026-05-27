<script lang="ts">
  import { getLocalModels, getVramEstimate, type LocalModel, type VramEstimateResponse } from './api';

  let {
    disabled = false,
    onchange,
  }: {
    disabled?: boolean;
    onchange?: (config: {
      vocalModel: string;
      stemModel: string;
      drumsModel: string;
      bassModel: string;
      otherModel: string;
      vocalOverlap: number;
    }) => void;
  } = $props();

  let vocalModel = $state('');
  let stemModel = $state('');
  let drumsModel = $state('');
  let bassModel = $state('');
  let otherModel = $state('');
  let vocalOverlap = $state(4);
  let localModels = $state<LocalModel[]>([]);
  let vramEstimate = $state<VramEstimateResponse | null>(null);
  let expanded = $state(false);

  // Load local models on mount
  $effect(() => {
    getLocalModels()
      .then((res) => (localModels = res.models || []))
      .catch(() => {});
  });

  // Fetch VRAM estimate when any model changes
  $effect(() => {
    const activeModels = [vocalModel, stemModel, drumsModel, bassModel, otherModel]
      .filter(Boolean)
      .join(',');
    if (activeModels.length > 0) {
      getVramEstimate(activeModels)
        .then((est) => (vramEstimate = est))
        .catch(() => {});
    } else {
      vramEstimate = null;
    }
  });

  function emitChange() {
    onchange?.({
      vocalModel,
      stemModel,
      drumsModel,
      bassModel,
      otherModel,
      vocalOverlap,
    });
  }

  function handleModelChange(category: string, value: string) {
    switch (category) {
      case 'vocal': vocalModel = value; break;
      case 'stems': stemModel = value; break;
      case 'drums': drumsModel = value; break;
      case 'bass': bassModel = value; break;
      case 'other': otherModel = value; break;
    }
    emitChange();
  }

  function handleOverlapChange(e: Event) {
    vocalOverlap = parseInt((e.target as HTMLInputElement).value);
    emitChange();
  }

  function toggleExpanded() {
    expanded = !expanded;
  }

  function modelsByCategory(cat: string): LocalModel[] {
    return localModels.filter((m) => m.category === cat);
  }

  const categories = ['Vocal', 'Stems', 'Drums', 'Bass', 'Other'];
  const stateMap: Record<string, { get: () => string; set: (v: string) => void }> = {
    Vocal: { get: () => vocalModel, set: (v) => handleModelChange('vocal', v) },
    Stems: { get: () => stemModel, set: (v) => handleModelChange('stems', v) },
    Drums: { get: () => drumsModel, set: (v) => handleModelChange('drums', v) },
    Bass: { get: () => bassModel, set: (v) => handleModelChange('bass', v) },
    Other: { get: () => otherModel, set: (v) => handleModelChange('other', v) },
  };

  const vramPercent = $derived(
    vramEstimate && vramEstimate.available_vram_mb > 0
      ? (vramEstimate.total_vram_mb / vramEstimate.available_vram_mb) * 100
      : 0,
  );
  const fits = $derived(
    vramEstimate
      ? vramEstimate.total_vram_mb <= vramEstimate.available_vram_mb
      : true,
  );
</script>

<div class="config-card">
  <button class="toggle-btn" onclick={toggleExpanded} disabled={disabled}>
    <span class="toggle-icon">{expanded ? '▼' : '▶'}</span>
    ⚙️ Configuración avanzada
  </button>

  {#if expanded}
    <div class="config-body">
      <!-- Model selection -->
      {#each categories as cat}
        <div class="field">
          <label class="label">{cat}</label>
          <select
            class="select"
            value={stateMap[cat].get()}
            onchange={(e) => stateMap[cat].set((e.target as HTMLSelectElement).value)}
            disabled={disabled}
          >
            <option value="">-- Auto --</option>
            {#each modelsByCategory(cat.toLowerCase()) as model}
              <option value={model.name}>{model.name} ({model.size_mb} MB)</option>
            {/each}
          </select>
        </div>
      {/each}

      <!-- Overlap -->
      <div class="field">
        <label class="label">Vocal Overlap: <strong>{vocalOverlap}</strong></label>
        <input
          type="range"
          min="1"
          max="16"
          step="1"
          value={vocalOverlap}
          oninput={handleOverlapChange}
          disabled={disabled}
          class="slider"
        />
        <div class="range-labels">
          <span>1</span><span>16</span>
        </div>
      </div>

      <!-- VRAM estimate -->
      {#if vramEstimate}
        <div class="vram-section">
          <div class="vram-text" class:red={!fits} class:green={fits}>
            Estimado: {vramEstimate.total_vram_mb} MB / Disponible: {vramEstimate.available_vram_mb} MB
          </div>
          <div class="vram-bar">
            <div
              class="vram-fill"
              class:red={!fits}
              class:green={fits}
              style="width: {Math.min(vramPercent, 100)}%"
            ></div>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .config-card {
    width: 100%;
    display: flex;
    flex-direction: column;
  }

  .toggle-btn {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 8px;
    color: #e0e0e0;
    font-size: 0.95rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
  }
  .toggle-btn:hover:not(:disabled) {
    background: #22223a;
  }
  .toggle-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .toggle-icon {
    font-size: 0.7rem;
    width: 12px;
  }

  .config-body {
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-top: none;
    border-radius: 0 0 8px 8px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .label {
    font-size: 0.8rem;
    font-weight: 600;
    color: #a0a0c0;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .select {
    background: #0a0a14;
    border: 1px solid #2a2a4a;
    border-radius: 6px;
    color: #e0e0e0;
    padding: 0.45rem 0.6rem;
    font-size: 0.85rem;
    outline: none;
  }
  .select:focus {
    border-color: #00d4ff;
  }

  .slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 6px;
    background: #2a2a4a;
    border-radius: 3px;
    outline: none;
  }
  .slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #00d4ff;
    cursor: pointer;
  }
  .range-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.65rem;
    color: #606080;
  }

  .vram-section {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .vram-text {
    font-size: 0.8rem;
    font-weight: 500;
  }
  .vram-text.green {
    color: #4caf50;
  }
  .vram-text.red {
    color: #f44336;
  }

  .vram-bar {
    width: 100%;
    height: 8px;
    background: #0a0a14;
    border-radius: 4px;
    overflow: hidden;
  }
  .vram-fill {
    height: 100%;
    border-radius: 4px;
    transition: width 0.3s ease;
  }
  .vram-fill.green {
    background: linear-gradient(90deg, #4caf50, #81c784);
  }
  .vram-fill.red {
    background: linear-gradient(90deg, #f44336, #e57373);
  }
</style>
