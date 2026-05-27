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

  // ── Build config for onstart ──
  function buildConfig() {
    const vStems: string[] = [];
    if (viperxStems.vocals) vStems.push('vocals');
    if (viperxStems.instrumental) vStems.push('instrumental');

    const dStems: string[] = [];
    if (demucsStems.drums) dStems.push('drums');
    if (demucsStems.bass) dStems.push('bass');
    if (demucsStems.other) dStems.push('other');
    if (demucsStems.vocals) dStems.push('vocals');

    return {
      viperx: viperxEnabled,
      viperxModel,
      viperxStems: vStems,
      demucs: demucsEnabled,
      demucsModel,
      demucsStems: dStems,
    };
  }

  function handleStart() {
    onstart?.(buildConfig());
  }

  // ── SVG graph helpers ──
  const NODE_W = 80;
  const NODE_H = 36;
  const STEM_W = 64;
  const STEM_H = 22;
  const X_GAP = 60;
  const Y_STEP = 32;

  interface GraphNode {
    label: string;
    x: number;
    y: number;
    active: boolean;
  }

  interface GraphStem {
    label: string;
    x: number;
    y: number;
    active: boolean;
  }

  // Compute positions
  const inputNode: GraphNode = { label: 'Input', x: 10, y: 40, active: true };

  const viperxNode = $derived<GraphNode>({
    label: 'ViperX',
    x: inputNode.x + NODE_W + X_GAP,
    y: 40,
    active: viperxEnabled,
  });

  const viperxOut = $derived.by<GraphStem[]>(() => {
    const stems: GraphStem[] = [];
    const baseX = viperxNode.x + NODE_W + 20;
    const baseY = 10;
    if (viperxEnabled) {
      stems.push({ label: 'vocals', x: baseX, y: baseY, active: viperxStems.vocals });
      stems.push({ label: 'instrumental', x: baseX, y: baseY + Y_STEP, active: viperxStems.instrumental });
    }
    return stems;
  });

  const demucsNode = $derived<GraphNode>({
    label: 'Demucs',
    x: viperxNode.x + NODE_W + STEM_W + X_GAP + 20,
    y: 40,
    active: demucsEnabled,
  });

  const demucsOut = $derived.by<GraphStem[]>(() => {
    const stems: GraphStem[] = [];
    const baseX = demucsNode.x + NODE_W + 20;
    const baseY = 8;
    if (demucsEnabled) {
      stems.push({ label: 'drums', x: baseX, y: baseY, active: demucsStems.drums });
      stems.push({ label: 'bass', x: baseX, y: baseY + Y_STEP, active: demucsStems.bass });
      stems.push({ label: 'other', x: baseX, y: baseY + 2 * Y_STEP, active: demucsStems.other });
    }
    return stems;
  });

  const svgWidth = demucsOut.length > 0
    ? demucsOut[demucsOut.length - 1].x + STEM_W + 20
    : demucsNode.x + NODE_W + 20;
  const svgHeight = 80;

  function nodeColor(active: boolean): string {
    return active ? '#00d4ff' : '#3a3a5a';
  }
  function stemColor(active: boolean): string {
    return active ? '#4caf50' : '#3a3a5a';
  }
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

  <!-- SVG Pipeline Graph -->
  <div class="graph-section">
    <svg width="100%" viewBox="0 0 {svgWidth} {svgHeight}" class="pipeline-graph">
      <!-- Arrow: Input → ViperX -->
      <line
        x1={inputNode.x + NODE_W} y1={inputNode.y + NODE_H / 2}
        x2={viperxNode.x} y2={viperxNode.y + NODE_H / 2}
        stroke={nodeColor(viperxEnabled)}
        stroke-width="2"
        marker-end="url(#arrowhead)"
      />

      <!-- Input node -->
      <rect x={inputNode.x} y={inputNode.y} width={NODE_W} height={NODE_H}
        rx="6" fill="#0a0a14" stroke={nodeColor(true)} stroke-width="2" />
      <text x={inputNode.x + NODE_W / 2} y={inputNode.y + NODE_H / 2 + 5}
        text-anchor="middle" fill="#e0e0e0" font-size="12">{inputNode.label}</text>

      <!-- ViperX node -->
      <rect x={viperxNode.x} y={viperxNode.y} width={NODE_W} height={NODE_H}
        rx="6" fill="#0a0a14" stroke={nodeColor(viperxNode.active)} stroke-width="2" />
      <text x={viperxNode.x + NODE_W / 2} y={viperxNode.y + NODE_H / 2 + 5}
        text-anchor="middle" fill={viperxNode.active ? '#00d4ff' : '#555'} font-size="12">{viperxNode.label}</text>

      <!-- ViperX output stems -->
      {#each viperxOut as stem}
        <!-- Arrow: ViperX → stem -->
        <line
          x1={viperxNode.x + NODE_W} y1={viperxNode.y + NODE_H / 2}
          x2={stem.x} y2={stem.y + STEM_H / 2}
          stroke={stemColor(stem.active)}
          stroke-width="1.5"
        />
        <rect x={stem.x} y={stem.y} width={STEM_W} height={STEM_H}
          rx="4" fill="#0a0a14" stroke={stemColor(stem.active)} stroke-width="1.5" />
        <text x={stem.x + STEM_W / 2} y={stem.y + STEM_H / 2 + 5}
          text-anchor="middle" fill={stem.active ? '#4caf50' : '#555'} font-size="10">{stem.label}</text>
      {/each}

      <!-- Arrow: ViperX stems → Demucs (collector) -->
      {#if viperxOut.length > 0 && demucsEnabled}
        <line
          x1={viperxOut[0].x + STEM_W} y1={viperxOut[0].y + STEM_H / 2}
          x2={demucsNode.x} y2={demucsNode.y + NODE_H / 2}
          stroke={nodeColor(demucsEnabled)}
          stroke-width="2"
        />
      {/if}

      <!-- Demucs node -->
      <rect x={demucsNode.x} y={demucsNode.y} width={NODE_W} height={NODE_H}
        rx="6" fill="#0a0a14" stroke={nodeColor(demucsNode.active)} stroke-width="2" />
      <text x={demucsNode.x + NODE_W / 2} y={demucsNode.y + NODE_H / 2 + 5}
        text-anchor="middle" fill={demucsNode.active ? '#00d4ff' : '#555'} font-size="12">{demucsNode.label}</text>

      <!-- Demucs output stems -->
      {#each demucsOut as stem}
        <line
          x1={demucsNode.x + NODE_W} y1={demucsNode.y + NODE_H / 2}
          x2={stem.x} y2={stem.y + STEM_H / 2}
          stroke={stemColor(stem.active)}
          stroke-width="1.5"
        />
        <rect x={stem.x} y={stem.y} width={STEM_W} height={STEM_H}
          rx="4" fill="#0a0a14" stroke={stemColor(stem.active)} stroke-width="1.5" />
        <text x={stem.x + STEM_W / 2} y={stem.y + STEM_H / 2 + 5}
          text-anchor="middle" fill={stem.active ? '#4caf50' : '#555'} font-size="10">{stem.label}</text>
      {/each}

      <!-- Arrowhead marker -->
      <defs>
        <marker id="arrowhead" markerWidth="10" markerHeight="7"
          refX="9" refY="3.5" orient="auto">
          <polygon points="0 0, 10 3.5, 0 7" fill="#00d4ff" />
        </marker>
      </defs>
    </svg>
  </div>

  <!-- Action -->
  <div class="actions">
    <button class="btn btn-primary" onclick={handleStart} disabled={disabled}>
      ▶ Ejecutar
    </button>
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

  .graph-section {
    width: 100%;
    overflow-x: auto;
    background: #0a0a14;
    border-radius: 8px;
    padding: 0.5rem;
    border: 1px solid #2a2a4a;
  }
  .pipeline-graph {
    display: block;
    min-width: 400px;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
  }
  .btn {
    padding: 0.6rem 1.5rem;
    border: none;
    border-radius: 8px;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, opacity 0.15s;
    flex: 1;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-primary {
    background: linear-gradient(135deg, #00d4ff, #0088cc);
    color: #0a0a14;
  }
  .btn-primary:hover:not(:disabled) {
    background: linear-gradient(135deg, #33ddff, #0099dd);
  }
</style>
