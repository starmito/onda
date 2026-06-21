<script lang="ts">
  import {
    applyCompressor,
    applyReverb,
    applyDelay,
    applyChorus,
    applyFlanger,
    applyPhaser,
    applyTremolo,
    applyNoiseGate,
  } from './api';

  interface Props {
    activeFile: string | null;
  }

  let { activeFile }: Props = $props();

  let loading = $state<Record<string, boolean>>({});
  let result = $state<Record<string, string>>({});
  let expanded = $state<Record<string, boolean>>({
    compressor: true,
  });

  const effects = [
    {
      id: 'compressor',
      name: 'Compressor',
      params: [
        { key: 'threshold', label: 'Threshold', min: -60, max: 0, step: 1, default: -20, unit: 'dB' },
        { key: 'ratio', label: 'Ratio', min: 1, max: 20, step: 0.5, default: 4, unit: ':1' },
        { key: 'attack', label: 'Attack', min: 0.1, max: 100, step: 0.1, default: 5, unit: 'ms' },
        { key: 'release', label: 'Release', min: 10, max: 1000, step: 1, default: 50, unit: 'ms' },
        { key: 'makeup', label: 'Makeup', min: 0, max: 20, step: 0.5, default: 0, unit: 'dB' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyCompressor({
          file,
          threshold: values.threshold,
          ratio: values.ratio,
          attack: values.attack,
          release: values.release,
          makeup: values.makeup,
        });
        return resp.file;
      },
    },
    {
      id: 'reverb',
      name: 'Reverb',
      params: [
        { key: 'room_size', label: 'Room size', min: 0, max: 1, step: 0.01, default: 0.5, unit: '' },
        { key: 'decay', label: 'Decay', min: 0.1, max: 10, step: 0.1, default: 2, unit: 's' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyReverb({
          file,
          room_size: values.room_size,
          decay: values.decay,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'delay',
      name: 'Delay',
      params: [
        { key: 'delay_time', label: 'Delay time', min: 0.01, max: 5, step: 0.01, default: 0.3, unit: 's' },
        { key: 'feedback', label: 'Feedback', min: 0, max: 100, step: 1, default: 30, unit: '%' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyDelay({
          file,
          delay_time: values.delay_time,
          feedback: values.feedback,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'chorus',
      name: 'Chorus',
      params: [
        { key: 'depth', label: 'Depth', min: 0, max: 10, step: 0.5, default: 5, unit: '' },
        { key: 'rate', label: 'Rate', min: 0.1, max: 10, step: 0.1, default: 1.5, unit: 'Hz' },
        { key: 'delay_ms', label: 'Delay', min: 10, max: 100, step: 1, default: 25, unit: 'ms' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyChorus({
          file,
          depth: values.depth,
          rate: values.rate,
          delay_ms: values.delay_ms,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'flanger',
      name: 'Flanger',
      params: [
        { key: 'depth', label: 'Depth', min: 0, max: 10, step: 0.5, default: 5, unit: '' },
        { key: 'rate', label: 'Rate', min: 0.1, max: 10, step: 0.1, default: 1.5, unit: 'Hz' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyFlanger({
          file,
          depth: values.depth,
          rate: values.rate,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'phaser',
      name: 'Phaser',
      params: [
        { key: 'depth', label: 'Depth', min: 0, max: 10, step: 0.5, default: 5, unit: '' },
        { key: 'rate', label: 'Rate', min: 0.1, max: 10, step: 0.1, default: 1.5, unit: 'Hz' },
        { key: 'wet_dry', label: 'Wet/Dry', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyPhaser({
          file,
          depth: values.depth,
          rate: values.rate,
          wet_dry: values.wet_dry,
        });
        return resp.file;
      },
    },
    {
      id: 'tremolo',
      name: 'Tremolo',
      params: [
        { key: 'speed', label: 'Speed', min: 0.1, max: 20, step: 0.1, default: 5, unit: 'Hz' },
        { key: 'depth', label: 'Depth', min: 0, max: 100, step: 1, default: 50, unit: '%' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyTremolo({
          file,
          speed: values.speed,
          depth: values.depth,
        });
        return resp.file;
      },
    },
    {
      id: 'noisegate',
      name: 'Noise Gate',
      params: [
        { key: 'threshold', label: 'Threshold', min: -80, max: 0, step: 1, default: -40, unit: 'dB' },
        { key: 'attack', label: 'Attack', min: 0.1, max: 100, step: 0.1, default: 5, unit: 'ms' },
        { key: 'release', label: 'Release', min: 10, max: 1000, step: 1, default: 50, unit: 'ms' },
      ],
      apply: async (file: string, values: Record<string, number>) => {
        const resp = await applyNoiseGate({
          file,
          threshold: values.threshold,
          attack: values.attack,
          release: values.release,
        });
        return resp.file;
      },
    },
  ];

  let values = $state<Record<string, Record<string, number>>>({});

  // Initialize default values
  for (const effect of effects) {
    if (!values[effect.id]) {
      values[effect.id] = {};
    }
    for (const param of effect.params) {
      values[effect.id][param.key] = param.default;
    }
  }

  async function handleApply(effectId: string) {
    if (!activeFile) return;
    const effect = effects.find((e) => e.id === effectId);
    if (!effect) return;

    loading[effectId] = true;
    result[effectId] = '';
    try {
      const outputFile = await effect.apply(activeFile, values[effectId]);
      result[effectId] = `Aplicado: ${outputFile}`;
    } catch (err) {
      result[effectId] = `Error: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      loading[effectId] = false;
    }
  }

  function toggleExpanded(effectId: string) {
    expanded[effectId] = !expanded[effectId];
  }

  function updateValue(effectId: string, key: string, value: number) {
    if (!values[effectId]) values[effectId] = {};
    values[effectId][key] = value;
  }
</script>

<div class="basic-effects-panel">
  <h3>Efectos DSP</h3>

  {#if !activeFile}
    <div class="empty-state">Carga una pista de audio para aplicar efectos.</div>
  {/if}

  <div class="effects-grid">
    {#each effects as effect (effect.id)}
      <div class="effect-card" class:disabled={!activeFile}>
        <button
          class="effect-header"
          onclick={() => toggleExpanded(effect.id)}
          disabled={!activeFile}
        >
          <span class="effect-name">{effect.name}</span>
          <span class="toggle-icon">{expanded[effect.id] ? '▼' : '▶'}</span>
        </button>

        {#if expanded[effect.id]}
          <div class="effect-body">
            {#each effect.params as param (param.key)}
              <label class="param-row">
                <span class="param-label">{param.label}</span>
                <input
                  type="range"
                  min={param.min}
                  max={param.max}
                  step={param.step}
                  value={values[effect.id]?.[param.key] ?? param.default}
                  oninput={(e) => updateValue(effect.id, param.key, parseFloat(e.currentTarget.value))}
                  disabled={!activeFile}
                />
                <span class="param-value">
                  {values[effect.id]?.[param.key] ?? param.default}{param.unit}
                </span>
              </label>
            {/each}

            <button
              class="btn-primary apply-btn"
              onclick={() => handleApply(effect.id)}
              disabled={!activeFile || loading[effect.id]}
            >
              {loading[effect.id] ? 'Aplicando...' : 'Aplicar'}
            </button>

            {#if result[effect.id]}
              <div class="result" class:error={result[effect.id].startsWith('Error')}>
                {result[effect.id]}
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .basic-effects-panel {
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

  .basic-effects-panel h3 {
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

  .effects-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 1rem;
  }

  .effect-card {
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--bg);
    overflow: hidden;
  }

  .effect-card.disabled {
    opacity: 0.6;
  }

  .effect-header {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    background: transparent;
    border: none;
    color: var(--text-primary);
    font-weight: 600;
    font-size: 0.9rem;
    cursor: pointer;
    text-align: left;
  }

  .effect-header:hover {
    background: var(--bg-hover);
  }

  .effect-header:disabled {
    cursor: not-allowed;
  }

  .toggle-icon {
    font-size: 0.7rem;
    color: var(--text-secondary);
  }

  .effect-body {
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    padding: 0.75rem 1rem 1rem;
    border-top: 1px solid var(--border);
  }

  .param-row {
    display: grid;
    grid-template-columns: 80px 1fr 60px;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .param-label {
    font-weight: 600;
  }

  .param-row input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }

  .param-value {
    text-align: right;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .btn-primary {
    padding: 0.5rem 0.9rem;
    border-radius: 8px;
    border: 1px solid var(--accent);
    background: var(--accent);
    color: #fff;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn-primary:hover:not(:disabled) {
    background: var(--accent-dark);
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .apply-btn {
    margin-top: 0.25rem;
  }

  .result {
    font-size: 0.8rem;
    color: var(--accent-light);
    word-break: break-all;
  }

  .result.error {
    color: #e57373;
  }
</style>
