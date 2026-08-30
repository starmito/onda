<script lang="ts">
  import { getModelConfig, setModelConfig, getLocalModels, getGpuInfo, getVRAMCalculator, type ModelConfigResponse, type LocalModel, type GpuInfo, type VRAMCalculatorResponse } from './api';

  interface Props {
    onclose?: () => void;
    initialModel?: string;
  }

  let { onclose, initialModel }: Props = $props();

  type ModelType = 'Roformer' | 'Demucs' | 'MDX' | 'MDXNet' | 'SCNet';
  const MODEL_TYPES: ModelType[] = ['Roformer', 'Demucs', 'MDX', 'MDXNet', 'SCNet'];

  function modelTypeFromModel(name?: string, category?: string): ModelType | '' {
    const n = (name || '').toLowerCase();
    if (n.includes('mdxnet')) return 'MDXNet';
    if (n.includes('roformer')) return 'Roformer';
    if (n.includes('mdx')) return 'MDX';
    if (n.includes('scnet')) return 'SCNet';
    if (n.includes('demucs')) return 'Demucs';

    const c = (category || '').toLowerCase();
    if (c.includes('mdxnet')) return 'MDXNet';
    if (c.includes('roformer')) return 'Roformer';
    if (c.includes('demucs')) return 'Demucs';
    if (c.includes('mdx')) return 'MDX';
    if (c.includes('scnet')) return 'SCNet';
    return '';
  }

  // ---- State ----
  let models = $state<LocalModel[]>([]);
  let selectedType = $state<ModelType | ''>('');
  let selectedModel = $state('');
  let previousModel = $state('');
  let configLoaded = $state(false);
  let segmentSize = $state(256);
  let overlap = $state(0.25);
  let chunkSize = $state(0);
  let batchSize = $state(0);
  let device = $state('cuda');
  let shifts = $state(1);
  let segment = $state(0);
  let jobs = $state(0);
  let dimT = $state(801);
  let numOverlap = $state(4);
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

  // Group all models by type for quick lookup
  let modelsByType = $derived.by(() => {
    const map: Record<ModelType, LocalModel[]> = { Roformer: [], Demucs: [], MDX: [], MDXNet: [], SCNet: [] };
    for (const m of models) {
      const t = modelTypeFromModel(m.name, m.category);
      if (t) map[t].push(m);
    }
    // Stable order: prefer the catalog order
    for (const t of MODEL_TYPES) {
      map[t].sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name));
    }
    return map;
  });

  // Models available for the selected type
  let filteredModels = $derived.by(() => {
    if (!selectedType) return [];
    return modelsByType[selectedType] ?? [];
  });

  // Type booleans
  let isRoformer = $derived.by(() => selectedType === 'Roformer');
  let isDemucs = $derived.by(() => selectedType === 'Demucs');
  let isMdx = $derived.by(() => selectedType === 'MDX');
  let isMdxNet = $derived.by(() => selectedType === 'MDXNet');
  let isScnet = $derived.by(() => selectedType === 'SCNet');

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
          selectedType = modelTypeFromModel(found?.name, found?.category) || '';
        }

        // If nothing pre-selected, pick the first type that has models
        if (!selectedType) {
          for (const t of MODEL_TYPES) {
            if (modelsByType[t].length > 0) {
              selectedType = t;
              break;
            }
          }
        }

        // Auto-select the first model of the active type if none selected
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

  // Keep selectedType in sync when the user changes the model directly
  $effect(() => {
    if (selectedModel) {
      const found = models.find(m => m.name === selectedModel);
      const t = modelTypeFromModel(found?.name, found?.category);
      if (t && selectedType !== t) {
        selectedType = t;
      }
    }
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
    const sh = shifts;
    const ss = segmentSize;
    const ov = overlap;
    const bs = batchSize;
    const seg = segment;

    // Debounce timer (avoid rapid-fire calls during slider drag)
    let cancelled = false;
    const timer = setTimeout(async () => {
      vramCalcLoading = true;
      vramCalcError = false;
      try {
        const params: { models: string; shifts?: number; segment_size?: number; overlap?: number; batch_size?: number; demucs_segment?: number } = {
          models: model,
        };
        if (isDemucs) {
          if (sh > 1) params.shifts = sh;
          params.demucs_segment = seg;
        } else {
          if (ss > 0) params.segment_size = ss;
          if (ov > 0) params.overlap = ov;
          if (bs > 0) params.batch_size = bs;
        }
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
      segmentSize = cfg.segment_size;
      overlap = cfg.overlap;
      chunkSize = cfg.chunk_size;
      batchSize = cfg.batch_size;
      device = cfg.device;
      shifts = cfg.shifts ?? 1;
      segment = cfg.segment ?? 0;
      jobs = cfg.jobs ?? 0;
      dimT = cfg.dim_t ?? 801;
      numOverlap = cfg.num_overlap ?? 4;
      // MDX/SCNet/MDXNet sliders start at 1; auto (0) is not a valid choice here.
      if ((isMdx || isScnet || isMdxNet) && batchSize < 1) {
        batchSize = 1;
      }
      configLoaded = true;
    } catch {
      // Use current values as defaults
    }
  }

  function handleTypeSelect(t: ModelType) {
    selectedType = t;
    const list = modelsByType[t] ?? [];
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
    const cfg: ModelConfigResponse = {
      segment_size: segmentSize,
      overlap,
      chunk_size: chunkSize,
      batch_size: batchSize,
      device,
    };
    // Include Demucs params only for Demucs type
    if (isDemucs) {
      cfg.shifts = Math.round(shifts);
      cfg.segment = Number(segment);
      cfg.jobs = Math.round(jobs);
    }
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
        {#each MODEL_TYPES as t}
          <button
            type="button"
            role="tab"
            aria-selected={selectedType === t}
            class="type-tab"
            class:active={selectedType === t}
            onclick={() => handleTypeSelect(t)}
          >
            {t}
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

      <!-- Sliders (disabled when no model selected) -->
      <fieldset class="sliders" disabled={!selectedModel}>
        {#if isRoformer}
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
            <p class="param-desc">Cada unidad sube la VRAM ~6.7 MiB. Más segmento = chunks más largos (mejor contexto, menos overhead), pero con batch alto puede agotar la GPU en canciones largas.</p>
            <div class="slider-labels">
              <span class="slider-min">64 — ⚡ Rápido / -VRAM / -Calidad</span>
              <span class="slider-max">1024 — 🐌 Lento / +VRAM / +Calidad</span>
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
            <p class="param-desc">NO afecta a la VRAM. Solo suaviza las transiciones entre segmentos a costa de más tiempo de proceso.</p>
            <div class="slider-labels">
              <span class="slider-min">0 — ⚡ Rápido / =VRAM / -Calidad</span>
              <span class="slider-max">0.5 — 🐌 Lento / =VRAM / +Calidad</span>
            </div>
          </div>

          <!-- Chunk Size (inverted: 0 = full song on the right = max quality) -->
          <div class="field">
            <label for="chunk-size">
              Chunk Size: <strong>{chunkSize === 0 ? 'canción completa' : chunkSize}</strong>
            </label>
            <input
              id="chunk-size"
              type="range"
              min="0"
              max="4096"
              step="1"
              value={4096 - chunkSize}
              oninput={(e) => chunkSize = 4096 - Number(e.currentTarget.value)}
            />
            <p class="param-desc">Divide la canción en trozos de N segundos para procesarla por partes. 0 = canción completa (máxima calidad, más VRAM). Reduce el uso de VRAM en canciones largas. Los trozos se unen con solapamiento suave para evitar artefactos en las uniones.</p>
            <div class="slider-labels">
              <span class="slider-min">4096 — 🧩 Troceado / -VRAM / -Calidad / +Rapidez</span>
              <span class="slider-max">0 — 🎵 Completa / +VRAM / +Calidad / -Rapidez</span>
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
            <p class="param-desc">NO afecta a la calidad. MULTIPLICA la VRAM del resto de parámetros: procesa varios chunks en paralelo. Con segmentos grandes o canciones largas, un batch alto agota la GPU (ej: SS1024 + batch 2 = 15 GB).</p>
            <div class="slider-labels">
              <span class="slider-min">0 — 🤖 Auto / -VRAM / =Calidad</span>
              <span class="slider-max">32 — ⚡ Paralelo / +VRAM / =Calidad</span>
            </div>
          </div>
        {/if}

        {#if isMdx || isScnet || isMdxNet}
          <!-- Segment Size -->
          <div class="field">
            <label for="seg-size-mdx">
              Segment Size: <strong>{segmentSize}</strong>
            </label>
            <input
              id="seg-size-mdx"
              type="range"
              min="64"
              max="1024"
              step="64"
              bind:value={segmentSize}
            />
            <p class="param-desc">Tamaño del chunk de análisis en frames. Más grande = mejor contexto y calidad, pero más VRAM.</p>
            <div class="slider-labels">
              <span class="slider-min">64 — ⚡ Rápido / -VRAM / -Calidad</span>
              <span class="slider-max">1024 — 🐌 Lento / +VRAM / +Calidad</span>
            </div>
          </div>

          <!-- Overlap -->
          <div class="field">
            <label for="overlap-mdx">
              Overlap: <strong>{formatOverlap(overlap)}</strong>
            </label>
            <input
              id="overlap-mdx"
              type="range"
              min="0"
              max="0.5"
              step="0.05"
              bind:value={overlap}
            />
            <p class="param-desc">Solapamiento entre chunks. Más overlap suaviza las transiciones pero ralentiza el proceso. NO multiplica VRAM.</p>
            <div class="slider-labels">
              <span class="slider-min">0 — ⚡ Rápido / =VRAM / -Calidad</span>
              <span class="slider-max">0.5 — 🐌 Lento / =VRAM / +Calidad</span>
            </div>
          </div>

          <!-- Batch Size -->
          <div class="field">
            <label for="batch-size-mdx">
              Batch Size: <strong>{batchSize === 0 ? 1 : batchSize}</strong>
            </label>
            <input
              id="batch-size-mdx"
              type="range"
              min="1"
              max="8"
              step="1"
              bind:value={batchSize}
            />
            <p class="param-desc">NO afecta a la calidad. Procesa N chunks en paralelo y MULTIPLICA la VRAM (igual que en Roformer).</p>
            <div class="slider-labels">
              <span class="slider-min">1 — 🐢 Mínimo / -VRAM / =Calidad</span>
              <span class="slider-max">8 — ⚡ Paralelo / +VRAM / =Calidad</span>
            </div>
          </div>

          <!-- Chunk Size (SCNet only) -->
          {#if isScnet}
            <div class="field">
              <label for="chunk-size-scnet">
                Chunk Size: <strong>{chunkSize === 0 ? 'YAML óptimo' : chunkSize}</strong>
              </label>
              <input
                id="chunk-size-scnet"
                type="range"
                min="0"
                max="1000000"
                step="10000"
                bind:value={chunkSize}
              />
              <p class="param-desc">Tamaño de chunk de audio en samples para SCNet. 0 = usa el valor óptimo definido en el YAML del modelo.</p>
              <div class="slider-labels">
                <span class="slider-min">0 — 🤖 YAML óptimo / +Calidad / -VRAM</span>
                <span class="slider-max">1000000 — 🧩 Samples / =Calidad / +VRAM</span>
              </div>
            </div>
          {/if}
        {/if}

        <!-- Device -->
        <div class="field">
          <label for="device">Device:</label>
          <select id="device" bind:value={device}>
            <option value="cuda">cuda</option>
            <option value="cpu">cpu</option>
          </select>
          <p class="param-desc">Dispositivo de inferencia. CUDA usa la GPU (más rápido, requiere VRAM). CPU es más lento pero no usa VRAM.</p>
        </div>

        <!-- Demucs PyTorch params (only for Demucs type) -->
        {#if isDemucs}
          <div class="demucs-section">
            <h3 class="demucs-title">🎛️ Parámetros Demucs</h3>

            <!-- Shifts -->
            <div class="field">
              <label for="demucs-shifts">
                Shifts: <strong>{shifts}</strong>
              </label>
              <input
                id="demucs-shifts"
                type="range"
                min="0"
                max="20"
                step="1"
                bind:value={shifts}
              />
              <p class="param-desc">Número de variaciones por shift para estabilización. Más shifts = mejor calidad, más lento y más VRAM. El paper original de Demucs usa 10.</p>
              <div class="slider-labels">
                <span class="slider-min">0 — ⚡ Rápido / -VRAM / -Calidad</span>
                <span class="slider-max">20 — 🐌 Lento / +VRAM / +Calidad (paper 10)</span>
              </div>
            </div>

            <!-- Segment -->
            <div class="field">
              <label for="demucs-segment">
                Segment: <strong>{segment === 0 ? 'auto' : segment + 's'}</strong>
              </label>
              <input
                id="demucs-segment"
                type="range"
                min="0"
                max="7"
                step="1"
                bind:value={segment}
              />
              <p class="param-desc">Duración del segmento en segundos. El valor máximo (7s) es el que MENOS VRAM usa (1.1 GB); valores 1-4 o auto usan ~1.6 GB. Máximo configurable 7s porque el límite interno del modelo es 7.8s y el CLI de demucs solo acepta valores enteros.</p>
              <div class="slider-labels">
                <span class="slider-min">0/auto — ⚡ Rápido / +VRAM / =Calidad</span>
                <span class="slider-max">7 — 🐌 Lento / -VRAM / =Calidad</span>
              </div>
            </div>

            <!-- Jobs -->
            <div class="field">
              <label for="demucs-jobs">
                Jobs: <strong>{jobs === 0 ? 'auto' : jobs}</strong>
              </label>
              <input
                id="demucs-jobs"
                type="range"
                min="0"
                max="8"
                step="1"
                bind:value={jobs}
              />
              <p class="param-desc">NO afecta a la calidad ni a la VRAM. Número de workers paralelos; 0 = automático. Solo cambia la velocidad.</p>
              <div class="slider-labels">
                <span class="slider-min">0 — 🤖 Auto / =Calidad / =VRAM</span>
                <span class="slider-max">8 — ⚡ Paralelo / =Calidad / =VRAM</span>
              </div>
            </div>
          </div>
        {/if}

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

  .btn-back {
    background: none;
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--accent-light);
    font-size: 0.85rem;
    padding: 0.3rem 0.8rem;
    cursor: pointer;
    transition: border-color 0.15s;
  }
  .btn-back:hover {
    border-color: var(--accent);
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

  .param-desc {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin-top: 2px;
    margin-bottom: 4px;
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

  /* Read-only YAML params (MDX / SCNet) */
  .readonly-params {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .readonly-title {
    margin: 0;
    font-size: 0.85rem;
    color: var(--accent-light);
    font-weight: 600;
  }

  .readonly-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.5rem;
  }

  .readonly-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.4rem 0.6rem;
    font-size: 0.8rem;
  }

  .readonly-key {
    color: var(--text-secondary);
  }

  .readonly-value {
    color: var(--accent-light);
    font-weight: 600;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }

  /* Demucs section */
  .demucs-section {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .demucs-title {
    margin: 0;
    font-size: 0.85rem;
    color: var(--accent-light);
    font-weight: 600;
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
</style>
