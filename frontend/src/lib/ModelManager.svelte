<script lang="ts">
  import { getModelConfig, setModelConfig, getLocalModels, type ModelConfigResponse, type LocalModel } from './api';

  interface Props {
    onclose?: () => void;
    initialModel?: string;
  }

  let { onclose, initialModel }: Props = $props();

  // ---- State ----
  let models = $state<LocalModel[]>([]);
  let selectedModel = $state('');
  let segmentSize = $state(256);
  let overlap = $state(0.25);
  let chunkSize = $state(0);
  let batchSize = $state(0);
  let device = $state('cuda');
  let feedback = $state('');
  let feedbackType = $state<'success' | 'error'>('success');
  let loading = $state(true);
  let saving = $state(false);

  // Group models by category for optgroup
  let groupedModels = $derived.by(() => {
    const groups: Record<string, LocalModel[]> = {};
    for (const m of models) {
      const cat = m.category || 'Other';
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(m);
    }
    // Sort categories
    const order = ['Roformer', 'Roformer/MelBand', 'MDX', 'SCnet', 'Demucs', 'VR_Arch', 'Other'];
    const sorted: { category: string; models: LocalModel[] }[] = [];
    for (const cat of order) {
      if (groups[cat] && groups[cat].length > 0) {
        sorted.push({ category: cat, models: groups[cat] });
        delete groups[cat];
      }
    }
    for (const [cat, m] of Object.entries(groups)) {
      sorted.push({ category: cat, models: m });
    }
    return sorted;
  });

  // Load model list + optionally load config for initialModel
  $effect(() => {
    async function load() {
      try {
        const res = await getLocalModels();
        models = res.models || [];
        if (initialModel && models.some(m => m.name === initialModel)) {
          selectedModel = initialModel;
        }
        // Load config if a model is selected
        if (selectedModel) {
          await loadConfig(selectedModel);
        }
      } catch {
        // Keep defaults on error
      }
      loading = false;
    }
    load();
  });

  async function loadConfig(modelName: string): Promise<void> {
    try {
      const cfg = await getModelConfig(modelName);
      segmentSize = cfg.segment_size;
      overlap = cfg.overlap;
      chunkSize = cfg.chunk_size;
      batchSize = cfg.batch_size;
      device = cfg.device;
    } catch {
      // Use current values as defaults
    }
  }

  async function handleModelSelect(e: Event) {
    const target = e.target as HTMLSelectElement;
    selectedModel = target.value;
    if (selectedModel) {
      await loadConfig(selectedModel);
    }
  }

  async function handleApply() {
    if (!selectedModel) return;
    const cfg: ModelConfigResponse = {
      segment_size: segmentSize,
      overlap,
      chunk_size: chunkSize,
      batch_size: batchSize,
      device,
    };
    saving = true;
    try {
      await setModelConfig(cfg, selectedModel);
      feedback = '✅ Configuración guardada';
      feedbackType = 'success';
    } catch (e: any) {
      feedback = `❌ Error: ${e.message}`;
      feedbackType = 'error';
    }
    saving = false;
    setTimeout(() => (feedback = ''), 3000);
  }

  function formatOverlap(v: number): string {
    return v.toFixed(2);
  }
</script>

{#if loading}
  <div class="backdrop">
    <div class="panel">
      <div class="panel-header">
        <h2>⚙️ Modelos</h2>
        <button class="btn-close" onclick={onclose}>✕</button>
      </div>
      <div class="panel-body loading-text">Cargando...</div>
    </div>
  </div>
{:else}
  <div class="backdrop" onclick={onclose} role="presentation">
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="panel" onclick={(e: MouseEvent) => e.stopPropagation()} role="dialog">
      <div class="panel-header">
        <h2>⚙️ Modelos{selectedModel ? ` — ${selectedModel}` : ''}</h2>
        <button class="btn-close" onclick={onclose}>✕</button>
      </div>

      <div class="panel-body">
        <!-- Model selector -->
        <div class="field">
          <label for="model-select">Modelo:</label>
          <select id="model-select" value={selectedModel} onchange={handleModelSelect}>
            <option value="">-- Seleccionar modelo --</option>
            {#each groupedModels as group}
              <optgroup label={group.category}>
                {#each group.models as m}
                  <option value={m.name}>{m.name}</option>
                {/each}
              </optgroup>
            {/each}
          </select>
          {#if models.length === 0}
            <div class="hint">No se encontraron modelos. Descarga uno primero.</div>
          {/if}
        </div>

        <!-- Sliders (disabled when no model selected) -->
        <fieldset class="sliders" disabled={!selectedModel}>
          <!-- Segment Size -->
          <div class="field">
            <label for="seg-size">
              Segment Size: <strong>{segmentSize}</strong>
            </label>
            <input
              id="seg-size"
              type="range"
              min="64"
              max="1024"
              step="64"
              bind:value={segmentSize}
            />
            <p class="param-desc">Tamaño del segmento de audio procesado. Valores altos = mejor calidad pero más VRAM y más lento.</p>
            <div class="slider-labels">
              <span>⚡ Fast / -VRAM</span>
              <span>🎵 Quality / +VRAM</span>
            </div>
          </div>

          <!-- Overlap -->
          <div class="field">
            <label for="overlap">
              Overlap: <strong>{formatOverlap(overlap)}</strong>
            </label>
            <input
              id="overlap"
              type="range"
              min="0"
              max="0.5"
              step="0.05"
              bind:value={overlap}
            />
            <p class="param-desc">Solapamiento entre segmentos. Más overlap = transiciones más suaves pero más lento y más VRAM.</p>
            <div class="slider-labels">
              <span>⚡ Fast / -VRAM</span>
              <span>🔄 Smooth / +VRAM</span>
            </div>
          </div>

          <!-- Chunk Size -->
          <div class="field">
            <label for="chunk-size">
              Chunk Size: <strong>{chunkSize === 0 ? 'auto' : chunkSize}</strong>
            </label>
            <input
              id="chunk-size"
              type="range"
              min="0"
              max="4096"
              step="256"
              bind:value={chunkSize}
            />
            <p class="param-desc">Tamaño del chunk para procesamiento por lotes. 0 = automático. Valores altos = más VRAM, potencialmente más rápido.</p>
            <div class="slider-labels">
              <span>🤖 Auto</span>
              <span>📦 Large / +VRAM</span>
            </div>
          </div>

          <!-- Batch Size -->
          <div class="field">
            <label for="batch-size">
              Batch Size: <strong>{batchSize === 0 ? 'auto' : batchSize}</strong>
            </label>
            <input
              id="batch-size"
              type="range"
              min="0"
              max="32"
              step="1"
              bind:value={batchSize}
            />
            <p class="param-desc">Número de muestras procesadas en paralelo. Valores altos = más rápido en GPU pero mucha más VRAM. 0 = automático.</p>
            <div class="slider-labels">
              <span>🤖 Auto</span>
              <span>⚡ GPU / ++VRAM</span>
            </div>
          </div>

          <!-- Device -->
          <div class="field">
            <label for="device">Device:</label>
            <select id="device" bind:value={device}>
              <option value="cuda">cuda</option>
              <option value="cpu">cpu</option>
            </select>
            <p class="param-desc">Dispositivo de inferencia. CUDA usa la GPU (más rápido, requiere VRAM). CPU es más lento pero no usa VRAM.</p>
          </div>

          <button class="btn-apply" onclick={handleApply} disabled={saving}>
            {saving ? 'Guardando...' : 'Aplicar'}
          </button>
        </fieldset>

        {#if feedback}
          <div class="feedback" class:success={feedbackType === 'success'} class:error={feedbackType === 'error'}>
            {feedback}
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    left: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 950;
    display: flex;
    justify-content: flex-end;
  }

  .panel {
    width: 340px;
    max-width: 90vw;
    height: 100%;
    background: #1a1a2e;
    border-left: 1px solid #2a2a4a;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    animation: slideIn 0.25s ease;
  }

  @keyframes slideIn {
    from { transform: translateX(100%); }
    to { transform: translateX(0); }
  }

  .panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid #2a2a4a;
  }

  .panel-header h2 {
    margin: 0;
    font-size: 1.1rem;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .btn-close {
    background: none;
    border: none;
    color: #666;
    font-size: 1.1rem;
    cursor: pointer;
    padding: 0.25rem 0.5rem;
    flex-shrink: 0;
  }
  .btn-close:hover {
    color: #e57373;
  }

  .panel-body {
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .loading-text {
    color: #888;
    text-align: center;
    padding-top: 2rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .field label {
    font-size: 0.85rem;
    color: #c0c0d0;
  }

  .field label strong {
    color: #00d4ff;
  }

  .field input[type='range'] {
    width: 100%;
    accent-color: #00d4ff;
    height: 6px;
  }

  .slider-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.7rem;
    color: #666;
  }

  .param-desc {
    font-size: 0.75rem;
    color: #888;
    margin-top: 2px;
    margin-bottom: 4px;
  }

  .field select {
    padding: 0.4rem 0.6rem;
    background: #0e0e1a;
    border: 1px solid #2a2a4a;
    border-radius: 6px;
    color: #e0e0e0;
    font-size: 0.85rem;
    outline: none;
    cursor: pointer;
    width: 100%;
  }
  .field select:focus {
    border-color: #00d4ff;
  }

  .sliders {
    border: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .sliders:disabled {
    opacity: 0.4;
    pointer-events: none;
  }

  .hint {
    font-size: 0.75rem;
    color: #606080;
    margin-top: 0.25rem;
  }

  .btn-apply {
    padding: 0.6rem 1rem;
    background: linear-gradient(135deg, #00d4ff, #b388ff);
    border: none;
    border-radius: 8px;
    color: #0a0a14;
    font-weight: 700;
    font-size: 0.9rem;
    cursor: pointer;
    transition: opacity 0.15s;
  }
  .btn-apply:hover {
    opacity: 0.9;
  }
  .btn-apply:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .feedback {
    text-align: center;
    font-size: 0.85rem;
    font-weight: 600;
    padding: 0.5rem;
    border-radius: 6px;
  }
  .feedback.success {
    background: #1b3a1b;
    color: #81c784;
  }
  .feedback.error {
    background: #3a1b1b;
    color: #e57373;
  }
</style>
