<script lang="ts">
  import { getModelConfig, setModelConfig, getLocalModels, getGpuInfo, getVRAMCalculator, buildVRAMCalculatorParams, type ModelFlag, type ModelFlagsResponse, type LocalModel, type GpuInfo, type VRAMCalculatorResponse } from './api';

  interface Props {
    onclose?: () => void;
    initialModel?: string;
  }

  let { onclose, initialModel }: Props = $props();

  // ---- State ----
  let models = $state<LocalModel[]>([]);
  let selectedCategory = $state<string>('');
  let selectedModel = $state('');
  let previousModel = $state('');
  let configLoaded = $state(false);
  let flags = $state<ModelFlag[]>([]);
  let flagValues = $state<Record<string, number | string>>({});
  let feedback = $state('');
  let feedbackType = $state<'success' | 'error'>('success');
  let loading = $state(true);
  let saving = $state(false);
  let totalVramMb = $state<number | null>(null);
  let vramError = $state(false);

  // VRAM calculator result from backend API
  let vramCalcResult = $state<VRAMCalculatorResponse | null>(null);
  let vramCalcLoading = $state(false);
  let vramCalcError = $state(false);

  // Derived VRAM percentage bar
  let vramPercent = $derived.by(() => {
    if (vramCalcResult === null || totalVramMb == null || totalVramMb <= 0) return null;
    return (vramCalcResult.total_vram_mb / totalVramMb) * 100;
  });

  // Categories come from the backend (derived from the real model type).
  let categories = $derived.by(() => {
    const set = new Set(models.map((m) => m.category).filter(Boolean));
    return [...set].sort((a, b) => a.localeCompare(b));
  });

  // Group all models by category for quick lookup.
  let modelsByCategory = $derived.by(() => {
    const map: Record<string, LocalModel[]> = {};
    for (const m of models) {
      const cat = m.category;
      if (!cat) continue;
      if (!map[cat]) map[cat] = [];
      map[cat].push(m);
    }
    for (const cat of Object.keys(map)) {
      map[cat].sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name));
    }
    return map;
  });

  // Models available for the selected category.
  let filteredModels = $derived.by(() => {
    if (!selectedCategory) return [];
    return modelsByCategory[selectedCategory] ?? [];
  });

  // Display name for the selected model
  let selectedModelDisplayName = $derived.by(() => {
    if (!selectedModel) return '';
    const found = models.find(m => m.name === selectedModel);
    return found?.display_name || found?.name || selectedModel;
  });

  // Load model list + optionally load config for initialModel
  $effect(() => {
    async function load() {
      try {
        const res = await getLocalModels();
        models = res.models || [];

        if (initialModel && models.some(m => m.name === initialModel)) {
          selectedModel = initialModel;
          const found = models.find(m => m.name === initialModel);
          selectedCategory = found?.category || '';
        }

        // If nothing pre-selected, pick the first category that has models.
        if (!selectedCategory && categories.length > 0) {
          selectedCategory = categories[0];
        }

        // Auto-select the first model of the active category if none selected.
        if (!selectedModel && filteredModels.length > 0) {
          selectedModel = filteredModels[0].name;
        }

        if (selectedModel) {
          await loadConfig(selectedModel);
        }
      } catch {
        // Keep defaults on error
      }
      loading = false;
    }
    load();

    // Load GPU info for VRAM estimation
    async function loadGpu() {
      try {
        const gpu = await getGpuInfo();
        if (!gpu.ok) {
          vramError = true;
          totalVramMb = null;
        } else if (typeof gpu.vram_total_mb === 'number' && isFinite(gpu.vram_total_mb) && gpu.vram_total_mb > 0) {
          totalVramMb = gpu.vram_total_mb;
          vramError = false;
        } else {
          vramError = true;
          totalVramMb = null;
        }
      } catch {
        vramError = true;
        totalVramMb = null;
      }
    }
    loadGpu();
  });

  // Keep selectedCategory in sync when the user changes the model directly.
  $effect(() => {
    if (selectedModel) {
      const found = models.find(m => m.name === selectedModel);
      if (found?.category && selectedCategory !== found.category) {
        selectedCategory = found.category;
      }
    }
  });

  // Build VRAM calculator params purely from flag names returned by the API.
  // The shared helper lives in api.ts so the launch preview and this page agree.
  function buildVRAMParams(model: string, values: Record<string, number | string>) {
    return buildVRAMCalculatorParams(model, values);
  }

  // True when the current VRAM estimate is backed by a real measurement.
  let vramReliable = $derived.by(() => {
    if (vramCalcResult === null) return true;
    return vramCalcResult.reliable !== false;
  });

  let vramWarning = $derived.by(() => {
    return vramCalcResult?.warning ?? '';
  });

  // Call backend VRAM calculator when parameters change
  $effect(() => {
    const model = selectedModel;
    if (!model) {
      vramCalcResult = null;
      vramCalcLoading = false;
      vramCalcError = false;
      configLoaded = false;
      return;
    }

    // Reset configLoaded when model changes
    if (model !== previousModel) {
      configLoaded = false;
      previousModel = model;
    }

    // Track configLoaded reactively — don't call API until config is loaded
    if (!configLoaded) return;

    // SNAPSHOT: read ALL reactive values synchronously so $effect tracks them
    const values = flagValues;

    // Debounce timer (avoid rapid-fire calls during slider drag)
    let cancelled = false;
    const timer = setTimeout(async () => {
      vramCalcLoading = true;
      vramCalcError = false;
      try {
        const params = buildVRAMParams(model, values);
        const result = await getVRAMCalculator(params);
        if (!cancelled) {
          vramCalcResult = result;
          vramCalcLoading = false;
        }
      } catch {
        if (!cancelled) {
          vramCalcError = true;
          vramCalcResult = null;
          vramCalcLoading = false;
        }
      }
    }, 300);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  });

  async function loadConfig(modelName: string): Promise<void> {
    try {
      const cfg = await getModelConfig(modelName);
      flags = cfg.flags ?? [];
      const next: Record<string, number | string> = {};
      for (const f of flags) {
        next[f.name] = f.value;
      }
      flagValues = next;
      configLoaded = true;
    } catch {
      // Use current values as defaults
    }
  }

  function handleCategorySelect(category: string) {
    selectedCategory = category;
    const list = modelsByCategory[category] ?? [];
    selectedModel = list.length > 0 ? list[0].name : '';
    if (selectedModel) {
      loadConfig(selectedModel);
    } else {
      configLoaded = false;
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
    saving = true;
    try {
      await setModelConfig(flagValues, selectedModel);
      feedback = '✅ Configuración guardada';
      feedbackType = 'success';
    } catch (e: any) {
      feedback = `❌ Error: ${e.message}`;
      feedbackType = 'error';
    }
    saving = false;
    setTimeout(() => (feedback = ''), 3000);
  }

  function formatFlagValue(flag: ModelFlag): string {
    const v = flagValues[flag.name];
    if (v === undefined || v === null) return String(flag.default);
    if (flag.name === 'num_overlap') return `1/${Number(v)}`;
    if (flag.name === 'chunk_size' && Number(v) === 0) return 'canción completa';
    if (flag.name === 'batch_size' && Number(v) === 0) return 'auto';
    if (flag.name === 'segment' && Number(v) === 0) return 'auto';
    if (flag.name === 'jobs' && Number(v) === 0) return 'auto';
    return String(v);
  }

  function updateFlag(name: string, value: number | string) {
    flagValues = { ...flagValues, [name]: value };
  }

  type Affect = 'quality' | 'vram' | 'speed';

  const FLAG_NOUNS: Record<string, string> = {
    num_overlap: 'solape',
    segment_size: 'segmento',
    chunk_size: 'trozo',
    batch_size: 'segmentos',
    shifts: 'predicciones',
    segment: 'segmento',
    jobs: 'trabajos',
  };

  // Flags whose semantic "more is better" is reversed: the best value is at the minimum.
  // chunk_size: 0 = process whole song = maximum quality.
  function isSliderInverted(flag: ModelFlag): boolean {
    return flag.name === 'chunk_size' && flag.better_side === 'quality';
  }

  function getSliderLabels(flag: ModelFlag): { left: string; right: string } | null {
    if (!flag.affects?.length || !flag.better_side) return null;

    const noun = FLAG_NOUNS[flag.name];
    const worseText = {
      quality: 'menos calidad',
      vram: 'menos VRAM',
      speed: 'más lento',
    }[flag.better_side as Affect];

    const betterText = {
      quality: 'más calidad',
      vram: 'más VRAM',
      speed: 'más rápido',
    }[flag.better_side as Affect];

    if (flag.name === 'chunk_size') {
      // Inverted: left = many chunks / worse, right = whole song / better.
      return { left: `más trozos · ${worseText}`, right: `canción entera · ${betterText}` };
    }

    if (noun) {
      return { left: `menos ${noun} · ${worseText}`, right: `más ${noun} · ${betterText}` };
    }

    return { left: worseText, right: betterText };
  }

  function getFlagRealValue(flag: ModelFlag): number {
    const v = flagValues[flag.name];
    if (v === undefined || v === null) return Number(flag.default);
    return Number(v);
  }

  function sliderInputToRealValue(flag: ModelFlag, visualValue: number): number {
    if (!isSliderInverted(flag)) return visualValue;
    return Number(flag.max ?? 100) - visualValue;
  }

  function realValueToSliderInput(flag: ModelFlag, realValue: number): number {
    if (!isSliderInverted(flag)) return realValue;
    return Number(flag.max ?? 100) - realValue;
  }

  function formatGb(mb: number): string {
    return (mb / 1024).toFixed(1) + ' GB';
  }

  function vramBarColor(pct: number): string {
    if (pct > 85) return '#e57373';
    if (pct >= 60) return '#ffb74d';
    return '#81c784';
  }
</script>

{#if loading}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <button class="btn-close" onclick={onclose}>✕</button>
      <h2>⚙️ Configuración de Modelos</h2>
      <div><!-- spacer --></div>
    </div>
    <div class="fullscreen-body loading-text">Cargando...</div>
  </div>
{:else}
  <div class="fullscreen">
    <div class="fullscreen-header">
      <button class="btn-close" onclick={onclose}>✕</button>
      <h2>⚙️ {selectedModelDisplayName || 'Configuración de Modelos'}</h2>
      <div><!-- spacer --></div>
    </div>
    <div class="fullscreen-body">
      <!-- Type tabs -->
      <div class="type-tabs" role="tablist" aria-label="Tipo de modelo">
        {#each categories as cat}
          <button
            type="button"
            role="tab"
            aria-selected={selectedCategory === cat}
            class="type-tab"
            class:active={selectedCategory === cat}
            onclick={() => handleCategorySelect(cat)}
          >
            {cat}
          </button>
        {/each}
      </div>

      <!-- Model selector -->
      <div class="field">
        <label for="model-select">Modelo:</label>
        <select id="model-select" value={selectedModel} onchange={handleModelSelect} disabled={filteredModels.length === 0}>
          <option value="">-- Seleccionar modelo --</option>
          {#each filteredModels as m}
            <option value={m.name}>{m.display_name || m.name}</option>
          {/each}
        </select>
        {#if filteredModels.length === 0}
          <div class="hint">No se encontraron modelos de este tipo. Descarga uno primero.</div>
        {/if}
      </div>

      <!-- Quality / VRAM trade-off scale -->
      <div class="quality-scale">
        <div class="quality-scale-title">Calidad / VRAM</div>
        <div class="quality-scale-bar">
          <span class="quality-scale-min">↓ Menos calidad</span>
          <span class="quality-scale-max">↑ Más calidad</span>
        </div>
        <div class="quality-scale-note">
          A la derecha: más calidad (o más VRAM si el flag no afecta la calidad).
        </div>
      </div>

      <!-- Flags (disabled when no model selected) -->
      <fieldset class="sliders" disabled={!selectedModel}>
        {#each flags as flag (flag.name)}
          {#if flag.editable}
            {@const inverted = isSliderInverted(flag)}
            {@const realValue = getFlagRealValue(flag)}
            {@const labels = getSliderLabels(flag)}
            <div class="field flag-field" data-flag-name={flag.name}>
              <label for="flag-{flag.name}">
                {flag.name}: <strong>{formatFlagValue(flag)}</strong>
              </label>
              <!-- A flag with explicit choices is a choice control even when the
                   backend omits the type field (e.g. the device flag). -->
              {#if flag.type === 'choice' || (flag.choices && flag.choices.length > 0)}
                <select
                  id="flag-{flag.name}"
                  value={flagValues[flag.name]}
                  onchange={(e) => updateFlag(flag.name, (e.target as HTMLSelectElement).value)}
                >
                  {#each flag.choices ?? [] as choice}
                    <option value={choice}>{choice}</option>
                  {/each}
                </select>
              {:else}
                <input
                  id="flag-{flag.name}"
                  type="range"
                  class="flag-slider"
                  class:inverted
                  min={inverted ? 0 : (flag.min ?? 0)}
                  max={inverted
                    ? Number(flag.max ?? 100) - Number(flag.min ?? 0)
                    : (flag.max ?? 100)}
                  step={flag.step ?? 1}
                  value={inverted ? realValueToSliderInput(flag, realValue) : realValue}
                  oninput={(e) =>
                    updateFlag(
                      flag.name,
                      sliderInputToRealValue(flag, Number(e.currentTarget.value)),
                    )}
                />
              {/if}
              {#if flag.description}
                <p class="param-desc flag-description">{flag.description}</p>
              {/if}
              {#if labels}
                <div class="slider-labels">
                  <span class="slider-min">{labels.left}</span>
                  <span class="slider-max">{labels.right}</span>
                </div>
              {/if}
            </div>
          {/if}
        {/each}

        <!-- VRAM Estimation (from backend calculator) -->
        {#if vramCalcLoading}
          <div class="vram-section">
            <div class="vram-text muted">Calculando VRAM...</div>
          </div>
        {:else if vramCalcResult !== null}
          <div class="vram-section">
            <div class="vram-header">
              <span>🧠 VRAM Estimada</span>
              {#if vramPercent !== null}
                <span class="vram-pct" style="color: {vramBarColor(vramPercent)}">{vramPercent.toFixed(0)}%</span>
              {/if}
              {#if vramCalcResult.fits}
                <span class="vram-fits">✓ Cabe</span>
              {:else}
                <span class="vram-fits vram-fits-no">✗ No cabe</span>
              {/if}
            </div>
            <div class="vram-bar-track">
              <div
                class="vram-bar-fill"
                style="width: {Math.min(vramPercent ?? 0, 100)}%; background: {vramBarColor(vramPercent ?? 0)}"
              ></div>
            </div>
            <div class="vram-text">
              Estimado: {formatGb(vramCalcResult.total_vram_mb)}
              {#if totalVramMb !== null} / {formatGb(totalVramMb)}{/if}
              {#if vramPercent !== null} ({vramPercent.toFixed(0)}%){/if}
              {#if vramCalcResult.free_after_mb !== undefined}
                · Libre después: {formatGb(vramCalcResult.free_after_mb)}
              {/if}
            </div>
            {#if !vramReliable || vramWarning}
              <div class="vram-warning">
                ⚠️ {vramWarning || 'Estimación aproximada: el consumo real puede variar.'}
              </div>
            {/if}
          </div>
        {:else if vramCalcError || vramError}
          <div class="vram-section">
            <div class="vram-text muted">VRAM no disponible</div>
          </div>
        {:else}
          <div class="vram-section">
            <div class="vram-text muted">Selecciona un modelo para estimar VRAM</div>
          </div>
        {/if}

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

  .btn-close {
    background: transparent; border: 1px solid var(--border); color: var(--text-secondary);
    font-size: 18px; width: 32px; height: 32px; border-radius: 6px;
    cursor: pointer; display: flex; align-items: center; justify-content: center;
    flex-shrink: 0;
  }
  .btn-close:hover { background: rgba(255,255,255,0.1); color: #fff; }

  .fullscreen-body {
    flex: 1;
    overflow-y: auto;
    padding: 1.5rem;
    max-width: 600px;
    margin: 0 auto;
    width: 100%;
    box-sizing: border-box;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .loading-text {
    color: var(--text-secondary);
    text-align: center;
    padding-top: 2rem;
  }

  .type-tabs {
    display: flex;
    gap: 0.4rem;
    margin-bottom: 1rem;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.5rem;
  }

  .type-tab {
    flex: 1;
    padding: 0.45rem 0.2rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-secondary);
    font-size: 0.8rem;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, color 0.15s;
  }
  .type-tab:hover {
    border-color: var(--accent);
    color: var(--text-primary);
  }
  .type-tab.active {
    background: linear-gradient(135deg, var(--accent), var(--accent-light));
    border-color: transparent;
    color: var(--text-primary);
    font-weight: 600;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .field label {
    font-size: 0.85rem;
    color: var(--text-primary);
  }

  .field label strong {
    color: var(--accent-light);
  }

  .field input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
    height: 6px;
  }

  .field select {
    padding: 0.4rem 0.6rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 0.85rem;
    outline: none;
    cursor: pointer;
    width: 100%;
  }
  .field select:focus {
    border-color: var(--accent);
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
    color: var(--text-muted);
    margin-top: 0.25rem;
  }

  .btn-apply {
    padding: 0.6rem 1rem;
    background: linear-gradient(135deg, var(--accent), var(--accent-light));
    border: none;
    border-radius: 8px;
    color: var(--text-primary);
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

  /* VRAM estimation */
  .vram-section {
    margin-top: 0.25rem;
  }

  .vram-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.8rem;
    color: var(--text-primary);
    margin-bottom: 0.3rem;
  }

  .vram-pct {
    font-weight: 700;
    font-size: 0.85rem;
  }

  .vram-fits {
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: #1b3a1b;
    color: #81c784;
  }
  .vram-fits-no {
    background: #3a1b1b;
    color: #e57373;
  }

  .vram-bar-track {
    width: 100%;
    height: 8px;
    background: #2a2a4a;
    border-radius: 4px;
    overflow: hidden;
  }

  .vram-bar-fill {
    height: 100%;
    border-radius: 4px;
    transition: width 0.2s ease, background 0.2s ease;
  }

  .vram-text {
    font-size: 0.7rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  .vram-text.muted {
    color: var(--text-muted);
    font-style: italic;
  }

  .vram-warning {
    font-size: 0.7rem;
    color: #ffb74d;
    margin-top: 0.35rem;
    line-height: 1.3;
  }

  /* Quality / VRAM scale */
  .quality-scale {
    margin-bottom: 1rem;
    padding: 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  .quality-scale-title {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 0.4rem;
  }

  .quality-scale-bar {
    position: relative;
    height: 10px;
    border-radius: 5px;
    background: linear-gradient(90deg, #2a2a4a 0%, var(--accent) 50%, #81c784 100%);
    margin-bottom: 0.3rem;
  }

  .quality-scale-min,
  .quality-scale-max {
    position: absolute;
    top: 14px;
    font-size: 0.65rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .quality-scale-min {
    left: 0;
  }

  .quality-scale-max {
    right: 0;
  }

  .quality-scale-note {
    font-size: 0.7rem;
    color: var(--text-secondary);
    padding-top: 1.1rem;
  }

  /* Flag descriptions and slider corner labels */
  .flag-description {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin: 2px 0 4px;
    line-height: 1.35;
  }

  .slider-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.7rem;
    color: var(--text-muted);
  }

  .slider-min,
  .slider-max {
    color: var(--text-muted);
    font-size: 0.65rem;
  }
</style>
