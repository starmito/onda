<script lang="ts">
  import { getModelConfig, setModelConfig, type ModelConfigResponse } from './api';

  interface Props {
    onclose?: () => void;
  }

  let { onclose }: Props = $props();

  // ---- State ----
  let segmentSize = $state(256);
  let overlap = $state(0.25);
  let chunkSize = $state(0);
  let batchSize = $state(0);
  let device = $state('cuda');
  let feedback = $state('');
  let feedbackType = $state<'success' | 'error'>('success');
  let loading = $state(true);

  // Load config on mount
  $effect(() => {
    getModelConfig()
      .then((cfg) => {
        segmentSize = cfg.segment_size;
        overlap = cfg.overlap;
        chunkSize = cfg.chunk_size;
        batchSize = cfg.batch_size;
        device = cfg.device;
        loading = false;
      })
      .catch(() => {
        // Use defaults on error
        loading = false;
      });
  });

  async function handleApply() {
    const cfg: ModelConfigResponse = {
      segment_size: segmentSize,
      overlap,
      chunk_size: chunkSize,
      batch_size: batchSize,
      device,
    };
    try {
      await setModelConfig(cfg);
      feedback = '✅ Configuración guardada';
      feedbackType = 'success';
    } catch (e: any) {
      feedback = `❌ Error: ${e.message}`;
      feedbackType = 'error';
    }
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
        <h2>⚙️ Modelos</h2>
        <button class="btn-close" onclick={onclose}>✕</button>
      </div>

      <div class="panel-body">
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
          <div class="range-labels">
            <span>64</span><span>1024</span>
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
          <div class="range-labels">
            <span>0.0</span><span>0.5</span>
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
          <div class="range-labels">
            <span>auto</span><span>4096</span>
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
          <div class="range-labels">
            <span>auto</span><span>32</span>
          </div>
        </div>

        <!-- Device -->
        <div class="field">
          <label for="device">Device:</label>
          <select id="device" bind:value={device}>
            <option value="cuda">cuda</option>
            <option value="cpu">cpu</option>
          </select>
        </div>

        <button class="btn-apply" onclick={handleApply}>
          Aplicar
        </button>

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
  }

  .btn-close {
    background: none;
    border: none;
    color: #666;
    font-size: 1.1rem;
    cursor: pointer;
    padding: 0.25rem 0.5rem;
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

  .range-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.65rem;
    color: #606080;
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
  }
  .field select:focus {
    border-color: #00d4ff;
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
