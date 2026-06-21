<script lang="ts">
  import DAWPage from './DAWPage.svelte';
  import BasicEffectsPanel from './BasicEffectsPanel.svelte';
  import BasicEQPanel from './BasicEQPanel.svelte';

  let viewMode = $state<'basic' | 'medium' | 'full'>('basic');
  let activeFile = $state<string | null>(null);

  function handleActiveTrackChange(fileName: string | null) {
    activeFile = fileName;
  }
</script>

<div class="daw-workspace">
  <div class="workspace-toolbar">
    <label class="mode-select">
      <span>Modo</span>
      <select bind:value={viewMode}>
        <option value="basic">Básico</option>
        <option value="medium">Medio</option>
        <option value="full">Completo</option>
      </select>
    </label>

    {#if activeFile}
      <span class="active-file">{activeFile}</span>
    {:else}
      <span class="active-file empty">Sin pista activa</span>
    {/if}
  </div>

  <div class="audio-panel">
    <DAWPage onActiveTrackChange={handleActiveTrackChange} />
  </div>

  <div class="mode-panel">
    {#if viewMode === 'basic'}
      <div class="basic-grid">
        <BasicEffectsPanel activeFile={activeFile} />
        <BasicEQPanel activeFile={activeFile} />
      </div>
    {:else if viewMode === 'medium'}
      <div class="placeholder">Medio - próximamente</div>
    {:else}
      <div class="placeholder">Completo - próximamente</div>
    {/if}
  </div>
</div>

<style>
  .daw-workspace {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .workspace-toolbar {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.6rem 0.8rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    flex-shrink: 0;
  }

  .mode-select {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 600;
  }

  .mode-select select {
    padding: 0.4rem 0.6rem;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--text-primary);
    font-size: 0.85rem;
  }

  .active-file {
    font-size: 0.8rem;
    color: var(--text-primary);
    margin-left: auto;
    max-width: 400px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .active-file.empty {
    color: var(--text-secondary);
  }

  .audio-panel {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .mode-panel {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .basic-grid {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: 1rem;
    height: 100%;
    min-height: 0;
  }

  @media (max-width: 1100px) {
    .basic-grid {
      grid-template-columns: 1fr;
      grid-template-rows: 1fr 1fr;
    }
  }

  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 120px;
    background: var(--bg-surface);
    border: 1px dashed var(--border);
    border-radius: 12px;
    color: var(--text-secondary);
    font-size: 1rem;
    font-weight: 600;
  }
</style>
