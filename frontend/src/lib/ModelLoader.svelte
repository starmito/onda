<script lang="ts">
  import { getLocalModels, downloadModel, uploadModel } from './api';
  import type { LocalModel } from './api';

  let localModels = $state<LocalModel[]>([]);
  let modelsLoading = $state(false);
  let modelsError = $state('');

  // HuggingFace download
  let hfRepo = $state('');
  let hfDownloading = $state(false);
  let hfStatus = $state<'idle' | 'downloading' | 'complete' | 'error'>('idle');
  let hfMessage = $state('');

  // PC upload
  let selectedFile = $state<File | null>(null);
  let uploadLoading = $state(false);
  let uploadStatus = $state<'idle' | 'uploading' | 'complete' | 'error'>('idle');
  let uploadMessage = $state('');

  // Categories for grouping
  const KNOWN_CATEGORIES = ['VR', 'MDX-Net', 'RoFormer', 'Demucs'] as const;

  $effect(() => {
    loadLocalModels();
  });

  async function loadLocalModels() {
    modelsLoading = true;
    modelsError = '';
    try {
      const res = await getLocalModels();
      localModels = res.models || [];
    } catch (err: any) {
      modelsError = err.message || 'Error al cargar modelos locales';
      localModels = [];
    } finally {
      modelsLoading = false;
    }
  }

  function groupByCategory(models: LocalModel[]): Record<string, LocalModel[]> {
    const groups: Record<string, LocalModel[]> = {};
    for (const cat of KNOWN_CATEGORIES) {
      groups[cat] = [];
    }
    groups['Otros'] = [];

    for (const m of models) {
      const cat = KNOWN_CATEGORIES.includes(m.category as any) ? m.category : 'Otros';
      groups[cat].push(m);
    }

    // Remove empty known categories
    for (const cat of KNOWN_CATEGORIES) {
      if (groups[cat].length === 0) {
        delete groups[cat];
      }
    }
    if (groups['Otros'].length === 0) {
      delete groups['Otros'];
    }

    return groups;
  }

  async function handleHfDownload() {
    const repo = hfRepo.trim();
    if (!repo) return;

    hfDownloading = true;
    hfStatus = 'downloading';
    hfMessage = '';

    try {
      const res = await downloadModel(repo);
      hfStatus = 'complete';
      hfMessage = res.message || 'Modelo descargado correctamente';
      hfRepo = '';
      // Refresh local model list
      await loadLocalModels();
    } catch (err: any) {
      hfStatus = 'error';
      hfMessage = err.message || 'Error al descargar el modelo';
    } finally {
      hfDownloading = false;
    }
  }

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
      uploadStatus = 'complete';
      uploadMessage = 'Modelo subido correctamente';
      selectedFile = null;
      // Refresh local model list
      await loadLocalModels();
    } catch (err: any) {
      uploadStatus = 'error';
      uploadMessage = err.message || 'Error al subir el modelo';
    } finally {
      uploadLoading = false;
    }
  }

  function formatSize(mb: number): string {
    return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb.toFixed(0)} MB`;
  }
</script>

<div class="model-loader">
  <!-- 📦 Modelos Locales -->
  <section class="section">
    <h3 class="section-title">📦 Modelos Locales</h3>

    {#if modelsLoading}
      <p class="info-text">⏳ Cargando modelos...</p>
    {:else if modelsError}
      <p class="error-text">⚠️ {modelsError}</p>
      <button class="retry-btn" onclick={loadLocalModels}>Reintentar</button>
    {:else if localModels.length === 0}
      <p class="info-text">No hay modelos locales instalados</p>
    {:else}
      {#each Object.entries(groupByCategory(localModels)) as [category, models]}
        <div class="category-group">
          <h4 class="category-name">{category}</h4>
          <div class="model-list">
            {#each models as model}
              <div class="model-item">
                <span class="model-name">{model.name}</span>
                <span class="model-size">{formatSize(model.size_mb)}</span>
                <span class="model-path" title={model.path}>{model.path}</span>
              </div>
            {/each}
          </div>
        </div>
      {/each}
    {/if}
  </section>

  <!-- 📥 Descargar desde HuggingFace -->
  <section class="section">
    <h3 class="section-title">📥 Descargar desde HuggingFace</h3>

    <div class="hf-download">
      <input
        type="text"
        class="hf-input"
        placeholder="Repo ID (ej: StemSplitio/htdemucs-ft-onnx)"
        bind:value={hfRepo}
        disabled={hfDownloading}
      />
      <button
        class="action-btn download-btn"
        disabled={hfDownloading || !hfRepo.trim()}
        onclick={handleHfDownload}
      >
        {#if hfDownloading}
          Descargando...
        {:else}
          Descargar
        {/if}
      </button>
    </div>

    {#if hfStatus === 'downloading'}
      <p class="status-text downloading">⏳ Descargando modelo desde HuggingFace...</p>
    {:else if hfStatus === 'complete'}
      <p class="status-text success">✅ {hfMessage}</p>
    {:else if hfStatus === 'error'}
      <p class="status-text error">❌ {hfMessage}</p>
    {/if}
  </section>

  <!-- 📂 Cargar desde PC -->
  <section class="section">
    <h3 class="section-title">📂 Cargar desde PC</h3>

    <div class="pc-upload">
      <label class="file-label">
        <input
          type="file"
          accept=".pth,.onnx,.ckpt"
          class="file-input"
          onchange={handleFileSelected}
          disabled={uploadLoading}
        />
        <span class="file-placeholder">
          {#if selectedFile}
            {selectedFile.name} ({formatSize(selectedFile.size / (1024 * 1024))})
          {:else}
            Seleccionar archivo (.pth, .onnx, .ckpt)
          {/if}
        </span>
      </label>

      <button
        class="action-btn upload-btn"
        disabled={uploadLoading || !selectedFile}
        onclick={handleUpload}
      >
        {#if uploadLoading}
          Subiendo...
        {:else}
          Subir modelo
        {/if}
      </button>
    </div>

    {#if uploadStatus === 'uploading'}
      <p class="status-text downloading">⏳ Subiendo modelo...</p>
    {:else if uploadStatus === 'complete'}
      <p class="status-text success">✅ {uploadMessage}</p>
    {:else if uploadStatus === 'error'}
      <p class="status-text error">❌ {uploadMessage}</p>
    {/if}
  </section>
</div>

<style>
  .model-loader {
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .section {
    padding: 1rem;
    background-color: #1a1a2e;
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .section-title {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: #e0e0e0;
    border-bottom: 1px solid #333;
    padding-bottom: 0.5rem;
  }

  /* Info / Error / Status texts */
  .info-text {
    margin: 0;
    color: #888;
    font-size: 0.85rem;
    text-align: center;
  }

  .error-text {
    margin: 0;
    color: #f4a236;
    font-size: 0.85rem;
    text-align: center;
  }

  .status-text {
    margin: 0;
    font-size: 0.85rem;
    text-align: center;
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

  .retry-btn {
    align-self: center;
    padding: 0.35rem 1rem;
    background-color: #333;
    color: #e0e0e0;
    border: 1px solid #555;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.85rem;
  }
  .retry-btn:hover {
    background-color: #444;
  }

  /* Category groups */
  .category-group {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .category-name {
    margin: 0;
    font-size: 0.8rem;
    font-weight: 600;
    color: #00d4ff;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .model-list {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .model-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.4rem 0.6rem;
    background-color: #111;
    border-radius: 6px;
    font-size: 0.8rem;
  }

  .model-name {
    font-weight: 500;
    color: #e0e0e0;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .model-size {
    color: #00d4ff;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .model-path {
    color: #666;
    font-size: 0.7rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-left: auto;
    max-width: 200px;
  }

  /* HuggingFace download */
  .hf-download {
    display: flex;
    gap: 0.5rem;
  }

  .hf-input {
    flex: 1;
    padding: 0.5rem 0.75rem;
    background-color: #111;
    color: #e0e0e0;
    border: 1px solid #444;
    border-radius: 6px;
    font-size: 0.85rem;
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
  }

  /* PC upload */
  .pc-upload {
    display: flex;
    gap: 0.5rem;
    align-items: stretch;
  }

  .file-label {
    flex: 1;
    display: flex;
    align-items: center;
    padding: 0.5rem 0.75rem;
    background-color: #111;
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
    color: #888;
    font-size: 0.85rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Action buttons */
  .action-btn {
    padding: 0.5rem 1.25rem;
    border: none;
    border-radius: 6px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background-color 0.2s, opacity 0.2s;
  }

  .download-btn {
    background-color: #00d4ff;
    color: #111;
  }
  .download-btn:hover:not(:disabled) {
    background-color: #00b8e0;
  }

  .upload-btn {
    background-color: #7c4dff;
    color: #fff;
  }
  .upload-btn:hover:not(:disabled) {
    background-color: #6a3ae0;
  }

  .action-btn:disabled {
    background-color: #444;
    color: #888;
    cursor: not-allowed;
    opacity: 0.7;
  }
</style>
