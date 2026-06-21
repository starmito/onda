<script lang="ts">
  import { applyEQ } from './api';
  import type { EqFilter } from './api';

  interface Props {
    activeFile: string | null;
  }

  let { activeFile }: Props = $props();

  const MIN_FREQ = 20;
  const MAX_FREQ = 20000;
  const LOG_MIN = Math.log10(MIN_FREQ);
  const LOG_MAX = Math.log10(MAX_FREQ);

  const filterTypes: EqFilter['type'][] = ['peak', 'lowshelf', 'highshelf', 'lowpass', 'highpass'];

  let bands = $state<EqFilter[]>(
    Array.from({ length: 4 }, (_, i) => ({
      type: 'peak',
      freq: [80, 250, 1000, 4000][i],
      gain: 0,
      q: 1,
    }))
  );

  let loading = $state(false);
  let result = $state('');
  let filtersApplied = $state(0);

  function freqToPosition(freq: number): number {
    return (Math.log10(freq) - LOG_MIN) / (LOG_MAX - LOG_MIN);
  }

  function positionToFreq(pos: number): number {
    const clamped = Math.max(0, Math.min(1, pos));
    const logVal = LOG_MIN + clamped * (LOG_MAX - LOG_MIN);
    return Math.round(Math.pow(10, logVal));
  }

  function formatFreq(freq: number): string {
    if (freq >= 1000) return `${(freq / 1000).toFixed(1)}k`;
    return `${freq}`;
  }

  function updateBandType(index: number, type: EqFilter['type']) {
    bands[index].type = type;
  }

  function updateBandFreq(index: number, position: number) {
    bands[index].freq = positionToFreq(position);
  }

  function updateBandGain(index: number, gain: number) {
    bands[index].gain = gain;
  }

  function updateBandQ(index: number, q: number) {
    bands[index].q = q;
  }

  async function handleApply() {
    if (!activeFile) return;
    loading = true;
    result = '';
    try {
      const resp = await applyEQ({ file: activeFile, filters: bands });
      filtersApplied = resp.filters_applied;
      result = `EQ aplicado: ${resp.file}`;
    } catch (err) {
      filtersApplied = 0;
      result = `Error: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      loading = false;
    }
  }

  function needsGain(type: string): boolean {
    return type === 'peak' || type === 'lowshelf' || type === 'highshelf';
  }
</script>

<div class="basic-eq-panel">
  <h3>EQ Paramétrico</h3>

  {#if !activeFile}
    <div class="empty-state">Carga una pista de audio para aplicar EQ.</div>
  {/if}

  <div class="bands-list" class:disabled={!activeFile}>
    {#each bands as band, i (i)}
      <div class="band-card">
        <div class="band-header">Banda {i + 1}</div>

        <label class="field-row">
          <span>Tipo</span>
          <select
            value={band.type}
            onchange={(e) => updateBandType(i, e.currentTarget.value as EqFilter['type'])}
            disabled={!activeFile}
          >
            {#each filterTypes as t (t)}
              <option value={t}>{t}</option>
            {/each}
          </select>
        </label>

        <label class="field-row">
          <span>Frecuencia</span>
          <input
            type="range"
            min="0"
            max="1"
            step="0.001"
            value={freqToPosition(band.freq)}
            oninput={(e) => updateBandFreq(i, parseFloat(e.currentTarget.value))}
            disabled={!activeFile}
          />
          <span class="value">{formatFreq(band.freq)} Hz</span>
        </label>

        {#if needsGain(band.type)}
          <label class="field-row">
            <span>Gain</span>
            <input
              type="range"
              min="-24"
              max="24"
              step="0.5"
              value={band.gain}
              oninput={(e) => updateBandGain(i, parseFloat(e.currentTarget.value))}
              disabled={!activeFile}
            />
            <span class="value">{band.gain.toFixed(1)} dB</span>
          </label>
        {/if}

        <label class="field-row">
          <span>Q</span>
          <input
            type="range"
            min="0.1"
            max="10"
            step="0.1"
            value={band.q}
            oninput={(e) => updateBandQ(i, parseFloat(e.currentTarget.value))}
            disabled={!activeFile}
          />
          <span class="value">{band.q.toFixed(1)}</span>
        </label>
      </div>
    {/each}
  </div>

  <button
    class="btn-primary apply-btn"
    onclick={handleApply}
    disabled={!activeFile || loading}
  >
    {loading ? 'Aplicando EQ...' : 'Aplicar EQ'}
  </button>

  {#if filtersApplied > 0}
    <div class="filters-applied">Filtros aplicados: {filtersApplied}</div>
  {/if}

  {#if result}
    <div class="result" class:error={result.startsWith('Error')}>
      {result}
    </div>
  {/if}
</div>

<style>
  .basic-eq-panel {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    padding: 1rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    height: 100%;
    min-height: 0;
    overflow-y: auto;
  }

  .basic-eq-panel h3 {
    margin: 0;
    font-size: 1rem;
    color: var(--text-primary);
  }

  .empty-state {
    padding: 1rem;
    text-align: center;
    color: var(--text-secondary);
    background: var(--bg);
    border-radius: 8px;
  }

  .bands-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 1rem;
  }

  .bands-list.disabled {
    opacity: 0.6;
  }

  .band-card {
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--bg);
    padding: 0.75rem 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }

  .band-header {
    font-weight: 600;
    color: var(--text-primary);
    font-size: 0.9rem;
  }

  .field-row {
    display: grid;
    grid-template-columns: 70px 1fr 70px;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .field-row span:first-child {
    font-weight: 600;
  }

  .field-row input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }

  .field-row select {
    grid-column: span 2;
    padding: 0.35rem 0.5rem;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.85rem;
  }

  .value {
    text-align: right;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .btn-primary {
    padding: 0.6rem 1rem;
    border-radius: 8px;
    border: 1px solid var(--accent);
    background: var(--accent);
    color: #fff;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    align-self: flex-start;
  }

  .btn-primary:hover:not(:disabled) {
    background: var(--accent-dark);
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .filters-applied {
    font-size: 0.85rem;
    color: var(--accent-light);
    font-weight: 600;
  }

  .result {
    font-size: 0.85rem;
    color: var(--accent-light);
    word-break: break-all;
  }

  .result.error {
    color: #e57373;
  }
</style>
