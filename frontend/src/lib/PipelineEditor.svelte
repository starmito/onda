<script lang="ts">
  import type { LocalModel } from './api';
  import { getLocalModels, getPresets, savePreset, deletePreset, setDefaultPreset } from './api';
  import type { PresetData } from './api';
  import { IconClose } from './icons';

  // ── Props ──
  interface Props {
    show?: boolean;
    onclose?: () => void;
    onpresetschange?: () => void;
  }

  let { show = false, onclose, onpresetschange }: Props = $props();

  // ── Model catalogue ──
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

  // ── Types ──
  type StepType = 'viperx' | 'demucs';
  type StemAction = 'route' | 'save' | 'discard';

  interface StemConfig {
    action: StemAction;
    target?: string; // 'result' | step id
  }

  interface PipelineStep {
    id: string;
    type: StepType;
    model: string;
    enabled: boolean;
    stems: Record<string, StemConfig>;
  }

  // ── Available stems per type ──
  const STEMS_BY_TYPE: Record<StepType, string[]> = {
    viperx: ['vocals', 'instrumental'],
    demucs: ['drums', 'bass', 'other', 'vocals'],
  };

  // ── State ──
  let viperxModels = $state<ModelOption[]>(viperxDefault);
  let demucsModels = $state<ModelOption[]>(demucsDefault);
  let modelsLoaded = $state(false);

  // ── Editor state ──
  let presetNameInput = $state('');
  let steps = $state<PipelineStep[]>([]);

  // ── Presets list for editor (load/delete) ──
  interface PipelinePreset {
    name: string;
    steps: PipelineStep[];
    locked: boolean;
  }

  let savedPresets = $state<PipelinePreset[]>([]);
  let selectedPreset = $state('');
  let presetsLoading = $state(true);
  let presetsError = $state(false);

  let saveSuccess = $state(false);
  let saveTimer: ReturnType<typeof setTimeout> | null = null;

  let defaultSuccess = $state(false);
  let defaultTimer: ReturnType<typeof setTimeout> | null = null;

  let deleteConfirmVisible = $state(false);

  // ── Load real model list from backend ──
  $effect(() => {
    getLocalModels()
      .then((res) => {
        if (res.models && res.models.length > 0) {
          const vxModels = res.models
            .filter((m: LocalModel) =>
              m.category === 'Roformer' ||
              m.category === 'Roformer/MelBand' ||
              m.category === 'VR_Arch'
            )
            .map((m: LocalModel) => ({ name: m.name, display_name: m.display_name || m.name, category: m.category }));
          if (vxModels.length > 0) {
            viperxModels = vxModels;
          }

          const dmModels = res.models
            .filter((m: LocalModel) =>
              m.category === 'Demucs' || m.category === 'MDX'
            )
            .map((m: LocalModel) => ({ name: m.name, display_name: m.display_name || m.name, category: m.category }));
          if (dmModels.length > 0) {
            demucsModels = dmModels;
          }
        }
        modelsLoaded = true;
      })
      .catch(() => {
        modelsLoaded = true;
      });
  });

  // ── Group models by category ──
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

  // ── Load presets from backend ──
  async function loadPresets() {
    presetsLoading = true;
    presetsError = false;
    try {
      const data = await getPresets();
      const list: PipelinePreset[] = Object.entries(data).map(([name, p]: [string, any]) => ({
        name,
        steps: p.steps || [],
        locked: p.locked ?? false,
      }));
      savedPresets = list;
      presetsLoading = false;
    } catch {
      presetsError = true;
      presetsLoading = false;
    }
  }

  $effect(() => {
    if (show) {
      loadPresets();
    }
  });

  // ── Step management ──
  function addStep() {
    const existingTypes = steps.map(s => s.type);
    let newType: StepType = 'viperx';
    if (existingTypes.includes('viperx')) {
      newType = 'demucs';
    }

    const stems: Record<string, StemConfig> = {};
    for (const s of STEMS_BY_TYPE[newType]) {
      stems[s] = { action: 'save' };
    }

    steps = [...steps, {
      id: `step-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
      type: newType,
      model: newType === 'viperx' ? 'BS_Roformer_Viperx' : 'htdemucs_ft',
      enabled: true,
      stems,
    }];
  }

  function removeStep(stepId: string) {
    steps = steps.filter(s => s.id !== stepId);
  }

  function updateStepModel(stepId: string, model: string) {
    steps = steps.map(s => s.id === stepId ? { ...s, model } : s);
  }

  function updateStepEnabled(stepId: string, enabled: boolean) {
    steps = steps.map(s => s.id === stepId ? { ...s, enabled } : s);
  }

  function updateStepType(stepId: string, type: StepType) {
    const stems: Record<string, StemConfig> = {};
    for (const s of STEMS_BY_TYPE[type]) {
      stems[s] = { action: 'save' };
    }
    const defaultModel = type === 'viperx' ? 'BS_Roformer_Viperx' : 'htdemucs_ft';
    steps = steps.map(s => s.id === stepId ? { ...s, type, stems, model: defaultModel } : s);
  }

  function updateStemAction(stepId: string, stemName: string, action: StemAction) {
    steps = steps.map(s => {
      if (s.id !== stepId) return s;
      return {
        ...s,
        stems: {
          ...s.stems,
          [stemName]: { ...s.stems[stemName], action },
        },
      };
    });
  }

  function updateStemTarget(stepId: string, stemName: string, target: string) {
    steps = steps.map(s => {
      if (s.id !== stepId) return s;
      return {
        ...s,
        stems: {
          ...s.stems,
          [stemName]: { ...s.stems[stemName], target },
        },
      };
    });
  }

  // ── Load preset into editor ──
  function handleLoadPreset(e: Event) {
    const name = (e.target as HTMLSelectElement).value;
    if (!name) return;
    selectedPreset = name;
    const preset = savedPresets.find((p) => p.name === name);
    if (!preset) return;

    presetNameInput = preset.name;
    steps = preset.steps.map(s => ({
      ...s,
      stems: { ...s.stems },
    }));
  }

  // ── Save preset ──
  async function handleSavePreset() {
    const name = presetNameInput.trim();
    if (!name || steps.length === 0) return;

    try {
      const presetData: PresetData = {
        name,
        steps,
        locked: false,
      };

      await savePreset(presetData);
      await loadPresets();
      selectedPreset = name;
      saveSuccess = true;
      if (saveTimer) clearTimeout(saveTimer);
      saveTimer = setTimeout(() => { saveSuccess = false; }, 5000);
      onpresetschange?.();
    } catch {
      // Handle error silently
    }
  }

  // ── Set default ──
  async function handleSetDefault() {
    if (!selectedPreset) return;
    try {
      await setDefaultPreset(selectedPreset);
      defaultSuccess = true;
      if (defaultTimer) clearTimeout(defaultTimer);
      defaultTimer = setTimeout(() => { defaultSuccess = false; }, 5000);
    } catch {
      // Handle error silently
    }
  }

  // ── Delete preset ──
  async function handleDeletePreset() {
    if (!selectedPreset) return;
    try {
      await deletePreset(selectedPreset);
      savedPresets = savedPresets.filter(p => p.name !== selectedPreset);
      if (presetNameInput === selectedPreset) {
        presetNameInput = '';
        steps = [];
      }
      selectedPreset = '';
      deleteConfirmVisible = false;
      onpresetschange?.();
    } catch {
      // Handle error silently
    }
  }

  function handleDeletePresetConfirm() {
    if (!selectedPreset) return;
    deleteConfirmVisible = true;
  }

  // ── Stem display names ──
  const STEM_LABELS: Record<string, string> = {
    vocals: '🎤 Vocals',
    instrumental: '🎵 Instrumental',
    drums: '🥁 Drums',
    bass: '🎸 Bass',
    other: '🎹 Other',
  };

  // ── Close handler ──
  function handleClose() {
    presetNameInput = '';
    steps = [];
    selectedPreset = '';
    deleteConfirmVisible = false;
    onclose?.();
  }
</script>

{#if show}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <h2>🎛 Editor de Pipeline</h2>
      <button class="btn-close" onclick={handleClose} aria-label="Cerrar">{@html IconClose}</button>
    </div>

    <div class="fullscreen-body">
      <div class="editor-content">
        <!-- ═══════════════════ -->
        <!-- PRESET NAME INPUT  -->
        <!-- ═══════════════════ -->
        <div class="section">
          <label class="label">Nombre del preset</label>
          <input
            type="text"
            class="input"
            placeholder="Ej: Mi preset personalizado"
            bind:value={presetNameInput}
          />
        </div>

        <!-- ═══════════════════ -->
        <!-- STEPS LIST         -->
        <!-- ═══════════════════ -->
        <div class="section">
          <div class="label-row">
            <span class="label">Pasos del pipeline</span>
            <button class="btn-add-step" onclick={addStep}>
              ➕ Añadir paso
            </button>
          </div>

          {#if steps.length === 0}
            <div class="empty-steps">
              <p>Aún no hay pasos. Añade al menos un paso para crear un preset.</p>
            </div>
          {/if}

          {#each steps as step (step.id)}
            <div class="step-card" class:step-disabled={!step.enabled}>
              <!-- Step header -->
              <div class="step-header">
                <div class="step-title-row">
                  <label class="step-enabled-toggle">
                    <input
                      type="checkbox"
                      checked={step.enabled}
                      onchange={(e) => updateStepEnabled(step.id, (e.target as HTMLInputElement).checked)}
                    />
                  </label>
                  <span class="step-number">Paso {steps.indexOf(step) + 1}</span>
                  <button class="btn-remove-step" onclick={() => removeStep(step.id)} title="Eliminar paso">✕</button>
                </div>
              </div>

              <!-- Type + Model row -->
              <div class="step-config-row">
                <div class="config-group">
                  <label class="config-label">Tipo</label>
                  <select
                    class="select"
                    value={step.type}
                    onchange={(e) => updateStepType(step.id, (e.target as HTMLSelectElement).value as StepType)}
                  >
                    <option value="viperx">ViperX (Vocales)</option>
                    <option value="demucs">Demucs (Stems)</option>
                  </select>
                </div>

                <div class="config-group">
                  <label class="config-label">Modelo</label>
                  <select
                    class="select"
                    value={step.model}
                    onchange={(e) => updateStepModel(step.id, (e.target as HTMLSelectElement).value)}
                  >
                    {#if step.type === 'viperx'}
                      {#each [...viperxGroups.entries()] as [cat, models]}
                        <optgroup label={cat}>
                          {#each models as m}
                            <option value={m.name}>{m.display_name || m.name}</option>
                          {/each}
                        </optgroup>
                      {/each}
                    {:else}
                      {#each [...demucsGroups.entries()] as [cat, models]}
                        <optgroup label={cat}>
                          {#each models as m}
                            <option value={m.name}>{m.display_name || m.name}</option>
                          {/each}
                        </optgroup>
                      {/each}
                    {/if}
                  </select>
                </div>
              </div>

              <!-- Routing Matrix -->
              <div class="routing-matrix">
                <div class="routing-header">
                  <span class="routing-stem-label">Stem</span>
                  <span class="routing-action-label">🔽 Siguiente</span>
                  <span class="routing-action-label">💾 Resultado</span>
                  <span class="routing-action-label">🗑 Descartar</span>
                </div>
                {#each STEMS_BY_TYPE[step.type] as stemName}
                  <div class="routing-row">
                    <span class="routing-stem-name">{STEM_LABELS[stemName] || stemName}</span>
                    <label class="routing-radio" class:active={step.stems[stemName]?.action === 'route'}>
                      <input
                        type="radio"
                        name="{step.id}-{stemName}"
                        checked={step.stems[stemName]?.action === 'route'}
                        onchange={() => updateStemAction(step.id, stemName, 'route')}
                      />
                      <span class="radio-indicator"></span>
                    </label>
                    <label class="routing-radio" class:active={step.stems[stemName]?.action === 'save'}>
                      <input
                        type="radio"
                        name="{step.id}-{stemName}"
                        checked={(!step.stems[stemName] || step.stems[stemName]?.action === 'save')}
                        onchange={() => updateStemAction(step.id, stemName, 'save')}
                      />
                      <span class="radio-indicator"></span>
                    </label>
                    <label class="routing-radio discard-radio" class:active={step.stems[stemName]?.action === 'discard'}>
                      <input
                        type="radio"
                        name="{step.id}-{stemName}"
                        checked={step.stems[stemName]?.action === 'discard'}
                        onchange={() => updateStemAction(step.id, stemName, 'discard')}
                      />
                      <span class="radio-indicator"></span>
                    </label>
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>

        <!-- ═══════════════════ -->
        <!-- SAVE PRESET        -->
        <!-- ═══════════════════ -->
        <div class="save-section">
          <button class="btn-save" onclick={handleSavePreset} disabled={!presetNameInput.trim() || steps.length === 0}>
            💾 Guardar Preset
          </button>
          {#if saveSuccess}
            <div class="feedback-banner success">✅ Preset guardado correctamente</div>
          {/if}
        </div>

        <!-- ═══════════════════════════════ -->
        <!--  GESTIÓN DE PRESETS              -->
        <!-- ═══════════════════════════════ -->
        <div class="management-section">
          <h3 class="section-title">✏️ Gestión de Presets</h3>

          <!-- Preset selector -->
          <div class="section">
            <span class="label">Mis presets</span>
            {#if presetsLoading}
              <div class="hint">Cargando presets...</div>
            {:else if presetsError}
              <div class="hint error">Error al cargar presets</div>
            {/if}
            {#if savedPresets.length > 0}
              <div class="preset-select-row">
                <select
                  class="select"
                  bind:value={selectedPreset}
                >
                  <option value="">-- Seleccionar preset --</option>
                  {#each savedPresets as p}
                    <option value={p.name}>{p.name}{#if p.locked} 🔒{/if}</option>
                  {/each}
                </select>
                <button
                  class="btn-load"
                  onclick={() => {
                    const p = savedPresets.find(p => p.name === selectedPreset);
                    if (p) {
                      presetNameInput = p.name;
                      steps = p.steps.map(s => ({ ...s, stems: { ...s.stems } }));
                    }
                  }}
                  disabled={!selectedPreset}
                  title="Cargar preset en el editor"
                >
                  📂 Cargar
                </button>
              </div>
            {:else if !presetsLoading && !presetsError}
              <p class="hint">No hay presets guardados.</p>
            {/if}
          </div>

          <!-- Set default -->
          <div class="section">
            <button class="btn-default" onclick={handleSetDefault} disabled={!selectedPreset}>
              ⭐ Establecer como predeterminado
            </button>
            {#if defaultSuccess}
              <div class="feedback-banner success">✅ Establecido como predeterminado</div>
            {/if}
          </div>

          <!-- Delete preset -->
          <div class="section delete-section">
            <button class="btn-delete" onclick={handleDeletePresetConfirm} disabled={!selectedPreset}>
              🗑 Eliminar Preset
            </button>
            {#if deleteConfirmVisible}
              <div class="delete-confirm">
                <p>¿Eliminar "{selectedPreset}"? Esta acción no se puede deshacer.</p>
                <div class="delete-confirm-actions">
                  <button class="btn-cancel" onclick={() => deleteConfirmVisible = false}>Cancelar</button>
                  <button class="btn-confirm-delete" onclick={handleDeletePreset}>Sí, eliminar</button>
                </div>
              </div>
            {/if}
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .fullscreen {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--bg-primary);
    z-index: 900;
    display: flex;
    flex-direction: column;
    animation: fadeIn 0.2s ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .fullscreen-header {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.75rem 1.25rem;
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
  }

  .fullscreen-header h2 {
    margin: 0;
    font-size: 1.1rem;
    color: var(--text-primary);
    flex: 1;
    text-align: center;
  }

  .fullscreen-body {
    flex: 1;
    overflow-y: auto;
    padding: 1.25rem;
    max-width: 800px;
    margin: 0 auto;
    width: 100%;
    box-sizing: border-box;
  }

  .btn-close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 18px;
    width: 32px;
    height: 32px;
    border-radius: 6px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .btn-close:hover {
    background: rgba(255,255,255,0.1);
    color: #fff;
  }

  .editor-content {
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .section-title {
    font-size: 1rem;
    font-weight: 600;
    color: var(--accent-light);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin: 0;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border);
  }

  .label {
    font-size: 0.85rem;
    font-weight: 600;
    color: #a0a0c0;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .label-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .input {
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    padding: 0.6rem 0.75rem;
    font-size: 0.95rem;
    outline: none;
    transition: border-color 0.15s;
  }

  .input:focus {
    border-color: var(--accent);
  }

  .select {
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    padding: 0.5rem 0.75rem;
    font-size: 0.9rem;
    outline: none;
    transition: border-color 0.15s;
    min-width: 180px;
  }

  .select:focus {
    border-color: var(--accent);
  }

  .select optgroup {
    background: var(--bg-primary);
    color: #a0a0c0;
    font-weight: 600;
    font-style: normal;
  }

  .select option {
    background: var(--bg-surface);
    color: var(--text-primary);
    padding: 0.3rem;
  }

  .hint {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin: 0;
  }

  .hint.error {
    color: #e57373;
  }

  /* ---- Step cards ---- */
  .btn-add-step {
    background: var(--accent);
    color: white;
    border: none;
    padding: 0.4rem 0.8rem;
    border-radius: 6px;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
  }

  .btn-add-step:hover {
    background: var(--accent-light);
  }

  .empty-steps {
    text-align: center;
    padding: 2rem;
    color: var(--text-muted);
    background: var(--bg-surface);
    border: 1px dashed var(--border);
    border-radius: 8px;
  }

  .empty-steps p {
    margin: 0;
    font-size: 0.9rem;
  }

  .step-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    transition: opacity 0.2s, border-color 0.2s;
  }

  .step-card.step-disabled {
    opacity: 0.55;
    border-color: #3a3a4a;
  }

  .step-header {
    display: flex;
    align-items: center;
  }

  .step-title-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
  }

  .step-enabled-toggle input {
    accent-color: var(--accent);
    width: 16px;
    height: 16px;
    cursor: pointer;
  }

  .step-number {
    font-size: 0.9rem;
    font-weight: 700;
    color: var(--accent-light);
    flex: 1;
  }

  .btn-remove-step {
    background: transparent;
    border: 1px solid #4a2a2a;
    color: #e57373;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    font-size: 14px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s;
  }

  .btn-remove-step:hover {
    background: #dc3545;
    color: white;
    border-color: #dc3545;
  }

  .step-config-row {
    display: flex;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .config-group {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    flex: 1;
    min-width: 150px;
  }

  .config-label {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }

  /* ---- Routing Matrix ---- */
  .routing-matrix {
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    margin-top: 0.25rem;
  }

  .routing-header {
    display: grid;
    grid-template-columns: 1fr repeat(3, 1fr);
    gap: 0;
    padding: 0.5rem 0.75rem;
    background: rgba(128,128,128,0.08);
    border-bottom: 1px solid var(--border);
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }

  .routing-stem-label {
    text-align: left;
  }

  .routing-action-label {
    text-align: center;
  }

  .routing-row {
    display: grid;
    grid-template-columns: 1fr repeat(3, 1fr);
    gap: 0;
    padding: 0.5rem 0.75rem;
    align-items: center;
    border-bottom: 1px solid rgba(128,128,128,0.06);
  }

  .routing-row:last-child {
    border-bottom: none;
  }

  .routing-stem-name {
    font-size: 0.85rem;
    font-weight: 500;
    color: var(--text-primary);
  }

  .routing-radio {
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    position: relative;
  }

  .routing-radio input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }

  .radio-indicator {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 2px solid #555;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s;
  }

  .routing-radio.active .radio-indicator {
    border-color: var(--accent);
    background: var(--accent);
    box-shadow: 0 0 6px var(--accent-glow);
  }

  .routing-radio.active .radio-indicator::after {
    content: '';
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: white;
  }

  .routing-radio:hover .radio-indicator {
    border-color: var(--accent);
  }

  .discard-radio.active .radio-indicator {
    border-color: #dc3545;
    background: #dc3545;
  }

  .discard-radio.active .radio-indicator::after {
    content: '';
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: white;
  }

  /* ---- Save ---- */
  .save-section {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.75rem;
    margin-top: 0.5rem;
  }

  .btn-save {
    background: #4caf50;
    color: white;
    border: none;
    padding: 14px 40px;
    border-radius: 10px;
    font-size: 17px;
    font-weight: bold;
    cursor: pointer;
    min-width: 240px;
    transition: background 0.2s;
  }

  .btn-save:hover {
    background: #43a047;
  }

  .btn-save:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .feedback-banner {
    padding: 10px 20px;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
  }

  .feedback-banner.success {
    background: #2e7d32;
    color: white;
  }

  /* ---- Management section ---- */
  .management-section {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    margin-top: 1.5rem;
    padding-top: 1.5rem;
    border-top: 1px solid var(--border);
  }

  .preset-select-row {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }

  .preset-select-row .select {
    flex: 1;
  }

  .btn-load {
    padding: 0.5rem 1rem;
    background: var(--accent);
    color: white;
    border: none;
    border-radius: 6px;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
    white-space: nowrap;
  }

  .btn-load:hover {
    background: var(--accent-light);
  }

  .btn-load:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .btn-default {
    width: 100%;
    padding: 12px;
    background: linear-gradient(135deg, #f9a825, #f57f17);
    color: #1a1a2e;
    border: none;
    border-radius: 8px;
    font-size: 15px;
    font-weight: bold;
    cursor: pointer;
    transition: opacity 0.2s;
  }

  .btn-default:hover {
    opacity: 0.9;
  }

  .btn-default:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .delete-section {
    border-top: 1px solid var(--border);
    padding-top: 1rem;
  }

  .btn-delete {
    width: 100%;
    padding: 12px;
    background: transparent;
    border: 2px solid #dc3545;
    color: #dc3545;
    border-radius: 8px;
    font-size: 15px;
    font-weight: bold;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-delete:hover {
    background: #dc3545;
    color: white;
  }

  .btn-delete:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .delete-confirm {
    margin-top: 12px;
    padding: 12px;
    background: rgba(220,53,69,0.1);
    border: 1px solid #dc3545;
    border-radius: 8px;
    text-align: center;
  }

  .delete-confirm p {
    color: var(--text-primary);
    margin: 0 0 10px 0;
    font-size: 14px;
  }

  .delete-confirm-actions {
    display: flex;
    gap: 10px;
    justify-content: center;
  }

  .btn-cancel {
    padding: 8px 20px;
    background: #444;
    color: var(--text-primary);
    border: none;
    border-radius: 6px;
    cursor: pointer;
  }

  .btn-confirm-delete {
    padding: 8px 20px;
    background: #dc3545;
    color: white;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-weight: bold;
  }
</style>
