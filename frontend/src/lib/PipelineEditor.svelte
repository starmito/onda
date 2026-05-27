<script lang="ts">
  import type { LocalModel } from './api';
  import { getLocalModels } from './api';

  // ── Props ──
  let {
    disabled = false,
    onstart,
  }: {
    disabled?: boolean;
    onstart?: (config: any) => void;
  } = $props();

  // ── Model catalogue ──
  // ViperX models grouped by category
  interface ModelOption {
    name: string;
    category: string;
  }

  const viperxDefault: ModelOption[] = [
    { name: 'MelBand_Karaoke', category: 'Roformer' },
    { name: 'BS_Roformer_Viperx', category: 'Roformer' },
    { name: 'BS_PolarFormer', category: 'VR_Arch' },
  ];

  const demucsDefault: ModelOption[] = [
    { name: 'htdemucs_ft', category: 'Demucs' },
    { name: 'htdemucs_6s', category: 'Demucs' },
    { name: 'Kim_Vocal_2', category: 'MDX' },
  ];

  // ── State ──
  let viperxModels = $state<ModelOption[]>(viperxDefault);
  let demucsModels = $state<ModelOption[]>(demucsDefault);
  let viperxModel = $state('BS_Roformer_Viperx');
  let demucsModel = $state('htdemucs_ft');
  let modelsLoaded = $state(false);

  // Step toggles
  let viperxEnabled = $state(true);
  let demucsEnabled = $state(true);

  // Stem checkboxes per step
  let viperxStems = $state({ vocals: true, instrumental: true });
  let demucsStems = $state({ drums: true, bass: true, other: true, vocals: true });

  // ── Auto-detect: ViperX vocals → disable Demucs vocals ──
  let demucsVocalsAutoDisabled = $derived(
    viperxEnabled && viperxStems.vocals
  );

  // Sync: when auto-disabled, uncheck Demucs vocals
  $effect(() => {
    if (demucsVocalsAutoDisabled && demucsStems.vocals) {
      demucsStems = { ...demucsStems, vocals: false };
    }
  });

  // ── Load real model list from backend ──
  $effect(() => {
    getLocalModels()
      .then((res) => {
        if (res.models && res.models.length > 0) {
          // Filter ViperX models: Roformer + VR_Arch
          const vxModels = res.models
            .filter((m: LocalModel) =>
              m.category === 'Roformer' ||
              m.category === 'Roformer/MelBand' ||
              m.category === 'VR_Arch'
            )
            .map((m: LocalModel) => ({ name: m.name, category: m.category }));
          if (vxModels.length > 0) {
            viperxModels = vxModels;
            // Select default if available
            const found = vxModels.find((m) => m.name === 'BS_Roformer_Viperx');
            if (found) viperxModel = found.name;
          }

          // Filter Demucs models: Demucs + MDX
          const dmModels = res.models
            .filter((m: LocalModel) =>
              m.category === 'Demucs' || m.category === 'MDX'
            )
            .map((m: LocalModel) => ({ name: m.name, category: m.category }));
          if (dmModels.length > 0) {
            demucsModels = dmModels;
            const found = dmModels.find((m) => m.name === 'htdemucs_ft');
            if (found) demucsModel = found.name;
          }
        }
        modelsLoaded = true;
      })
      .catch(() => {
        // Keep defaults
        modelsLoaded = true;
      });
  });

  // ── Group models by category for optgroup rendering ──
  function groupByCategory(models: ModelOption[]): Map<string, ModelOption[]> {
    const map = new Map<string, ModelOption[]>();
    for (const m of models) {
      const cat = m.category;
      if (!map.has(cat)) map.set(cat, []);
      map.get(cat)!.push(m);
    }
    return map;
  }

  const viperxGroups = $derived(groupByCategory(viperxModels));
  const demucsGroups = $derived(groupByCategory(demucsModels));
</script>

<div class="editor-card">
  <h2 class="editor-title">🎛 Editor de Pipeline</h2>

  <!-- ViperX Model Selector -->
  <div class="section">
    <label class="label" for="viperx-model">Modelo ViperX</label>
    <select
      id="viperx-model"
      class="select"
      bind:value={viperxModel}
      disabled={disabled}
    >
      {#each [...viperxGroups.entries()] as [cat, models]}
        <optgroup label={cat}>
          {#each models as m}
            <option value={m.name}>{m.name}</option>
          {/each}
        </optgroup>
      {/each}
    </select>
  </div>

  <!-- Demucs Model Selector -->
  <div class="section">
    <label class="label" for="demucs-model">Modelo Demucs</label>
    <select
      id="demucs-model"
      class="select"
      bind:value={demucsModel}
      disabled={disabled}
    >
      {#each [...demucsGroups.entries()] as [cat, models]}
        <optgroup label={cat}>
          {#each models as m}
            <option value={m.name}>{m.name}</option>
          {/each}
        </optgroup>
      {/each}
    </select>
  </div>

  <!-- Step toggles + stems -->
  <div class="section">
    <label class="step-row">
      <input
        type="checkbox"
        bind:checked={viperxEnabled}
        disabled={disabled}
      />
      <span class="step-label">Paso 1: ViperX (separación vocal)</span>
    </label>
    <div class="step-options" class:disabled-step={!viperxEnabled}>
      <label class="chip">
        <input type="checkbox" bind:checked={viperxStems.vocals} disabled={disabled || !viperxEnabled} />
        🎤 Vocals
      </label>
      <label class="chip">
        <input type="checkbox" bind:checked={viperxStems.instrumental} disabled={disabled || !viperxEnabled} />
        🎵 Instrumental
      </label>
    </div>
  </div>

  <div class="section">
    <label class="step-row">
      <input
        type="checkbox"
        bind:checked={demucsEnabled}
        disabled={disabled}
      />
      <span class="step-label">Paso 2: Demucs (separación stems)</span>
    </label>
    <div class="step-options" class:disabled-step={!demucsEnabled}>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsStems.drums} disabled={disabled || !demucsEnabled} />
        🥁 Drums
      </label>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsStems.bass} disabled={disabled || !demucsEnabled} />
        🎸 Bass
      </label>
      <label class="chip">
        <input type="checkbox" bind:checked={demucsStems.other} disabled={disabled || !demucsEnabled} />
        🎹 Other
      </label>
      <label class="chip" title={demucsVocalsAutoDisabled ? 'ViperX ya separa las vocals' : ''}>
        <input
          type="checkbox"
          bind:checked={demucsStems.vocals}
          disabled={disabled || !demucsEnabled || demucsVocalsAutoDisabled}
        />
        🎤 Vocals
        {#if demucsVocalsAutoDisabled}
          <span class="tooltip-icon" title="ViperX ya separa las vocals">ⓘ</span>
        {/if}
      </label>
    </div>
  </div>
</div>

<style>
  .editor-card {
    width: 100%;
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 12px;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .editor-title {
    margin: 0;
    font-size: 1rem;
    font-weight: 700;
    color: #00d4ff;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .label {
    font-size: 0.85rem;
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
    padding: 0.5rem 0.75rem;
    font-size: 0.9rem;
    outline: none;
    transition: border-color 0.15s;
  }
  .select:focus {
    border-color: #00d4ff;
  }
  .select optgroup {
    background: #0a0a14;
    color: #a0a0c0;
    font-weight: 600;
    font-style: normal;
  }
  .select option {
    background: #1a1a2e;
    color: #e0e0e0;
    padding: 0.3rem;
  }

  .step-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    font-size: 0.95rem;
    color: #e0e0e0;
  }
  .step-row input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 16px;
    height: 16px;
  }
  .step-label {
    font-weight: 500;
  }

  .step-options {
    margin-left: 1.5rem;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    transition: opacity 0.2s;
  }
  .step-options.disabled-step {
    opacity: 0.35;
    pointer-events: none;
  }

  .chip {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    background: #0a0a14;
    border: 1px solid #2a2a4a;
    border-radius: 20px;
    padding: 0.3rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
    color: #c0c0d0;
  }
  .chip input[type="checkbox"] {
    accent-color: #00d4ff;
    width: 14px;
    height: 14px;
  }

  .tooltip-icon {
    font-size: 0.75rem;
    color: #00d4ff;
    cursor: help;
    margin-left: 0.15rem;
  }
</style>
