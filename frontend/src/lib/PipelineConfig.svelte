<script lang="ts">
  let {
    disabled = false,
    onstart,
  }: {
    disabled?: boolean;
    onstart?: (config: PipelineConfig) => void;
  } = $props();

  export interface PipelineConfig {
    viperx: boolean;
    viperxKeep: 'both' | 'vocals' | 'instrumental';
    demucs: boolean;
    demucsKeep: string[];
  }

  let viperx = $state(false);
  let viperxKeep = $state<'both' | 'vocals' | 'instrumental'>('both');
  let demucs = $state(false);
  let demucsKeep = $state<string[]>(['drums', 'bass', 'other', 'vocals']);

  function toggleDemucsStem(stem: string) {
    if (demucsKeep.includes(stem)) {
      demucsKeep = demucsKeep.filter((s) => s !== stem);
    } else {
      demucsKeep = [...demucsKeep, stem];
    }
  }

  function canStart(): boolean {
    return (viperx && viperxKeep) || (demucs && demucsKeep.length > 0);
  }

  function handleStart() {
    if (!canStart()) return;
    onstart?.({
      viperx,
      viperxKeep,
      demucs,
      demucsKeep,
    });
  }
</script>

<div class="pipeline-config">
  <h3 class="config-title">⚙️ Pipeline Configuration</h3>

  <!-- ViperX -->
  <label class="pipeline-check">
    <input type="checkbox" bind:checked={viperx} disabled={disabled} />
    <span class="check-label">ViperX</span>
    <span class="check-desc">Voice/Instrument separation</span>
  </label>

  {#if viperx}
    <div class="sub-options">
      <select bind:value={viperxKeep} disabled={disabled} class="sub-select">
        <option value="both">Both (Vocals + Instrumental)</option>
        <option value="vocals">Vocals Only</option>
        <option value="instrumental">Instrumental Only</option>
      </select>
    </div>
  {/if}

  <!-- HTDemucs -->
  <label class="pipeline-check">
    <input type="checkbox" bind:checked={demucs} disabled={disabled} />
    <span class="check-label">HTDemucs</span>
    <span class="check-desc">4-stem separation</span>
  </label>

  {#if demucs}
    <div class="sub-options stems-grid">
      {#each ['drums', 'bass', 'other', 'vocals'] as stem}
        <label class="stem-check">
          <input
            type="checkbox"
            checked={demucsKeep.includes(stem)}
            disabled={disabled}
            onchange={() => toggleDemucsStem(stem)}
          />
          <span class="stem-emoji">
            {#if stem === 'drums'}🥁
            {:else if stem === 'bass'}🎸
            {:else if stem === 'other'}🎹
            {:else}🎤
            {/if}
          </span>
          {stem}
        </label>
      {/each}
    </div>
  {/if}

  <button
    class="start-btn"
    disabled={disabled || !canStart()}
    onclick={handleStart}
  >
    ▶ START Pipeline
  </button>
</div>

<style>
  .pipeline-config {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1rem;
    background: #1a1a2e;
    border-radius: 8px;
  }

  .config-title {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: #e0e0e0;
  }

  .pipeline-check {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    padding: 0.5rem;
    border-radius: 6px;
    transition: background 0.15s;
  }
  .pipeline-check:hover {
    background: #22223a;
  }
  .pipeline-check input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 16px;
    height: 16px;
    cursor: pointer;
  }

  .check-label {
    font-weight: 600;
    color: #00d4ff;
    font-size: 0.9rem;
  }
  .check-desc {
    font-size: 0.75rem;
    color: #777;
    margin-left: auto;
  }

  .sub-options {
    padding-left: 2.25rem;
    animation: fadeIn 0.2s ease;
  }

  .sub-select {
    width: 100%;
    padding: 0.4rem 0.6rem;
    background: #111;
    color: #e0e0e0;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.85rem;
    cursor: pointer;
    outline: none;
  }
  .sub-select:focus {
    border-color: #00d4ff;
  }

  .stems-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.4rem;
  }

  .stem-check {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.85rem;
    color: #ccc;
    cursor: pointer;
    padding: 0.25rem 0.4rem;
    border-radius: 4px;
    transition: background 0.15s;
    text-transform: capitalize;
  }
  .stem-check:hover {
    background: #22223a;
  }
  .stem-check input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 14px;
    height: 14px;
    cursor: pointer;
  }
  .stem-emoji {
    font-size: 0.9rem;
  }

  .start-btn {
    width: 100%;
    padding: 0.65rem;
    background: linear-gradient(135deg, #00d4ff, #00a8cc);
    color: #0a0a14;
    border: none;
    border-radius: 8px;
    font-size: 1rem;
    font-weight: 700;
    cursor: pointer;
    transition: opacity 0.2s, transform 0.15s;
    letter-spacing: 0.5px;
  }
  .start-btn:hover:not(:disabled) {
    opacity: 0.9;
    transform: scale(1.01);
  }
  .start-btn:disabled {
    background: #333;
    color: #666;
    cursor: not-allowed;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(-4px); }
    to { opacity: 1; transform: translateY(0); }
  }
</style>
