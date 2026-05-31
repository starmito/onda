<script lang="ts">
  import {
    getModelCatalog,
    getLocalModels,
    downloadModel,
    uploadModel,
    deleteModel,
    type UVRModelEntry,
    type LocalModel,
  } from './api';

  interface Props {
    onclose?: () => void;
  }

  let { onclose }: Props = $props();

  // ---- Tab state ----
  type Tab = 'download' | 'upload' | 'installed';
  let tab = $state<Tab>('download');
  let search = $state('');

  // ---- Catalog state ----
  let catalog = $state<UVRModelEntry[]>([]);
  let catalogLoading = $state(true);
  let catalogError = $state(false);

  // ---- Downloading state ----
  let downloading = $state<Set<string>>(new Set());
  let downloadErrors = $state<Map<string, string>>(new Map());

  // ---- Upload state ----
  let uploadMessage = $state('');
  let uploadMessageType = $state<'success' | 'error'>('success');
  let uploadingModel = $state(false);

  // ---- Installed models ----
  let localModels = $state<LocalModel[]>([]);
  let installedLoading = $state(true);
  let deleteFeedback = $state('');
  let deleteFeedbackType = $state<'success' | 'error'>('success');

  // Load catalog with safety timeout
  $effect(() => {
    let timeout: ReturnType<typeof setTimeout> | null = null;
    
    try {
      timeout = setTimeout(() => {
        catalogError = true;
        catalogLoading = false;
      }, 10000);

      // IIFE to safely call async function
      (async () => {
        try {
          const data = await getModelCatalog();
          if (timeout) clearTimeout(timeout);
          catalog = [...data];
          catalogLoading = false;
        } catch (err) {
          console.error('ModelDownloader: catalog load failed:', err);
          if (timeout) clearTimeout(timeout);
          catalogError = true;
          catalogLoading = false;
        }
      })();
    } catch (err) {
      // Failsafe: if even the setup crashes
      console.error('ModelDownloader: effect crashed:', err);
      catalogError = true;
      catalogLoading = false;
    }
  });

  // ---- Load installed models when tab changes ----
  $effect(() => {
    if (tab !== 'installed') return;
    installedLoading = true;
    getLocalModels()
      .then((res) => {
        localModels = res.models || [];
        installedLoading = false;
      })
      .catch(() => {
        localModels = [];
        installedLoading = false;
      });
  });

  // ---- Derived: filtered + grouped catalog ----
  let filtered = $derived.by(() => {
    if (!search) return catalog;
    const q = search.toLowerCase();
    return catalog.filter(
      (m) =>
        m.name.toLowerCase().includes(q) ||
        m.display_name?.toLowerCase().includes(q) ||
        m.description?.toLowerCase().includes(q),
    );
  });

  let groupedCatalog = $derived.by(() => {
    const groups: Record<string, UVRModelEntry[]> = {};
    for (const m of filtered) {
      const cat = m.category || 'Other';
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(m);
    }
    // Sort categories in display order
    const order = [
      'Roformer',
      'Roformer/MelBand',
      'MDX',
      'SCnet',
      'Demucs',
      'VR_Arch',
      'Other',
    ];
    const sorted: { category: string; models: UVRModelEntry[] }[] = [];
    for (const cat of order) {
      if (groups[cat] && groups[cat].length > 0) {
        sorted.push({ category: cat, models: groups[cat] });
        delete groups[cat];
      }
    }
    for (const [cat, m] of Object.entries(groups)) {
      sorted.push({ category: cat, models: m });
    }
    return sorted;
  });

  // ---- Actions ----
  async function refreshCatalog() {
    try {
      const data = await getModelCatalog();
      catalog = data;
    } catch {
      // Silently ignore refresh failures; existing catalog stays visible
    }
  }

  async function startDownload(model: UVRModelEntry) {
    if (!model.huggingface_repo) return;
    const set = new Set(downloading);
    set.add(model.filename);
    downloading = set;
    downloadErrors.delete(model.filename);
    downloadErrors = new Map(downloadErrors);

    try {
      await downloadModel(model.huggingface_repo);
      // Refresh catalog to update downloaded status
      await refreshCatalog();
    } catch (err: any) {
      const errors = new Map(downloadErrors);
      errors.set(model.filename, err.message || 'Download failed');
      downloadErrors = errors;
    } finally {
      const set2 = new Set(downloading);
      set2.delete(model.filename);
      downloading = set2;
    }
  }

  function formatSize(mb: number): string {
    if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB';
    return mb + ' MB';
  }

  // ---- Upload handlers ----
  function handleUploadDragOver(e: DragEvent) {
    e.preventDefault();
  }

  async function handleUploadDrop(e: DragEvent) {
    e.preventDefault();
    const files = e.dataTransfer?.files;
    if (!files || files.length === 0) return;
    await uploadFiles(Array.from(files));
  }

  async function handleUploadSelect(e: Event) {
    const input = e.target as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;
    await uploadFiles(Array.from(input.files));
    input.value = '';
  }

  async function uploadFiles(files: File[]) {
    const valid = files.filter((f) => {
      const ext = '.' + f.name.split('.').pop()?.toLowerCase();
      return ['.ckpt', '.pth', '.onnx', '.safetensors', '.pt'].includes(ext);
    });

    if (valid.length === 0) {
      uploadMessage = 'Solo archivos .ckpt, .pth, .onnx, .safetensors, .pt';
      uploadMessageType = 'error';
      setTimeout(() => (uploadMessage = ''), 3000);
      return;
    }

    uploadingModel = true;
    uploadMessage = '';
    let successCount = 0;
    let failCount = 0;

    for (const file of valid) {
      try {
        await uploadModel(file);
        successCount++;
      } catch {
        failCount++;
      }
    }

    uploadingModel = false;
    if (successCount > 0) {
      uploadMessage = `✅ ${successCount} modelo(s) subido(s)${failCount > 0 ? `, ${failCount} fallo(s)` : ''}`;
      uploadMessageType = 'success';
    } else {
      uploadMessage = '❌ Fallo al subir modelos';
      uploadMessageType = 'error';
    }
    setTimeout(() => (uploadMessage = ''), 4000);

    // Refresh installed tab if visible (will be reloaded on next tab switch)
    getLocalModels()
      .then((res) => (localModels = res.models || []))
      .catch(() => {});
    // Also refresh catalog to update downloaded/uninstalled status
    await refreshCatalog();
  }

  // ---- Delete handler ----
  async function handleDeleteModel(model: LocalModel) {
    try {
      await deleteModel(model.name);
      localModels = localModels.filter((m) => m.name !== model.name);
      deleteFeedback = `✅ "${model.name}" eliminado`;
      deleteFeedbackType = 'success';
      // Refresh catalog so the model shows as not-downloaded again
      await refreshCatalog();
    } catch (err: any) {
      deleteFeedback = `❌ Error: ${err.message}`;
      deleteFeedbackType = 'error';
    }
    setTimeout(() => (deleteFeedback = ''), 3000);
  }
</script>

<div class="backdrop" onclick={onclose} role="presentation">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="panel" onclick={(e: MouseEvent) => e.stopPropagation()} role="dialog">
    <div class="panel-header">
      <h2>📥 Modelos</h2>
      <button class="btn-close" onclick={onclose}>✕</button>
    </div>

    <!-- Tab bar -->
    <div class="tab-bar">
      <button
        class="tab-btn"
        class:active={tab === 'download'}
        onclick={() => { tab = 'download'; }}
      >
        📥 Descargar
      </button>
      <button
        class="tab-btn"
        class:active={tab === 'upload'}
        onclick={() => { tab = 'upload'; }}
      >
        📤 Subir
      </button>
      <button
        class="tab-btn"
        class:active={tab === 'installed'}
        onclick={() => { tab = 'installed'; }}
      >
        ✅ Instalados
      </button>
    </div>

    <div class="panel-body">
      <!-- ========= TAB: DOWNLOAD ========= -->
      {#if tab === 'download'}
        <!-- Search -->
        <div class="search-wrap">
          <input
            type="text"
            class="search-input"
            placeholder="Buscar modelos..."
            bind:value={search}
          />
        </div>

        {#if catalogLoading}
          <div class="empty-state">Cargando catálogo...</div>
        {:else if catalogError}
          <div class="empty-state error">Error al cargar el catálogo</div>
        {:else if filtered.length === 0}
          <div class="empty-state">
            {search ? 'Sin resultados para "' + search + '"' : 'Catálogo vacío'}
          </div>
        {:else}
          <div class="catalog-list">
            {#each groupedCatalog as group (group.category)}
              <div class="category-group">
                <h3 class="category-title">{group.category}</h3>
                {#each group.models as model (model.filename)}
                  <div class="model-row">
                    <div class="model-info">
                      <span class="model-name">{model.display_name || model.name}</span>
                      {#if model.description}
                        <span class="model-desc">{model.description}</span>
                      {/if}
                      <span class="model-size">{formatSize(model.size_mb)}</span>
                    </div>
                    <div class="model-action">
                      {#if model.downloaded}
                        <span class="check-icon" title="Ya instalado">✅</span>
                      {:else if downloading.has(model.filename)}
                        <span class="spinner">⏳</span>
                      {:else}
                        <button
                          class="btn-download"
                          onclick={() => startDownload(model)}
                          disabled={!model.huggingface_repo}
                        >
                          Descargar
                        </button>
                        {#if downloadErrors.has(model.filename)}
                          <span class="download-error" title={downloadErrors.get(model.filename)}>❌</span>
                        {/if}
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            {/each}
          </div>
        {/if}

      <!-- ========= TAB: UPLOAD ========= -->
      {:else if tab === 'upload'}
        <div
          class="dropzone"
          class:uploading={uploadingModel}
          ondragover={handleUploadDragOver}
          ondrop={handleUploadDrop}
          onclick={() => document.getElementById('model-upload-input')?.click()}
          role="button"
          tabindex="0"
        >
          <span class="dropzone-icon">📤</span>
          <span class="dropzone-text">
            {uploadingModel ? 'Subiendo...' : 'Arrastra archivos de modelo aquí o haz clic'}
          </span>
          <span class="dropzone-hint">.ckpt, .pth, .onnx, .safetensors, .pt</span>
        </div>
        <input
          id="model-upload-input"
          type="file"
          hidden
          accept=".ckpt,.pth,.onnx,.safetensors,.pt"
          multiple
          onchange={handleUploadSelect}
        />

        {#if uploadMessage}
          <div class="feedback" class:success={uploadMessageType === 'success'} class:error={uploadMessageType === 'error'}>
            {uploadMessage}
          </div>
        {/if}

      <!-- ========= TAB: INSTALLED ========= -->
      {:else if tab === 'installed'}
        {#if installedLoading}
          <div class="empty-state">Cargando...</div>
        {:else if localModels.length === 0}
          <div class="empty-state">No hay modelos instalados</div>
        {:else}
          <div class="installed-list">
            {#each localModels as model (model.name)}
              <div class="installed-row">
                <div class="installed-info">
                  <span class="installed-name">{model.display_name || model.name}</span>
                  <span class="installed-cat">{model.category}</span>
                  <span class="installed-size">{formatSize(model.size_mb)}</span>
                </div>
                <button class="btn-delete" onclick={() => handleDeleteModel(model)} title="Eliminar modelo">
                  🗑️
                </button>
              </div>
            {/each}
          </div>
        {/if}

        {#if deleteFeedback}
          <div class="feedback" class:success={deleteFeedbackType === 'success'} class:error={deleteFeedbackType === 'error'}>
            {deleteFeedback}
          </div>
        {/if}
      {/if}
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    left: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 950;
    display: flex;
    justify-content: flex-end;
  }

  .panel {
    width: 340px;
    max-width: 90vw;
    height: 100%;
    background: #1a1a2e;
    border-left: 1px solid #2a2a4a;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    animation: slideIn 0.25s ease;
  }

  @keyframes slideIn {
    from { transform: translateX(100%); }
    to { transform: translateX(0); }
  }

  .panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid #2a2a4a;
    flex-shrink: 0;
  }

  .panel-header h2 {
    margin: 0;
    font-size: 1.1rem;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .btn-close {
    background: none;
    border: none;
    color: #666;
    font-size: 1.1rem;
    cursor: pointer;
    padding: 0.25rem 0.5rem;
    flex-shrink: 0;
  }
  .btn-close:hover {
    color: #e57373;
  }

  /* Tab bar */
  .tab-bar {
    display: flex;
    border-bottom: 1px solid #2a2a4a;
    flex-shrink: 0;
  }

  .tab-btn {
    flex: 1;
    padding: 0.6rem 0.5rem;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: #666;
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab-btn:hover {
    color: #888;
  }
  .tab-btn.active {
    color: #00d4ff;
    border-bottom-color: #00d4ff;
  }

  .panel-body {
    padding: 1rem 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    flex: 1;
    overflow-y: auto;
  }

  /* Search */
  .search-wrap {
    flex-shrink: 0;
  }

  .search-input {
    width: 100%;
    box-sizing: border-box;
    padding: 0.45rem 0.75rem;
    background: #0e0e1a;
    border: 1px solid #2a2a4a;
    border-radius: 6px;
    color: #e0e0e0;
    font-size: 0.82rem;
    outline: none;
  }
  .search-input:focus {
    border-color: #00d4ff;
  }
  .search-input::placeholder {
    color: #555;
  }

  /* Empty state */
  .empty-state {
    text-align: center;
    color: #666;
    padding: 2rem 0;
    font-size: 0.85rem;
  }
  .empty-state.error {
    color: #e57373;
  }

  /* Catalog list */
  .catalog-list {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .category-group {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .category-title {
    margin: 0;
    font-size: 0.75rem;
    font-weight: 700;
    color: #b388ff;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    padding-bottom: 0.2rem;
    border-bottom: 1px solid #2a2a4a;
  }

  .model-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0.6rem;
    background: #0e0e1a;
    border: 1px solid #1a1a3a;
    border-radius: 6px;
    gap: 0.5rem;
  }

  .model-info {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
    flex: 1;
  }

  .model-name {
    font-size: 0.8rem;
    font-weight: 600;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .model-desc {
    font-size: 0.68rem;
    color: #777;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .model-size {
    font-size: 0.7rem;
    color: #606080;
    font-weight: 500;
  }

  .model-action {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    flex-shrink: 0;
  }

  .check-icon {
    font-size: 1rem;
  }

  .spinner {
    font-size: 1rem;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .btn-download {
    padding: 0.3rem 0.7rem;
    background: linear-gradient(135deg, #00d4ff, #0095cc);
    border: none;
    border-radius: 5px;
    color: #0a0a14;
    font-weight: 700;
    font-size: 0.7rem;
    cursor: pointer;
    white-space: nowrap;
    transition: opacity 0.15s;
  }
  .btn-download:hover {
    opacity: 0.85;
  }
  .btn-download:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .download-error {
    font-size: 0.9rem;
    cursor: help;
  }

  /* Upload dropzone */
  .dropzone {
    width: 100%;
    box-sizing: border-box;
    border: 2px dashed #2a2a4a;
    border-radius: 10px;
    padding: 2rem 1rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    background: #0e0e1a;
  }
  .dropzone:hover {
    border-color: #00d4ff;
    background: #111128;
  }
  .dropzone.uploading {
    opacity: 0.6;
    pointer-events: none;
  }
  .dropzone-icon {
    font-size: 2rem;
  }
  .dropzone-text {
    font-size: 0.85rem;
    font-weight: 600;
    color: #c0c0d0;
    text-align: center;
  }
  .dropzone-hint {
    font-size: 0.7rem;
    color: #606080;
  }

  /* Installed list */
  .installed-list {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .installed-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0.6rem;
    background: #0e0e1a;
    border: 1px solid #1a1a3a;
    border-radius: 6px;
    gap: 0.5rem;
  }

  .installed-info {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 0;
    flex: 1;
  }

  .installed-name {
    font-size: 0.8rem;
    font-weight: 600;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .installed-cat {
    font-size: 0.68rem;
    color: #b388ff;
  }

  .installed-size {
    font-size: 0.68rem;
    color: #606080;
  }

  .btn-delete {
    background: none;
    border: none;
    font-size: 0.9rem;
    cursor: pointer;
    padding: 0.2rem;
    flex-shrink: 0;
    opacity: 0.6;
    transition: opacity 0.15s;
  }
  .btn-delete:hover {
    opacity: 1;
  }

  /* Feedback */
  .feedback {
    text-align: center;
    font-size: 0.8rem;
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
</style>
