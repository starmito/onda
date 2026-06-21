<script lang="ts">
  import DAWPage from './DAWPage.svelte';
  import BasicEffectsPanel from './BasicEffectsPanel.svelte';
  import BasicEQPanel from './BasicEQPanel.svelte';
  import MIDIPage from './MIDIPage.svelte';
  import SpectrogramPage from './SpectrogramPage.svelte';
  import { IconSliders, IconPiano, IconSpectrogram } from './icons';

  let viewMode = $state<'basic' | 'medium' | 'full'>('basic');
  let activeFile = $state<string | null>(null);
  let activeTab = $state<'effects' | 'midi' | 'spectrogram'>('effects');

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
      <div class="medium-panel">
        <div class="tab-bar">
          <button
            class="tab-button"
            class:active={activeTab === 'effects'}
            onclick={() => (activeTab = 'effects')}
            type="button"
          >
            <span class="tab-icon">{@html IconSliders}</span>
            <span>Efectos + EQ</span>
          </button>
          <button
            class="tab-button"
            class:active={activeTab === 'midi'}
            onclick={() => (activeTab = 'midi')}
            type="button"
          >
            <span class="tab-icon">{@html IconPiano}</span>
            <span>Piano Roll</span>
          </button>
          <button
            class="tab-button"
            class:active={activeTab === 'spectrogram'}
            onclick={() => (activeTab = 'spectrogram')}
            type="button"
          >
            <span class="tab-icon">{@html IconSpectrogram}</span>
            <span>Espectrograma</span>
          </button>
        </div>

        <div class="tab-content">
          {#if activeTab === 'effects'}
            <div class="basic-grid">
              <BasicEffectsPanel activeFile={activeFile} />
              <BasicEQPanel activeFile={activeFile} />
            </div>
          {:else if activeTab === 'midi'}
            <MIDIPage />
          {:else if activeTab === 'spectrogram'}
            <SpectrogramPage />
          {/if}
        </div>
      </div>
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

  .medium-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 0.75rem;
  }

  .tab-bar {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.4rem;
    background: #1a1a2e;
    border: 1px solid var(--border);
    border-radius: 10px;
    flex-shrink: 0;
  }

  .tab-button {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.9rem;
    border: none;
    border-radius: 8px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
  }

  .tab-button:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-primary);
  }

  .tab-button.active {
    background: rgba(255, 255, 255, 0.1);
    color: var(--accent, #7c5cff);
    box-shadow: inset 0 -2px 0 0 var(--accent, #7c5cff);
  }

  .tab-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.1rem;
    height: 1.1rem;
  }

  .tab-icon :global(svg) {
    width: 100%;
    height: 100%;
  }

  .tab-content {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
</style>
