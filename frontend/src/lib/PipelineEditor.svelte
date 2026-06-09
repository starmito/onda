<script lang="ts">
  import type { LocalModel } from './api';
  import { getLocalModels, getPresets, savePreset, deletePreset } from './api';
  import type { PresetData } from './api';

  // ── Props ──
  let {
    disabled = false,
    hasFiles = false,
    onstart,
  }: {
    disabled?: boolean;
    hasFiles?: boolean;
    onstart?: (config: any) => void;
  } = $props();

  // ── Model catalogue ──
  // ViperX models grouped by category
  interface ModelOption {
    name: string;
    display_name: string;
    category: string;
  }

  const viperxDefault: ModelOption[] = [
    { name: 'MelBand_Karaoke', display_name: 'MelBand Karaoke', category: 'Roformer' },
    { name: 'BS_Roformer_Viperx', display_name: 'BS Roformer ViperX', category: 'Roformer' },
    { name: 'BS_PolarFormer', display_name: 'BS PolarFormer', category: 'VR_Arch' },
  ];

  const demucsDefault: ModelOption[] = [
    { name: 'htdemucs_ft', display_name: 'HTDemucs FT', category: 'Demucs' },
    { name: 'htdemucs_6s', display_name: 'HTDemucs 6s', category: 'Demucs' },
    { name: 'Kim_Vocal_2', display_name: 'Kim Vocal 2', category: 'MDX' },
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
    demucsEnabled && viperxEnabled && viperxStems.vocals
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
            .map((m: LocalModel) => ({ name: m.name, display_name: m.display_name || m.name, category: m.category }));
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
            .map((m: LocalModel) => ({ name: m.name, display_name: m.display_name || m.name, category: m.category }));
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
      preset: selectedPreset || undefined,
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
    const baseY = 12;
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
    const baseY = 12;
    if (demucsEnabled) {
      stems.push({ label: 'drums', x: baseX, y: baseY, active: demucsStems.drums });
      stems.push({ label: 'bass', x: baseX, y: baseY + Y_STEP, active: demucsStems.bass });
      stems.push({ label: 'other', x: baseX, y: baseY + 2 * Y_STEP, active: demucsStems.other });
    }
    return stems;
  });

  const svgWidth = $derived(demucsOut.length > 0
    ? demucsOut[demucsOut.length - 1].x + STEM_W + 20
    : demucsNode.x + NODE_W + 20);
  const svgHeight = $derived.by(() => {
    const maxStemY = Math.max(
      viperxOut.length > 0 ? viperxOut[viperxOut.length - 1].y + STEM_H + 12 : 0,
      demucsOut.length > 0 ? demucsOut[demucsOut.length - 1].y + STEM_H + 12 : 0,
      80,
    );
    return maxStemY + 10;
  });

  function nodeColor(active: boolean): string {
    return active ? '#00d4ff' : '#3a3a5a';
  }
  function stemColor(active: boolean): string {
    return active ? '#4caf50' : '#3a3a5a';
  }

  // ── Presets (backend API) ──
  interface PipelinePreset {
    name: string;
    viperxModel: string;
    demucsModel: string;
    viperxEnabled: boolean;
    demucsEnabled: boolean;
    viperxStems: string[];
    demucsStems: string[];
  }

  let savedPresets = $state<PipelinePreset[]>([]);
  let presetNameInput = $state('');
  let selectedPreset = $state('');
  let presetsLoading = $state(true);
  let presetsError = $state(false);

  let saveSuccess = $state(false);
  let saveTimer: ReturnType<typeof setTimeout> | null = null;

  let deleteSelectedPreset = $state('');
  let deleteConfirmVisible = $state(false);

  // Load presets from backend API
  $effect(() => {
    getPresets()
      .then(data => {
        const list: PipelinePreset[] = Object.entries(data).map(([name, p]) => ({
          name,
          viperxModel: p.vocalModel,
          demucsModel: p.stemModel,
          viperxEnabled: p.viperxEnabled,
          demucsEnabled: p.demucsEnabled,
          viperxStems: p.viperxStems || ['vocals', 'instrumental'],
          demucsStems: p.demucsStems || ['drums', 'bass', 'other', 'vocals'],
        }));
        savedPresets = list;
        presetsLoading = false;
      })
      .catch(() => {
        presetsError = true;
        presetsLoading = false;
      });
  });

  async function handleSavePreset() {
    const name = presetNameInput.trim();
    if (!name) return;

    const vStems: string[] = [];
    if (viperxStems.vocals) vStems.push('vocals');
    if (viperxStems.instrumental) vStems.push('instrumental');

    const dStems: string[] = [];
    if (demucsStems.drums) dStems.push('drums');
    if (demucsStems.bass) dStems.push('bass');
    if (demucsStems.other) dStems.push('other');
    if (demucsStems.vocals) dStems.push('vocals');

    const presetData: PresetData = {
      name,
      viperxEnabled,
      demucsEnabled,
      vocalModel: viperxModel,
      vocalOverlap: 4,
      stemModel: demucsModel,
      drumsModel: '',
      bassModel: '',
      otherModel: '',
      viperxStems: vStems,
      demucsStems: dStems,
      pitch: 0,
      description: '',
    };

    try {
      await savePreset(presetData);
      const data = await getPresets();
      const list: PipelinePreset[] = Object.entries(data).map(([n, p]) => ({
        name: n,
        viperxModel: p.vocalModel,
        demucsModel: p.stemModel,
        viperxEnabled: p.viperxEnabled,
        demucsEnabled: p.demucsEnabled,
        viperxStems: p.viperxStems || ['vocals', 'instrumental'],
        demucsStems: p.demucsStems || ['drums', 'bass', 'other', 'vocals'],
      }));
      savedPresets = list;
      selectedPreset = name;
      presetNameInput = '';
      saveSuccess = true;
      if (saveTimer) clearTimeout(saveTimer);
      saveTimer = setTimeout(() => { saveSuccess = false; }, 5000);
    } catch {
      // Handle error silently
    }
  }

  function handleLoadPreset(e: Event) {
    const name = (e.target as HTMLSelectElement).value;
    if (!name) return;
    selectedPreset = name;
    const preset = savedPresets.find((p) => p.name === name);
    if (!preset) return;

    viperxModel = preset.viperxModel;
    demucsModel = preset.demucsModel;
    viperxEnabled = preset.viperxEnabled;
    demucsEnabled = preset.demucsEnabled;

    viperxStems = {
      vocals: preset.viperxStems.includes('vocals'),
      instrumental: preset.viperxStems.includes('instrumental'),
    };
    demucsStems = {
      drums: preset.demucsStems.includes('drums'),
      bass: preset.demucsStems.includes('bass'),
      other: preset.demucsStems.includes('other'),
      vocals: preset.demucsStems.includes('vocals'),
    };
  }

  async function handleDeletePreset() {
    if (!deleteSelectedPreset) return;
    try {
      await deletePreset(deleteSelectedPreset);
      savedPresets = savedPresets.filter(p => p.name !== deleteSelectedPreset);
      deleteSelectedPreset = '';
      deleteConfirmVisible = false;
    } catch {
      // Handle error silently
    }
  }

  function handleDeletePresetConfirm() {
    if (!deleteSelectedPreset) return;
    deleteConfirmVisible = true;
  }
</script>

<div class="editor-card">
  <h2 class="editor-title">🎛 Editor de Presets</h2>

  <!-- Presets: Load -->
  <div class="section">
    <span class="label">Presets</span>
    {#if presetsLoading}
      <div class="loading-hint">Cargando presets...</div>
    {:else if presetsError}
      <div class="error-hint">Error al cargar presets</div>
    {/if}
    {#if savedPresets.length > 0}
      <div class="preset-row">
        <select
          class="select"
          value={selectedPreset}
          onchange={handleLoadPreset}
          disabled={disabled}
        >
          <option value="">-- Mis presets --</option>
          {#each savedPresets as p}
            <option value={p.name}>{p.name}</option>
          {/each}
        </select>
      </div>
    {/if}
    <div class="preset-row">
      <input
        type="text"
        class="input"
        placeholder="Nombre del preset"
        bind:value={presetNameInput}
        disabled={disabled}
      />
    </div>
  </div>

  <!-- ViperX Model Selector -->
  <div class="section">
    <label class="label" for="viperx-model">Modelo separador de Voces</label>
    <select
      id="viperx-model"
      class="select"
      bind:value={viperxModel}
      disabled={disabled}
    >
      {#each [...viperxGroups.entries()] as [cat, models]}
        <optgroup label={cat}>
          {#each models as m}
            <option value={m.name}>{m.display_name || m.name}</option>
          {/each}
        </optgroup>
      {/each}
    </select>
  </div>

  <!-- Demucs Model Selector -->
  <div class="section">
    <label class="label" for="demucs-model">Modelo separador de Stems</label>
    <select
      id="demucs-model"
      class="select"
      bind:value={demucsModel}
      disabled={disabled}
    >
      {#each [...demucsGroups.entries()] as [cat, models]}
        <optgroup label={cat}>
          {#each models as m}
            <option value={m.name}>{m.display_name || m.name}</option>
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
      <span class="step-label">Paso 1: Separación de Voces</span>
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
      <span class="step-label">Paso 2: Separación de Stems</span>
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
      <text x={viperxNode.x + NODE_W / 2} y={viperxNode.y + NODE_H / 2 + 18}
        text-anchor="middle" class="graph-model-name">{viperxModel}</text>

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
      <text x={demucsNode.x + NODE_W / 2} y={demucsNode.y + NODE_H / 2 + 18}
        text-anchor="middle" class="graph-model-name">{demucsModel}</text>

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

  <!-- Save Preset Button -->
  <div class="preset-save-section">
    <button class="btn-save-large" onclick={handleSavePreset} disabled={disabled || !presetNameInput.trim()}>
      💾 Guardar Preset
    </button>
    {#if saveSuccess}
      <div class="save-banner">✅ Preset guardado correctamente</div>
    {/if}
  </div>

  <!-- Delete Presets Section -->
  <div class="section delete-preset-section">
    <span class="label">🗑 Eliminar Presets</span>
    {#if savedPresets.length > 0}
      <div class="preset-row">
        <select class="select" bind:value={deleteSelectedPreset} disabled={disabled}>
          <option value="">-- Seleccionar preset --</option>
          {#each savedPresets as p}
            <option value={p.name}>{p.name}</option>
          {/each}
        </select>
      </div>
      <button class="btn-delete-large" onclick={handleDeletePresetConfirm} disabled={disabled || !deleteSelectedPreset}>
        🗑 Eliminar Preset
      </button>
      {#if deleteConfirmVisible}
        <div class="delete-confirm">
          <p>¿Eliminar "{deleteSelectedPreset}"? Esta acción no se puede deshacer.</p>
          <div class="delete-confirm-actions">
            <button class="btn-cancel" onclick={() => deleteConfirmVisible = false}>Cancelar</button>
            <button class="btn-confirm-delete" onclick={handleDeletePreset}>Sí, eliminar</button>
          </div>
        </div>
      {/if}
    {:else}
      <p class="hint">No hay presets guardados.</p>
    {/if}
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
  .graph-model-name {
    font-size: 7px;
    fill: #888;
  }

  .hint {
    margin: 0;
    font-size: 0.8rem;
    color: #888;
    text-align: center;
  }

  .no-files-msg {
    flex: 1;
    text-align: center;
    padding: 0.6rem 1.5rem;
    font-size: 0.9rem;
    font-weight: 600;
    color: #666;
    background: #111128;
    border: 1px dashed #2a2a4a;
    border-radius: 8px;
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

  .preset-row {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }
  .input {
    flex: 1;
    background: #0a0a14;
    border: 1px solid #2a2a4a;
    border-radius: 6px;
    color: #e0e0e0;
    padding: 0.5rem 0.75rem;
    font-size: 0.9rem;
    outline: none;
    transition: border-color 0.15s;
  }
  .input:focus {
    border-color: #00d4ff;
  }
  .loading-hint {
    font-size: 0.8rem;
    color: #888;
    padding: 0.25rem 0;
  }
  .error-hint {
    font-size: 0.8rem;
    color: #e57373;
    padding: 0.25rem 0;
  }
  .btn-sm {
    padding: 0.4rem 0.8rem;
    font-size: 0.8rem;
    flex: 0 0 auto;
    border: none;
    border-radius: 6px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, opacity 0.15s;
  }
  .btn-sm:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-save {
    background: #1b3a1b;
    color: #81c784;
    border: 1px solid #2a4a2a;
  }
  .btn-save:hover:not(:disabled) {
    background: #2a4a2a;
  }
  .btn-delete {
    background: #3a1b1b;
    color: #e57373;
    border: 1px solid #4a2a2a;
    padding: 0.4rem 0.6rem;
  }
  .btn-delete:hover:not(:disabled) {
    background: #4a2a2a;
  }

  .preset-save-section { margin-top: 20px; display: flex; flex-direction: column; align-items: center; gap: 10px; }
  .btn-save-large { background: #4caf50; color: white; border: none; padding: 14px 40px; border-radius: 10px; font-size: 17px; font-weight: bold; cursor: pointer; min-width: 240px; transition: background 0.2s; }
  .btn-save-large:hover { background: #43a047; }
  .btn-save-large:disabled { opacity: 0.4; cursor: not-allowed; }
  .save-banner { background: #2e7d32; color: white; padding: 10px 20px; border-radius: 8px; font-size: 14px; font-weight: 500; }
  .delete-preset-section { margin-top: 20px; border-top: 1px solid #333; padding-top: 16px; }
  .btn-delete-large { width: 100%; margin-top: 10px; padding: 12px; background: transparent; border: 2px solid #dc3545; color: #dc3545; border-radius: 8px; font-size: 15px; font-weight: bold; cursor: pointer; transition: all 0.2s; }
  .btn-delete-large:hover { background: #dc3545; color: white; }
  .btn-delete-large:disabled { opacity: 0.3; cursor: not-allowed; }
  .delete-confirm { margin-top: 12px; padding: 12px; background: rgba(220,53,69,0.1); border: 1px solid #dc3545; border-radius: 8px; text-align: center; }
  .delete-confirm p { color: #eee; margin: 0 0 10px 0; font-size: 14px; }
  .delete-confirm-actions { display: flex; gap: 10px; justify-content: center; }
  .btn-cancel { padding: 8px 20px; background: #444; color: #eee; border: none; border-radius: 6px; cursor: pointer; }
  .btn-confirm-delete { padding: 8px 20px; background: #dc3545; color: white; border: none; border-radius: 6px; cursor: pointer; font-weight: bold; }
</style>
