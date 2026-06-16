<script lang="ts">
  import {
    getModelCatalog,
    getHfCatalog,
    getLocalModels,
    downloadModel,
    getDownloadStatus,
    uploadModel,
    deleteModel,
    type UVRModelEntry,
    type HFModelEntry,
    type LocalModel,
    type DownloadProgress,
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

  // ---- Source filter state ----
  type SourceFilter = 'all' | 'uvr' | 'hf';
  let sourceFilter = $state<SourceFilter>('all');

  // ---- HF catalog state ----
  let hfCatalog = $state<HFModelEntry[]>([]);
  let hfCatalogLoading = $state(false);
  let hfCatalogError = $state(false);

  // ---- Downloading state ----
  let downloading = $state<Set<string>>(new Set());
  let downloadErrors = $state<Map<string, string>>(new Map());
  let downloadProgress = $state<Map<string, DownloadProgress>>(new Map());


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

  // Load HF catalog
  $effect(() => {
    hfCatalogLoading = true;
    getHfCatalog()
      .then(data => {
        const all: HFModelEntry[] = [];
        for (const [cat, info] of Object.entries(data.categories)) {
          for (const m of info.models) {
            if (m.size_mb > 0) {
              all.push({ ...m, category: cat });
            }
          }
        }
        hfCatalog = all;
        hfCatalogLoading = false;
      })
      .catch(() => {
        hfCatalogError = true;
        hfCatalogLoading = false;
      });
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

  // ---- Derived: combined catalog, filtered, grouped ----
  type SourceType = 'uvr' | 'hf';

  interface CombinedModel {
    name: string;
    display_name?: string;
    category: string;
    size_mb: number;
    description?: string;
    downloaded: boolean;
    source: SourceType;
    huggingface_repo?: string;
    filename?: string;
    hf_path?: string;
  }

  let combinedModels = $derived.by(() => {
    const uvrMapped: CombinedModel[] = (sourceFilter === 'hf' ? [] : catalog).map(m => ({
      name: m.name,
      display_name: m.display_name,
      category: m.category,
      size_mb: m.size_mb,
      description: m.description,
      downloaded: m.downloaded,
      source: 'uvr' as SourceType,
      huggingface_repo: m.huggingface_repo,
      filename: m.filename,
    }));

    const hfMapped: CombinedModel[] = (sourceFilter === 'uvr' ? [] : hfCatalog).map(m => ({
      name: m.name,
      category: m.category,
      size_mb: m.size_mb,
      downloaded: false,
      source: 'hf' as SourceType,
      hf_path: m.hf_path,
      filename: m.filename,
    }));

    return [...uvrMapped, ...hfMapped];
  });

  let filtered = $derived.by(() => {
    if (!search) return combinedModels;
    const q = search.toLowerCase();
    return combinedModels.filter(
      (m) =>
        m.name.toLowerCase().includes(q) ||
        m.display_name?.toLowerCase().includes(q) ||
        m.description?.toLowerCase().includes(q),
    );
  });

  let groupedCatalog = $derived.by(() => {
    const groups: Record<string, CombinedModel[]> = {};
    const seen = new Set<string>(); // track unique model identities
    
    // Pre-process: filter sub-components + rename Demucs v2/v3 without mutating state
    const entries = filtered.flatMap(m => {
      // Solo UVR models tienen Demucs sub-components
      if (m.source === 'uvr') {
        // Skip entries without filename (shouldn't happen for UVR models)
        if (!m.filename) return m;
        // Skip Demucs sub-components (UUID-named .th files)
        if (/^[0-9a-f]{8}-[0-9a-f]{8}$/i.test(m.name) && m.filename.endsWith('.th')) return [];
        const isDemucsV2 = (['demucs.th', 'demucs_extra.th', 'tasnet.th', 'tasnet_extra.th', 'light.th', 'light_extra.th'] as string[]).includes(m.filename);
        const isDemucsV3 = m.filename.match(/^(demucs|demucs_extra|tasnet|tasnet_extra)-[0-9a-f]{8}\.th$/);
        if (isDemucsV2 && m.display_name === m.name) {
          return { ...m, display_name: m.name + ' (v2)' };
        } else if (isDemucsV3 && m.display_name === m.name) {
          return { ...m, display_name: m.name + ' (v3)' };
        }
      }
      return m;
    });
    
    for (const m of entries) {
      // For models with same name but different files (.ckpt vs .yaml),
      // prefer the weights file over config file
      const key = `${m.category}::${m.name}`;
      const ext = m.filename?.split('.').pop()?.toLowerCase() || '';
      const isWeights = ['ckpt', 'pth', 'th', 'onnx', 'safetensors', 'pt'].includes(ext);
      const isConfig = ext === 'yaml';
      
      if (seen.has(key)) {
        // Already have this model — replace ONLY if current is config and new is weights
        if (isWeights) {
          // Replace the config entry with the weights entry
          const cat = m.category || 'Other';
          if (groups[cat]) {
            groups[cat] = groups[cat].filter(e => `${e.category}::${e.name}` !== key);
            groups[cat].push(m);
          }
        }
        // Otherwise skip (keep existing)
        continue;
      }
      
      seen.add(key);
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
    const sorted: { category: string; models: CombinedModel[] }[] = [];
    for (const cat of order) {
      if (groups[cat] && groups[cat].length > 0) {
        sorted.push({ category: cat, models: groups[cat] });
        delete groups[cat];
      }
    }
    for (const [cat, m] of Object.entries(groups)) {
      sorted.push({ category: cat, models: m });
    }

    // Second pass: dedup by display_name — keep weights, discard configs
    for (const group of sorted) {
      const byDisplayName = new Map<string, CombinedModel>();
      for (const m of group.models) {
        const dn = m.display_name || m.name;
        const existing = byDisplayName.get(dn);
        if (!existing) {
          byDisplayName.set(dn, m);
        } else if (m.size_mb > 0 && existing.size_mb === 0) {
          // Replace config (0 MB) with weights (>0 MB)
          byDisplayName.set(dn, m);
        }
        // If both have size >0, keep the first (older entry)
      }
      group.models = Array.from(byDisplayName.values());
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

  function getRepoKey(model: CombinedModel): string {
    if (model.source === 'uvr') return model.huggingface_repo || model.name;
    return 'Politrees/UVR_resources';
  }

  async function startDownload(model: CombinedModel) {
    const key = model.filename || model.name;
    const repoKey = getRepoKey(model);
    
    const set = new Set(downloading);
    set.add(key);
    downloading = set;
    
    const errMap = new Map(downloadErrors);
    errMap.delete(key);
    downloadErrors = errMap;

    // Initialize progress
    const progMap = new Map(downloadProgress);
    progMap.set(key, { status: 'downloading', repo: repoKey, progress: 0, downloaded_bytes: 0, total_bytes: 0 });
    downloadProgress = progMap;

    try {
      if (model.source === 'uvr') {
        await downloadModel(model.huggingface_repo!);
      } else {
        await downloadModel('Politrees/UVR_resources', model.hf_path);
        // If checkpoint, also download .yaml
        if (model.filename?.match(/\.(ckpt|pth)$/i)) {
          const baseName = model.filename!.slice(0, model.filename!.lastIndexOf('.'));
          const yamlEntry = hfCatalog.find(m => m.filename === baseName + '.yaml');
          if (yamlEntry) {
            await downloadModel('Politrees/UVR_resources', yamlEntry.hf_path);
          }
        }
      }

      // Start polling for download progress
      const pollInterval = setInterval(async () => {
        try {
          const status = await getDownloadStatus(repoKey);
          const pMap = new Map(downloadProgress);
          pMap.set(key, status);
          downloadProgress = pMap;

          if (status.status === 'done') {
            clearInterval(pollInterval);
            // Remove from downloading, refresh catalog
            const s = new Set(downloading);
            s.delete(key);
            downloading = s;
            await refreshCatalog();
          } else if (status.status === 'error') {
            clearInterval(pollInterval);
            const errs = new Map(downloadErrors);
            errs.set(key, status.error || 'Download failed');
            downloadErrors = errs;
            const s = new Set(downloading);
            s.delete(key);
            downloading = s;
            const pm = new Map(downloadProgress);
            pm.delete(key);
            downloadProgress = pm;
          }
        } catch (pollErr) {
          // If polling returns 404, download might be done already
          clearInterval(pollInterval);
          const s = new Set(downloading);
          s.delete(key);
          downloading = s;
          const pm = new Map(downloadProgress);
          pm.delete(key);
          downloadProgress = pm;
        }
      }, 2000);

    } catch (err: any) {
      const errors = new Map(downloadErrors);
      errors.set(key, err.message || 'Download failed');
      downloadErrors = errors;
      const s = new Set(downloading);
      s.delete(key);
      downloading = s;
      const pm = new Map(downloadProgress);
      pm.delete(key);
      downloadProgress = pm;
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

<div class="fullscreen">
  <div class="fullscreen-header">
    <button class="btn-close" onclick={onclose}>✕</button>
    <h2>📥 Gestor de Modelos</h2>
    <div></div>
  </div>

  <div class="fullscreen-body">
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

        <!-- Source filters -->
        <div class="source-filters">
          <button
            class="source-btn"
            class:active={sourceFilter === 'all'}
            onclick={() => (sourceFilter = 'all')}
          >Todas las fuentes</button>
          <button
            class="source-btn"
            class:active={sourceFilter === 'uvr'}
            onclick={() => (sourceFilter = 'uvr')}
          >UVR</button>
          <button
            class="source-btn"
            class:active={sourceFilter === 'hf'}
            onclick={() => (sourceFilter = 'hf')}
          >Hugging Face</button>
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
              {#each group.models as model}
                <div class="model-row">
                  <div class="model-info">
                    <span class="model-name">{model.display_name || model.name}</span>
                    {#if model.description}
                      <span class="model-desc">{model.description}</span>
                    {/if}
                    <span class="model-size">{formatSize(model.size_mb)}</span>
                  </div>
                  <div class="model-action">
                    <span class="source-badge" class:uvr={model.source === 'uvr'} class:hf={model.source === 'hf'}>
                      {model.source === 'uvr' ? 'UVR' : 'HF'}
                    </span>
                    {#if model.downloaded}
                      <span class="check-icon" title="Ya instalado">✅</span>
                    {:else if downloading.has(model.filename || model.name)}
                      {#if (downloadProgress.get(model.filename || model.name)?.progress ?? 0) > 0}
                        <div class="download-progress-wrap">
                          <div class="progress-bar">
                            <div class="progress-fill" style="width: {downloadProgress.get(model.filename || model.name)?.progress ?? 0}%"></div>
                          </div>
                          <span class="progress-text">{downloadProgress.get(model.filename || model.name)?.progress ?? 0}%</span>
                        </div>
                        {#if downloadProgress.get(model.filename || model.name)?.speed}
                          <span class="speed-text">{downloadProgress.get(model.filename || model.name)?.speed}</span>
                        {/if}
                      {:else}
                        <span class="spinner">⏳</span>
                      {/if}
                    {:else}
                      <button
                        class="btn-download"
                        onclick={() => startDownload(model)}
                        disabled={model.source === 'uvr' ? !model.huggingface_repo : !model.hf_path}
                      >
                        Descargar
                      </button>
                      {#if downloadErrors.has(model.filename || model.name)}
                        <span class="download-error" title={downloadErrors.get(model.filename || model.name)}>❌</span>
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
    padding: 1.25rem;
    max-width: 800px;
    margin: 0 auto;
    width: 100%;
    box-sizing: border-box;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  /* Tab bar */
  .tab-bar {
    display: flex;
    justify-content: center;
    gap: 0.5rem;
    margin-bottom: 1rem;
  }

  .tab-btn {
    flex: 1;
    padding: 0.6rem 0.5rem;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-muted);
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab-btn:hover {
    color: var(--text-secondary);
  }
  .tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  /* Search */
  .search-wrap {
    flex-shrink: 0;
  }

  .search-input {
    width: 100%;
    box-sizing: border-box;
    padding: 0.45rem 0.75rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 0.82rem;
    outline: none;
  }
  .search-input:focus {
    border-color: var(--accent);
  }
  .search-input::placeholder {
    color: var(--text-muted);
  }

  /* Empty state */
  .empty-state {
    text-align: center;
    color: var(--text-muted);
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
    color: var(--accent-light);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    padding-bottom: 0.2rem;
    border-bottom: 1px solid var(--border);
  }

  .model-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0.6rem;
    background: var(--bg-primary);
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
    color: var(--text-primary);
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
    color: var(--text-muted);
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
    background: linear-gradient(135deg, var(--accent), var(--accent-dark));
    border: none;
    border-radius: 5px;
    color: var(--text-primary);
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

  .download-progress-wrap {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 120px;
  }

  .progress-bar {
    flex: 1;
    height: 6px;
    background: var(--bg-surface, #1a1a3a);
    border-radius: 3px;
    overflow: hidden;
    min-width: 60px;
  }

  .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), var(--accent-dark));
    border-radius: 3px;
    transition: width 0.3s ease;
  }

  .progress-text {
    font-size: 0.7rem;
    font-weight: 700;
    color: var(--accent);
    min-width: 2.2em;
    text-align: right;
  }

  .speed-text {
    font-size: 0.6rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .download-error {
    font-size: 0.9rem;
    cursor: help;
  }

  /* Upload dropzone */
  .dropzone {
    width: 100%;
    box-sizing: border-box;
    border: 2px dashed var(--border);
    border-radius: 10px;
    padding: 2rem 1rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    background: var(--bg-primary);
  }
  .dropzone:hover {
    border-color: var(--accent);
    background: var(--bg-hover);
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
    color: var(--text-primary);
    text-align: center;
  }
  .dropzone-hint {
    font-size: 0.7rem;
    color: var(--text-muted);
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
    background: var(--bg-primary);
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
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .installed-cat {
    font-size: 0.68rem;
    color: var(--accent-light);
  }

  .installed-size {
    font-size: 0.68rem;
    color: var(--text-muted);
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

  .source-filters {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 1rem;
    justify-content: center;
  }

  .source-btn {
    padding: 0.4rem 1rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 20px;
    color: var(--text-secondary);
    font-size: 0.8rem;
    cursor: pointer;
    transition: all 0.15s;
  }
  .source-btn:hover {
    border-color: var(--text-muted);
    color: var(--text-primary);
  }
  .source-btn.active {
    background: var(--accent-bg);
    border-color: var(--accent);
    color: var(--accent);
  }

  .source-badge {
    font-size: 0.65rem;
    padding: 0.15rem 0.4rem;
    border-radius: 4px;
    font-weight: 600;
    flex-shrink: 0;
  }
  .source-badge.uvr {
    background: #1b3a2a;
    color: #81c784;
  }
  .source-badge.hf {
    background: #1b2a3a;
    color: #64b5f6;
  }
</style>
