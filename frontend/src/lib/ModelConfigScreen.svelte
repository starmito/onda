<script lang="ts">
  import { getModelList, getLocalModels, downloadModel, uploadModel, getGpuInfo } from './api';
  import type { LocalModel, GpuInfo } from './api';

  let {
    modelInfos = [],
    onclose,
  }: {
    modelInfos?: LocalModel[];
    onclose: () => void;
  } = $props();

  // ---- Internal state ----
  let models = $state<LocalModel[]>([]);
  let modelsLoading = $state(false);
  let selectedModel = $state<LocalModel | null>(null);

  // Sync prop to internal state
  $effect(() => {
    if (modelInfos.length > 0) {
      models = modelInfos;
    }
  });

  // Config values for selected model
  let segmentSize = $state(256);
  let overlap = $state(0.50);
  let batchSize = $state(1);

  // Whether config was loaded from localStorage (to avoid overwriting)
  let configLoaded = $state(false);
  let defaultsLoaded = $state(false);

  // Default values for the selected model
  let defaultSegmentSize = $state(256);
  let defaultOverlap = $state(0.50);
  let defaultBatchSize = $state(1);

  // HuggingFace downloader
  let hfRepo = $state('');
  let hfLoading = $state(false);
  let hfStatus = $state<'idle' | 'downloading' | 'done' | 'error'>('idle');
  let hfMessage = $state('');

  // PC uploader
  let selectedFile = $state<File | null>(null);
  let uploadLoading = $state(false);
  let uploadStatus = $state<'idle' | 'uploading' | 'done' | 'error'>('idle');
  let uploadMessage = $state('');

  // GPU info for VRAM bar
  let gpuInfo = $state<GpuInfo | null>(null);

  // Categories
  const KNOWN_CATEGORIES = ['VR', 'MDX-Net', 'RoFormer', 'Demucs'] as const;

  // ---- VRAM table (hardcoded) ----
  const VRAM_TABLE: Record<string, number> = {
    melband_kj: 3200,
    melband_roformer: 4200,
    polarformer: 4800,
    viperx: 3800,
    htdemucs_ft: 2800,
    htdemucs_drums: 800,
    htdemucs_bass: 800,
    htdemucs_other: 800,
    mdx_kim_vocal_2: 800,
  };
  const DEFAULT_VRAM = 2000;

  // ---- Factor tables ----
  const OVERLAP_FACTORS: Record<number, number> = {
    0.25: 0.7,
    0.50: 1.0,
    0.75: 1.3,
  };

  const BATCH_FACTORS: Record<number, number> = {
    1: 1.0,
    2: 1.8,
    4: 3.2,
    8: 5.8,
  };

  // ---- Derived VRAM ----
  let baseVram = $derived(
    selectedModel ? (VRAM_TABLE[selectedModel.name] ?? DEFAULT_VRAM) : 0,
  );

  let adjustedVram = $derived(
    Math.round(
      baseVram *
        (segmentSize / 256) *
        (OVERLAP_FACTORS[overlap] ?? 1.0) *
        (BATCH_FACTORS[batchSize] ?? 1.0),
    ),
  );

  let vramTotal = $derived(gpuInfo?.vram_total_mb ?? 0);
  let vramPercent = $derived(
    vramTotal > 0 ? Math.round((adjustedVram / vramTotal) * 100) : 0,
  );

  let vramColor = $derived(
    vramPercent < 50 ? '#4caf50' : vramPercent < 80 ? '#f4a236' : '#ff5252',
  );

  // ---- Lifecycle ----
  $effect(() => {
    loadGpuInfo();
    refreshModels();
  });

  // When selected model changes, load its config from localStorage
  $effect(() => {
    if (selectedModel) {
      loadConfigFromStorage(selectedModel.name);
      // Also update defaults based on selected model
      defaultSegmentSize = 256;
      defaultOverlap = 0.50;
      defaultBatchSize = 1;
      defaultsLoaded = true;
    } else {
      configLoaded = false;
      defaultsLoaded = false;
    }
  });

  // ---- GPU info ----
  async function loadGpuInfo() {
    try {
      gpuInfo = await getGpuInfo();
    } catch {
      // GPU info not available — vramTotal stays 0
    }
  }

  // ---- Model list ----
  async function refreshModels() {
    modelsLoading = true;
    try {
      const res = await getLocalModels();
      models = res.models || [];
    } catch {
      // Keep existing models
    } finally {
      modelsLoading = false;
    }
  }

  // ---- Category grouping ----
  function groupByCategory(modelList: LocalModel[]): Record<string, LocalModel[]> {
    const groups: Record<string, LocalModel[]> = {};
    for (const cat of KNOWN_CATEGORIES) {
      groups[cat] = [];
    }
    groups['Otros'] = [];

    for (const m of modelList) {
      const cat = KNOWN_CATEGORIES.includes(m.category as any) ? m.category : 'Otros';
      groups[cat].push(m);
    }

    // Remove empty
    for (const key of [...KNOWN_CATEGORIES, 'Otros'] as string[]) {
      if (groups[key] && groups[key].length === 0) {
        delete groups[key];
      }
    }

    return groups;
  }

  // ---- Model selection ----
  function selectModel(model: LocalModel) {
    if (selectedModel?.name === model.name) {
      // Deselect
      selectedModel = null;
    } else {
      selectedModel = model;
    }
  }

  // ---- Config persistence ----
  function loadConfigFromStorage(modelName: string) {
    try {
      const raw = localStorage.getItem(`onda_model_config_${modelName}`);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (typeof parsed.segmentSize === 'number') segmentSize = parsed.segmentSize;
        if (typeof parsed.overlap === 'number') overlap = parsed.overlap;
        if (typeof parsed.batchSize === 'number') batchSize = parsed.batchSize;
      } else {
        // No saved config — use defaults
        segmentSize = 256;
        overlap = 0.50;
        batchSize = 1;
      }
    } catch {
      segmentSize = 256;
      overlap = 0.50;
      batchSize = 1;
    }
    configLoaded = true;
  }

  function saveConfig() {
    if (!selectedModel) return;
    const data = { segmentSize, overlap, batchSize };
    localStorage.setItem(
      `onda_model_config_${selectedModel.name}`,
      JSON.stringify(data),
    );
  }

  function resetDefaults() {
    segmentSize = defaultSegmentSize;
    overlap = defaultOverlap;
    batchSize = defaultBatchSize;
  }

  // ---- HuggingFace download ----
  async function handleHfDownload() {
    const repo = hfRepo.trim();
    if (!repo) return;

    hfLoading = true;
    hfStatus = 'downloading';
    hfMessage = '';

    try {
      const res = await downloadModel(repo);
      hfStatus = 'done';
      hfMessage = res.message || 'Modelo descargado correctamente';
      hfRepo = '';
      await refreshModels();
    } catch (err: any) {
      hfStatus = 'error';
      hfMessage = err.message || 'Error al descargar el modelo';
    } finally {
      hfLoading = false;
    }
  }

  // ---- PC upload ----
  function handleFileSelected(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0] || null;
    selectedFile = file;
    uploadStatus = 'idle';
    uploadMessage = '';
  }

  async function handleUpload() {
    if (!selectedFile) return;

    uploadLoading = true;
    uploadStatus = 'uploading';
    uploadMessage = '';

    try {
      await uploadModel(selectedFile);
      uploadStatus = 'done';
      uploadMessage = 'Modelo subido correctamente';
      selectedFile = null;
      await refreshModels();
    } catch (err: any) {
      uploadStatus = 'error';
      uploadMessage = err.message || 'Error al subir el modelo';
    } finally {
      uploadLoading = false;
    }
  }

  // ---- Formatting ----
  function formatSize(mb: number): string {
    if (!mb || mb <= 0) return '';
    return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb.toFixed(0)} MB`;
  }

  function formatVram(mb: number): string {
    return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`;
  }

  // ---- Dropdown values ----
  const SEGMENT_OPTIONS = [128, 256, 512, 1024, 2048];
  const OVERLAP_OPTIONS = [0.25, 0.50, 0.75];
  const BATCH_OPTIONS = [1, 2, 4, 8];
</script>

<div class="screen">
  <!-- Header -->
  <header class="screen-header">
    <button class="back-btn" onclick={onclose}>🟢 Back</button>
    <h1 class="screen-title">📦 Model Configuration</h1>
  </header>

  <!-- Top bar: HF download + PC upload -->
  <div class="top-bar">
    <!-- HuggingFace -->
    <div class="top-section">
      <span class="top-label">⬇ HuggingFace</span>
      <div class="top-row">
        <input
          type="text"
          class="hf-input"
          placeholder="Repo ID (ej: StemSplitio/htdemucs-ft-onnx)"
          bind:value={hfRepo}
          disabled={hfLoading}
        />
        <button
          class="action-btn download-btn"
          disabled={hfLoading || !hfRepo.trim()}
          onclick={handleHfDownload}
        >
          {hfLoading ? 'Descargando...' : '⬇ Descargar'}
        </button>
      </div>
      {#if hfStatus === 'downloading'}
        <p class="status-text downloading">⏳ Descargando modelo...</p>
      {:else if hfStatus === 'done'}
        <p class="status-text success">✅ {hfMessage}</p>
      {:else if hfStatus === 'error'}
        <p class="status-text error">❌ {hfMessage}</p>
      {/if}
    </div>

    <!-- PC Upload -->
    <div class="top-section">
      <span class="top-label">📂 Desde PC</span>
      <div class="top-row">
        <label class="file-label">
          <input
            type="file"
            accept=".pth,.onnx,.ckpt,.th,.safetensors"
            class="file-input"
            onchange={handleFileSelected}
            disabled={uploadLoading}
          />
          <span class="file-placeholder">
            {#if selectedFile}
              {selectedFile.name} ({formatSize(selectedFile.size / (1024 * 1024))})
            {:else}
              Seleccionar modelo...
            {/if}
          </span>
        </label>
        <button
          class="action-btn upload-btn"
          disabled={uploadLoading || !selectedFile}
          onclick={handleUpload}
        >
          {uploadLoading ? 'Subiendo...' : '📂 Subir'}
        </button>
      </div>
      {#if uploadStatus === 'uploading'}
        <p class="status-text downloading">⏳ Subiendo modelo...</p>
      {:else if uploadStatus === 'done'}
        <p class="status-text success">✅ {uploadMessage}</p>
      {:else if uploadStatus === 'error'}
        <p class="status-text error">❌ {uploadMessage}</p>
      {/if}
    </div>
  </div>

  <!-- Main content: left list + right config -->
  <div class="main-content">
    <!-- Left panel: model list -->
    <aside class="left-panel">
      <h2 class="panel-title">Modelos</h2>

      {#if modelsLoading}
        <p class="info-text">⏳ Cargando...</p>
      {:else if models.length === 0}
        <p class="info-text">No hay modelos instalados</p>
      {:else}
        <div class="model-list-scroll">
          {#each Object.entries(groupByCategory(models)) as [category, categoryModels]}
            <div class="category-group">
              <h3 class="category-name">{category}</h3>
              {#each categoryModels as model}
                <button
                  class="model-row"
                  class:selected={selectedModel?.name === model.name}
                  onclick={() => selectModel(model)}
                >
                  <span class="model-row-name">{model.name}</span>
                  <span class="model-row-size">{formatSize(model.size_mb)}</span>
                  <span class="model-row-cat">{model.category}</span>
                </button>
              {/each}
            </div>
          {/each}
        </div>
      {/if}
    </aside>

    <!-- Right panel: model configurator -->
    <section class="right-panel">
      {#if selectedModel}
        <h2 class="panel-title">Configuración: {selectedModel.name}</h2>

        <!-- Model info -->
        <div class="model-info">
          <div class="info-row">
            <span class="info-label">Model:</span>
            <span class="info-value">{selectedModel.name}</span>
          </div>
          <div class="info-row">
            <span class="info-label">Type:</span>
            <span class="info-value">{selectedModel.category}</span>
          </div>
          <div class="info-row">
            <span class="info-label">Path:</span>
            <span class="info-value path">{selectedModel.path}</span>
          </div>
        </div>

        <!-- Parameters -->
        <h3 class="section-subtitle">⚙️ Parameters</h3>

        <div class="param-group">
          <!-- Segment Size -->
          <label class="param-field">
            <span class="param-label">Segment Size:</span>
            <select
              class="param-select"
              bind:value={segmentSize}
            >
              {#each SEGMENT_OPTIONS as opt}
                <option value={opt}>{opt}</option>
              {/each}
            </select>
            <span class="param-desc">Samples per chunk. Más = +VRAM, +calidad</span>
          </label>

          <!-- Overlap -->
          <label class="param-field">
            <span class="param-label">Overlap:</span>
            <select
              class="param-select"
              bind:value={overlap}
            >
              {#each OVERLAP_OPTIONS as opt}
                <option value={opt}>{opt.toFixed(2)}</option>
              {/each}
            </select>
            <span class="param-desc">Chunk overlap. Menos = -VRAM, posible artefactos</span>
          </label>

          <!-- Batch Size -->
          <label class="param-field">
            <span class="param-label">Batch Size:</span>
            <select
              class="param-select"
              bind:value={batchSize}
            >
              {#each BATCH_OPTIONS as opt}
                <option value={opt}>{opt}</option>
              {/each}
            </select>
            <span class="param-desc">Paralelismo. ×2 batch = ~×1.8 VRAM</span>
          </label>
        </div>

        <!-- VRAM estimator -->
        <div class="vram-section">
          <div class="vram-header">
            <span>💾 VRAM: ~{formatVram(adjustedVram)}</span>
            {#if vramTotal > 0}
              <span>/ {formatVram(vramTotal)}</span>
            {/if}
            {#if vramTotal > 0}
              <span class="vram-pct">({vramPercent}%)</span>
            {/if}
          </div>
          <div class="vram-bar-bg">
            <div
              class="vram-bar-fill"
              style="width: {Math.min(vramPercent, 100)}%; background: {vramColor};"
            ></div>
          </div>
          <div class="vram-footer">
            <span>base: {formatVram(baseVram)}</span>
            <span>seg: {segmentSize}</span>
            <span>overlap: {overlap.toFixed(2)}</span>
            <span>batch: {batchSize}</span>
          </div>
        </div>

        <!-- Save / Reset -->
        <div class="action-buttons">
          <button class="save-btn" onclick={saveConfig}>💾 Save</button>
          <button class="reset-btn" onclick={resetDefaults}>🔄 Reset Defaults</button>
        </div>
      {:else}
        <div class="empty-state">
          <p>Selecciona un modelo de la lista para configurarlo</p>
        </div>
      {/if}
    </section>
  </div>
</div>

<style>
  .screen {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: #0a0a14;
    color: #e0e0e0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto,
      Oxygen-Sans, Ubuntu, Cantarell, 'Helvetica Neue', sans-serif;
    overflow: hidden;
  }

  /* ---- Header ---- */
  .screen-header {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.75rem 1.5rem;
    background: #1a1a2e;
    border-bottom: 2px solid transparent;
    border-image: linear-gradient(
        90deg,
        rgba(0, 212, 255, 0.3),
        rgba(0, 212, 255, 0.05)
      )
      1;
    flex-shrink: 0;
  }

  .back-btn {
    padding: 0.4rem 1rem;
    background: #22223a;
    color: #00d4ff;
    border: 1px solid #333;
    border-radius: 6px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s;
  }
  .back-btn:hover {
    background: #2a2a44;
  }

  .screen-title {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 700;
    background: linear-gradient(135deg, #00d4ff, #b388ff);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  /* ---- Top bar ---- */
  .top-bar {
    display: flex;
    gap: 1rem;
    padding: 0.75rem 1.5rem;
    background: #111122;
    border-bottom: 1px solid #1a1a2e;
    flex-shrink: 0;
  }

  .top-section {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .top-label {
    font-size: 0.8rem;
    font-weight: 600;
    color: #888;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .top-row {
    display: flex;
    gap: 0.5rem;
  }

  .hf-input {
    flex: 1;
    padding: 0.45rem 0.65rem;
    background: #111;
    color: #e0e0e0;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.82rem;
    outline: none;
  }
  .hf-input:focus {
    border-color: #00d4ff;
  }
  .hf-input:disabled {
    opacity: 0.5;
  }
  .hf-input::placeholder {
    color: #555;
    font-size: 0.78rem;
  }

  /* File input */
  .file-label {
    flex: 1;
    display: flex;
    align-items: center;
    padding: 0.45rem 0.65rem;
    background: #111;
    border: 1px solid #444;
    border-radius: 6px;
    cursor: pointer;
    transition: border-color 0.2s;
  }
  .file-label:hover {
    border-color: #666;
  }

  .file-input {
    display: none;
  }

  .file-placeholder {
    color: #666;
    font-size: 0.82rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Action buttons */
  .action-btn {
    padding: 0.45rem 1.1rem;
    border: none;
    border-radius: 6px;
    font-size: 0.82rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background-color 0.2s, opacity 0.2s;
  }

  .download-btn {
    background: #00d4ff;
    color: #111;
  }
  .download-btn:hover:not(:disabled) {
    background: #00b8e0;
  }

  .upload-btn {
    background: #7c4dff;
    color: #fff;
  }
  .upload-btn:hover:not(:disabled) {
    background: #6a3ae0;
  }

  .action-btn:disabled {
    background: #444;
    color: #888;
    cursor: not-allowed;
    opacity: 0.7;
  }

  /* Status texts */
  .status-text {
    margin: 0;
    font-size: 0.78rem;
  }
  .status-text.downloading {
    color: #00d4ff;
  }
  .status-text.success {
    color: #4caf50;
  }
  .status-text.error {
    color: #ff5252;
  }

  /* ---- Main content ---- */
  .main-content {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  /* ---- Left panel ---- */
  .left-panel {
    width: 280px;
    min-width: 240px;
    background: #111122;
    border-right: 1px solid #1a1a2e;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .panel-title {
    margin: 0;
    padding: 0.75rem 1rem;
    font-size: 0.9rem;
    font-weight: 600;
    color: #aaa;
    border-bottom: 1px solid #1a1a2e;
    flex-shrink: 0;
  }

  .info-text {
    padding: 1rem;
    margin: 0;
    color: #666;
    font-size: 0.82rem;
    text-align: center;
  }

  .model-list-scroll {
    flex: 1;
    overflow-y: auto;
    padding: 0.5rem 0;
  }

  .category-group {
    margin-bottom: 0.25rem;
  }

  .category-name {
    margin: 0;
    padding: 0.4rem 1rem 0.2rem;
    font-size: 0.7rem;
    font-weight: 700;
    color: #00d4ff;
    text-transform: uppercase;
    letter-spacing: 1px;
  }

  .model-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.45rem 1rem 0.45rem 1.5rem;
    background: none;
    border: none;
    color: #c0c0c0;
    font-size: 0.8rem;
    cursor: pointer;
    text-align: left;
    transition: background 0.15s;
  }
  .model-row:hover {
    background: #1a1a2e;
  }
  .model-row.selected {
    background: rgba(0, 212, 255, 0.08);
    border-left: 3px solid #00d4ff;
    padding-left: calc(1.5rem - 3px);
    color: #e0e0e0;
  }

  .model-row-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
  }

  .model-row-size {
    color: #00d4ff;
    font-size: 0.72rem;
    flex-shrink: 0;
  }

  .model-row-cat {
    color: #555;
    font-size: 0.68rem;
    flex-shrink: 0;
  }

  /* ---- Right panel ---- */
  .right-panel {
    flex: 1;
    overflow-y: auto;
    padding: 1.25rem 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .right-panel .panel-title {
    padding: 0 0 0.5rem 0;
    border-bottom: 1px solid #1a1a2e;
    font-size: 1rem;
    color: #e0e0e0;
  }

  .section-subtitle {
    margin: 0.5rem 0 0 0;
    font-size: 0.85rem;
    font-weight: 600;
    color: #aaa;
  }

  /* Model info */
  .model-info {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0.75rem;
    background: #1a1a2e;
    border-radius: 8px;
  }

  .info-row {
    display: flex;
    gap: 0.5rem;
    font-size: 0.82rem;
  }

  .info-label {
    color: #888;
    min-width: 50px;
    flex-shrink: 0;
  }

  .info-value {
    color: #e0e0e0;
    font-weight: 500;
  }

  .info-value.path {
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.72rem;
    color: #888;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Parameter fields */
  .param-group {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 0.75rem;
    background: #1a1a2e;
    border-radius: 8px;
  }

  .param-field {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }

  .param-label {
    font-size: 0.82rem;
    font-weight: 600;
    color: #ccc;
  }

  .param-select {
    padding: 0.45rem 0.65rem;
    background: #111;
    color: #e0e0e0;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.85rem;
    cursor: pointer;
    outline: none;
    max-width: 180px;
    transition: border-color 0.2s;
  }
  .param-select:hover {
    border-color: #666;
  }
  .param-select:focus {
    border-color: #00d4ff;
    box-shadow: 0 0 8px rgba(0, 212, 255, 0.15);
  }

  .param-desc {
    font-size: 0.7rem;
    color: #666;
  }

  /* VRAM section */
  .vram-section {
    padding: 0.75rem;
    background: #1a1a2e;
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .vram-header {
    display: flex;
    gap: 0.25rem;
    font-size: 0.82rem;
    font-weight: 600;
    color: #e0e0e0;
  }

  .vram-pct {
    color: #888;
    font-weight: 500;
  }

  .vram-bar-bg {
    width: 100%;
    height: 10px;
    background: #111;
    border-radius: 5px;
    overflow: hidden;
  }

  .vram-bar-fill {
    height: 100%;
    border-radius: 5px;
    transition: width 0.3s ease, background 0.3s ease;
    min-width: 2px;
  }

  .vram-footer {
    display: flex;
    gap: 0.75rem;
    font-size: 0.68rem;
    color: #555;
  }

  /* Action buttons (Save / Reset) */
  .action-buttons {
    display: flex;
    gap: 0.75rem;
    padding-top: 0.5rem;
  }

  .save-btn,
  .reset-btn {
    padding: 0.55rem 1.5rem;
    border: none;
    border-radius: 6px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s;
  }

  .save-btn {
    background: #00d4ff;
    color: #111;
  }
  .save-btn:hover {
    background: #00b8e0;
  }

  .reset-btn {
    background: #333;
    color: #e0e0e0;
    border: 1px solid #555;
  }
  .reset-btn:hover {
    background: #444;
  }

  /* Empty state */
  .empty-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #555;
    font-size: 0.9rem;
  }

  /* Animations */
  .right-panel {
    animation: fadeIn 0.25s ease;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  /* Responsive: stack vertically on mobile */
  @media (max-width: 700px) {
    .main-content {
      flex-direction: column;
    }

    .left-panel {
      width: 100%;
      min-width: unset;
      max-height: 40vh;
      border-right: none;
      border-bottom: 1px solid #1a1a2e;
    }

    .top-bar {
      flex-direction: column;
    }
  }
</style>
