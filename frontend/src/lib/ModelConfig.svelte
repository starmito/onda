<script lang="ts">
  import type { ModelInfo } from './api';

  let {
    models = [],
    config = {
      vocalModel: '',
      stemModel: '',
      drumsModel: '',
      bassModel: '',
      otherModel: '',
      vocalOverlap: 4,
    },
    onchange,
  }: {
    models?: ModelInfo[];
    config?: {
      vocalModel: string;
      stemModel: string;
      drumsModel: string;
      bassModel: string;
      otherModel: string;
      vocalOverlap: number;
    };
    onchange?: (config: {
      vocalModel: string;
      stemModel: string;
      drumsModel: string;
      bassModel: string;
      otherModel: string;
      vocalOverlap: number;
    }) => void;
  } = $props();

  let expanded = $state(false);

  function toggle() {
    expanded = !expanded;
  }

  function emit() {
    onchange?.({ ...config });
  }

  function setModel(key: 'vocalModel' | 'stemModel' | 'drumsModel' | 'bassModel' | 'otherModel', value: string) {
    config[key] = value;
    config = config; // trigger reactivity
    emit();
  }

  function setOverlap(value: number) {
    config.vocalOverlap = value;
    config = config;
    emit();
  }

  function filterModels(categories: string[]): ModelInfo[] {
    return models.filter((m) => categories.includes(m.category));
  }
</script>

<div class="model-config">
  <button class="toggle-header" onclick={toggle}>
    <span class="toggle-arrow">{expanded ? '▼' : '▶'}</span>
    <span class="toggle-title">⚙️ Configuración avanzada</span>
  </button>

  {#if expanded}
    <div class="config-body">
      <label class="field">
        <span class="field-label">Vocal Model</span>
        <select
          class="field-select"
          value={config.vocalModel}
          onchange={(e) => setModel('vocalModel', (e.target as HTMLSelectElement).value)}
        >
          <option value="">(none)</option>
          {#each filterModels(['RoFormer', 'VR']) as model}
            <option value={model.name}>{model.name}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Stem Model</span>
        <select
          class="field-select"
          value={config.stemModel}
          onchange={(e) => setModel('stemModel', (e.target as HTMLSelectElement).value)}
        >
          <option value="">(none)</option>
          {#each filterModels(['Demucs']) as model}
            <option value={model.name}>{model.name}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Drums Model</span>
        <select
          class="field-select"
          value={config.drumsModel}
          onchange={(e) => setModel('drumsModel', (e.target as HTMLSelectElement).value)}
        >
          <option value="">(none)</option>
          {#each filterModels(['Demucs']) as model}
            <option value={model.name}>{model.name}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Bass Model</span>
        <select
          class="field-select"
          value={config.bassModel}
          onchange={(e) => setModel('bassModel', (e.target as HTMLSelectElement).value)}
        >
          <option value="">(none)</option>
          {#each filterModels(['Demucs']) as model}
            <option value={model.name}>{model.name}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Other Model</span>
        <select
          class="field-select"
          value={config.otherModel}
          onchange={(e) => setModel('otherModel', (e.target as HTMLSelectElement).value)}
        >
          <option value="">(none)</option>
          {#each filterModels(['RoFormer', 'VR']) as model}
            <option value={model.name}>{model.name}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Vocal Overlap: {config.vocalOverlap}</span>
        <input
          type="range"
          min="2"
          max="8"
          step="1"
          value={config.vocalOverlap}
          class="field-slider"
          oninput={(e) => setOverlap(Number((e.target as HTMLInputElement).value))}
        />
        <div class="slider-labels">
          <span>2</span>
          <span>4</span>
          <span>6</span>
          <span>8</span>
        </div>
      </label>
    </div>
  {/if}
</div>

<style>
  .model-config {
    background: #1a1a2e;
    border-radius: 8px;
    overflow: hidden;
  }

  .toggle-header {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    background: none;
    border: none;
    color: #e0e0e0;
    font-size: 0.95rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
  }
  .toggle-header:hover {
    background: #22223a;
  }

  .toggle-arrow {
    font-size: 0.75rem;
    color: #00d4ff;
    width: 1rem;
    text-align: center;
    transition: transform 0.2s ease;
  }

  .toggle-title {
    color: #e0e0e0;
  }

  .config-body {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 0 1rem 1rem 1rem;
    animation: fadeIn 0.2s ease;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .field-label {
    font-size: 0.82rem;
    font-weight: 500;
    color: #aaa;
  }

  .field-select {
    padding: 0.4rem 0.6rem;
    background: #111;
    color: #e0e0e0;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.85rem;
    cursor: pointer;
    outline: none;
    transition: border-color 0.2s ease;
  }
  .field-select:hover {
    border-color: #666;
  }
  .field-select:focus {
    border-color: #00d4ff;
    box-shadow: 0 0 8px rgba(0, 212, 255, 0.15);
  }

  .field-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 6px;
    background: linear-gradient(90deg, #00d4ff, #b388ff);
    border-radius: 3px;
    outline: none;
    cursor: pointer;
  }
  .field-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #00d4ff;
    cursor: pointer;
    border: 2px solid #1a1a2e;
    box-shadow: 0 0 6px rgba(0, 212, 255, 0.3);
  }
  .field-slider::-moz-range-thumb {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #00d4ff;
    cursor: pointer;
    border: 2px solid #1a1a2e;
  }

  .slider-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.65rem;
    color: #666;
    padding: 0 2px;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(-4px); }
    to { opacity: 1; transform: translateY(0); }
  }
</style>
