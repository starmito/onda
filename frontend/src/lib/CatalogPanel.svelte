<script lang="ts">
  import { getModelCatalog, type UVRModelEntry } from './api';

  let uvrModels = $state<UVRModelEntry[]>([]);
  let hfData = $state<any>(null);
  let expandedUVR = $state(false);
  let expandedHF = $state(false);
  let downloading = $state<Set<string>>(new Set());

  $effect(() => {
    getModelCatalog().then(m => uvrModels = m).catch(() => {});
    fetch('/api/models/catalog/hf')
      .then(r => r.json())
      .then(d => hfData = d)
      .catch(() => {});
  });

  function groupByCategory(models: UVRModelEntry[]): Record<string, UVRModelEntry[]> {
    const groups: Record<string, UVRModelEntry[]> = {};
    for (const m of models) {
      const cat = m.category || 'Other';
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(m);
    }
    return groups;
  }

  async function downloadUVR(model: UVRModelEntry) {
    if (!model.download_url) return;
    const key = `uvr:${model.name}`;
    downloading.add(key);
    downloading = downloading; // trigger reactivity
    try {
      // The download_url from catalog is the HuggingFace repo or GitHub URL
      // The backend download endpoint expects {source, repo}
      const repo = model.download_url?.includes('huggingface.co') 
        ? model.download_url.split('huggingface.co/')[1]?.replace('/resolve/main', '')?.split('/').slice(0,2).join('/')
        : model.download_url;
      await fetch('/api/models/download', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({source: 'huggingface', repo: repo || model.download_url})
      });
    } catch(e) {}
    downloading.delete(key);
    downloading = downloading;
  }

  async function downloadHF(filename: string, hfPath: string) {
    const key = `hf:${filename}`;
    downloading.add(key);
    downloading = downloading;
    try {
      await fetch('/api/models/download', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({source: 'huggingface', repo: `Politrees/UVR_resources`, filename: hfPath})
      });
    } catch(e) {}
    downloading.delete(key);
    downloading = downloading;
  }
</script>

<div class="catalog-panel">
  <!-- UVR Nativa -->
  <details bind:open={expandedUVR}>
    <summary>📦 UVR Nativa ({uvrModels.length} modelos)</summary>
    {#if expandedUVR && uvrModels.length > 0}
      {#each Object.entries(groupByCategory(uvrModels)) as [cat, models]}
        <div class="category-section">
          <h4>{cat} ({models.length})</h4>
          <div class="model-list">
            {#each models as model (model.name + (model.filename || ''))}
              <div class="model-row">
                <span class="model-name" title={model.display_name || model.name}>
                  {model.display_name || model.name}
                </span>
                <span class="model-size">{model.size_mb} MB</span>
                {#if model.download_url}
                  <button 
                    class="dl-btn" 
                    onclick={() => downloadUVR(model)}
                    disabled={downloading.has(`uvr:${model.name}`)}
                  >
                    {downloading.has(`uvr:${model.name}`) ? '⏳' : '⬇'}
                  </button>
                {/if}
              </div>
            {/each}
          </div>
        </div>
      {/each}
    {/if}
  </details>

  <!-- Repo HF -->
  <details bind:open={expandedHF}>
    <summary>🤗 Repo HF — Politrees/UVR_resources</summary>
    {#if expandedHF && hfData?.categories}
      {#each Object.entries(hfData.categories) as [cat, info]}
        {@const models = info.models || []}
        {@const filtered = models.filter(m => m.size_mb > 0)}
        <div class="category-section">
          <h4>{cat} ({filtered.length})</h4>
          <div class="model-list">
            {#each filtered as model (model.hf_path || model.filename)}
              <div class="model-row">
                <span class="model-name" title={model.name}>{model.name}</span>
                <span class="model-size">{model.size_mb} MB</span>
                <span class="model-filename">{model.filename}</span>
                <button 
                  class="dl-btn" 
                  onclick={() => downloadHF(model.filename, model.hf_path)}
                  disabled={downloading.has(`hf:${model.filename}`)}
                >
                  {downloading.has(`hf:${model.filename}`) ? '⏳' : '⬇'}
                </button>
              </div>
            {/each}
          </div>
        </div>
      {/each}
    {/if}
  </details>
</div>

<style>
  .catalog-panel {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  details {
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 8px;
    padding: 0.75rem 1rem;
    transition: border-color 0.2s;
  }
  details[open] {
    border-color: #00d4ff44;
  }
  summary {
    cursor: pointer;
    font-weight: 600;
    color: #e0e0e0;
    font-size: 1rem;
    padding: 0.25rem 0;
    user-select: none;
  }
  summary:hover {
    color: #00d4ff;
  }
  .category-section {
    margin-top: 0.5rem;
  }
  h4 {
    color: #a0a0c0;
    margin: 0.5rem 0 0.25rem;
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    border-bottom: 1px solid #2a2a3e;
    padding-bottom: 0.25rem;
  }
  .model-list {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }
  .model-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.3rem 0.5rem;
    border-radius: 4px;
    font-size: 0.8rem;
    transition: background 0.15s;
  }
  .model-row:hover {
    background: #0a0a14;
  }
  .model-name {
    flex: 1;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .model-size {
    color: #606080;
    flex-shrink: 0;
    font-size: 0.75rem;
    min-width: 55px;
    text-align: right;
  }
  .model-filename {
    color: #444;
    font-size: 0.65rem;
    flex-shrink: 0;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dl-btn {
    background: #2a2a4a;
    border: 1px solid #3a3a5a;
    color: #00d4ff;
    border-radius: 4px;
    cursor: pointer;
    padding: 0.15rem 0.4rem;
    font-size: 0.75rem;
    transition: all 0.15s;
    flex-shrink: 0;
  }
  .dl-btn:hover:not(:disabled) {
    background: #3a3a5a;
    border-color: #00d4ff;
  }
  .dl-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>