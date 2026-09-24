<script lang="ts">
  import { onDestroy } from 'svelte';
  import { listStems, mergeStems, getExportProfiles, downloadUrl, deleteSong } from './api';
  import type { StemsResponse, AudioExportProfiles } from './api';
  import { IconDownload, IconRefresh, IconTrash } from './icons';
  import { removeGroup, filterExportFileNames } from './exportHelpers';
  import { expandExportName, groupBaseSong, groupPitch, groupDisplayName } from './exportName';

  // ── State ──
  let stemsResponse = $state<StemsResponse | null>(null);
  let loading = $state(true);
  let exporting = $state<Record<string, boolean>>({});
  let selectedFormats = $state<Record<string, string>>({});
  let suffixes = $state<Record<string, string>>({});
  let availableProfiles = $state<AudioExportProfiles | null>(null);
  let toast = $state<{ message: string; type: 'success' | 'error' } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  const DEFAULT_FORMATS = ['WAV', 'FLAC', 'MP3'];

  $effect(() => {
    loadStems();
    loadProfiles();
  });

  async function loadProfiles() {
    try {
      availableProfiles = await getExportProfiles();
    } catch (err) {
      console.error('Failed to load export profiles:', err);
    }
  }

  function defaultFormat(): string {
    return (availableProfiles?.defaultFormat || 'flac').toUpperCase();
  }

  async function loadStems() {
    loading = true;
    try {
      stemsResponse = filterExportFileNames(await listStems());
      // Initialize default format per song from the configured export profile.
      const defaults: Record<string, string> = {};
      if (stemsResponse?.output) {
        for (const song of Object.keys(stemsResponse.output)) {
          defaults[song] = defaultFormat();
        }
      }
      selectedFormats = { ...selectedFormats, ...defaults };
    } catch (err: any) {
      showToast(`Error al cargar stems: ${err.message || 'desconocido'}`, 'error');
    } finally {
      loading = false;
    }
  }

  function formatOptions(): string[] {
    if (availableProfiles?.formats) {
      const profileKeys = Object.keys(availableProfiles.formats);
      // Prefer intersection with known formats; fall back to defaults
      const known = new Set(DEFAULT_FORMATS.map(f => f.toLowerCase()));
      const intersection = profileKeys.filter(k => known.has(k.toLowerCase()));
      if (intersection.length > 0) {
        return intersection.map(k => k.toUpperCase());
      }
    }
    return DEFAULT_FORMATS;
  }

  async function handleMerge(song: string) {
    const stems = stemsResponse?.output[song];
    if (!stems || stems.length === 0) {
      showToast('No hay stems para unir', 'error');
      return;
    }
    const format = selectedFormats[song] || defaultFormat();
    const suffix = (suffixes[song] || '').trim();
    const baseSong = groupBaseSong(song);
    const pitch = groupPitch(song);
    const tpl = availableProfiles?.nameTemplate || '{song} ({pitches}) ({suffix})';
    const outputName = expandExportName(tpl, baseSong, pitch, suffix, format.toLowerCase());
    exporting = { ...exporting, [song]: true };
    try {
      const resp = await mergeStems(song, stems, format.toLowerCase(), outputName);
      const url = resp.url || downloadUrl(song, resp.file);
      const a = document.createElement('a');
      a.href = url;
      a.download = resp.file;
      a.click();
      showToast(`Exportado: ${resp.file} (${format}, ${formatBytes(resp.size)})`, 'success');
    } catch (err: any) {
      showToast(`Error al unir stems: ${err.message || 'desconocido'}`, 'error');
    } finally {
      exporting = { ...exporting, [song]: false };
    }
  }

  async function handleDeleteGroup(song: string) {
    if (!stemsResponse?.output[song]) return;
    if (!confirm(`¿Eliminar el grupo "${groupDisplayName(song)}"?\n\nSe borrará la carpeta de este grupo y todo su contenido. Esta acción no se puede deshacer.`)) {
      return;
    }
    try {
      await deleteSong(song);
      stemsResponse = removeGroup(stemsResponse, song);
      showToast('Grupo eliminado', 'success');
    } catch (err: any) {
      showToast(`Error al eliminar el grupo: ${err.message || 'desconocido'}`, 'error');
    }
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
  }

  function previewFileName(group: string): string {
    const format = selectedFormats[group] || defaultFormat();
    const baseSong = groupBaseSong(group);
    const pitch = groupPitch(group);
    const suffix = (suffixes[group] || '').trim();
    const tpl = availableProfiles?.nameTemplate || '{song} ({pitches}) ({suffix})';
    const base = expandExportName(tpl, baseSong, pitch, suffix, format.toLowerCase());
    return `${base}.${format.toLowerCase()}`;
  }

  function showToast(message: string, type: 'success' | 'error') {
    toast = { message, type };
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = null; }, 3000);
  }

  onDestroy(() => {
    if (toastTimer) clearTimeout(toastTimer);
  });
</script>

<div class="export-page">
  <section class="export-section">
    <div class="section-header">
      <div>
        <h3 class="section-title">Unir y exportar</h3>
        <p class="section-desc">
          Selecciona un grupo de stems, elige el formato de salida y genera una única pista combinada (mixdown).
        </p>
      </div>
      <button class="refresh-btn" onclick={loadStems} disabled={loading} title="Recargar grupos">
        <span class="icon-spin" class:spinning={loading}>{@html IconRefresh}</span>
        Recargar
      </button>
    </div>

    {#if loading}
      <div class="empty-state">Cargando grupos de stems…</div>
    {:else if !stemsResponse || !Object.keys(stemsResponse.output || {}).length}
      <div class="empty-state">
        No hay grupos de stems disponibles.<br />
        Separa una canción primero para ver sus pistas aquí.
      </div>
    {:else}
      <div class="export-groups-list">
        {#each Object.entries(stemsResponse.output) as [song, stems] (song)}
          <div class="export-group-card">
            <div class="export-group-header">
              <span class="export-song-name">📁 {groupDisplayName(song)}</span>
              <span class="export-stem-count">{stems.length} pistas</span>
            </div>

            <div class="export-stems-list">
              {#each stems as stem}
                <span class="export-stem-tag">{stem}</span>
              {/each}
            </div>

            <div class="export-suffix-row">
              <label class="suffix-label" for={`suffix-${song}`}>Sufijo:</label>
              <input
                id={`suffix-${song}`}
                class="suffix-input"
                type="text"
                bind:value={suffixes[song]}
                placeholder="sufijo entre paréntesis (opcional)"
                disabled={exporting[song]}
              />
            </div>
            <p class="export-name-preview">
              {previewFileName(song)}
            </p>

            <div class="export-actions-row">
              <label class="format-label" for={`format-${song}`}>Formato:</label>
              <select
                id={`format-${song}`}
                class="format-select"
                bind:value={selectedFormats[song]}
                disabled={exporting[song]}
              >
                {#each formatOptions() as fmt}
                  <option value={fmt}>{fmt}</option>
                {/each}
              </select>

              <div class="export-merge-actions">
                <button
                  class="delete-group-btn"
                  onclick={() => handleDeleteGroup(song)}
                  disabled={exporting[song]}
                  title="Eliminar este grupo"
                >
                  <span class="btn-icon">{@html IconTrash}</span>
                  Eliminar grupo
                </button>

                <button
                  class="merge-export-btn"
                  onclick={() => handleMerge(song)}
                  disabled={exporting[song]}
                >
                  {#if exporting[song]}
                    <span class="spinner"></span>
                    Exportando…
                  {:else}
                    <span class="btn-icon">{@html IconDownload}</span>
                    Unir y exportar
                  {/if}
                </button>
              </div>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  {#if toast}
    <div class="toast {toast.type}">{toast.message}</div>
  {/if}
</div>

<style>
  .export-page {
    width: 100%;
    box-sizing: border-box;
    padding: 1rem;
  }

  .export-section {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1.25rem;
  }

  .section-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1rem;
    flex-wrap: wrap;
  }

  .section-title {
    margin: 0 0 0.35rem 0;
    font-size: 1.05rem;
    font-weight: 700;
    color: var(--text-primary);
  }

  .section-desc {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-secondary);
    max-width: 640px;
    line-height: 1.4;
  }

  .refresh-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.4rem 0.7rem;
    border-radius: 6px;
    border: 1px solid var(--border-light);
    background: var(--bg-hover);
    color: var(--text-secondary);
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
  }
  .refresh-btn:hover:not(:disabled) { background: #333355; border-color: var(--text-muted); }
  .refresh-btn:disabled { opacity: 0.6; cursor: not-allowed; }

  .icon-spin :global(svg) {
    width: 14px;
    height: 14px;
    display: block;
  }
  .icon-spin.spinning :global(svg) {
    animation: spin 1s linear infinite;
  }

  .empty-state {
    padding: 2rem 1rem;
    text-align: center;
    color: var(--text-muted);
    font-size: 0.9rem;
    line-height: 1.5;
  }

  .export-groups-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .export-group-card {
    background: var(--bg-primary);
    border: 1px solid var(--border-light);
    border-radius: 10px;
    padding: 0.9rem 1rem;
    animation: fadeIn 0.3s ease;
  }

  .export-group-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    margin-bottom: 0.5rem;
    flex-wrap: wrap;
  }

  .export-song-name {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 0.95rem;
    font-weight: 700;
    color: var(--accent-light);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .export-stem-count {
    font-size: 0.75rem;
    color: var(--text-muted);
    font-weight: 600;
  }

  .export-stems-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
    margin-bottom: 0.75rem;
  }

  .export-stem-tag {
    font-size: 0.7rem;
    color: var(--text-secondary);
    background: var(--bg-hover);
    border: 1px solid var(--border-light);
    border-radius: 4px;
    padding: 0.15rem 0.45rem;
  }

  .export-suffix-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 0.6rem;
  }

  .suffix-label {
    font-size: 0.8rem;
    color: var(--text-secondary);
    font-weight: 600;
  }

  .suffix-input {
    flex: 1;
    min-width: 160px;
    padding: 0.35rem 0.6rem;
    border-radius: 6px;
    border: 1px solid var(--border-light);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.8rem;
    outline: none;
  }
  .suffix-input:focus { border-color: var(--accent); }
  .suffix-input:disabled { opacity: 0.6; cursor: not-allowed; }

  .export-name-preview {
    margin: -0.25rem 0 0.5rem 0;
    font-size: 0.75rem;
    color: var(--text-muted);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    word-break: break-word;
  }

  .export-actions-row {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
    border-top: 1px solid var(--border);
    padding-top: 0.75rem;
  }

  .export-merge-actions {
    display: inline-flex;
    align-items: center;
    gap: 1.75rem;
    flex-wrap: wrap;
  }

  .format-label {
    font-size: 0.8rem;
    color: var(--text-secondary);
    font-weight: 600;
  }

  .format-select {
    padding: 0.35rem 0.6rem;
    border-radius: 6px;
    border: 1px solid var(--border-light);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
    outline: none;
    min-width: 80px;
  }
  .format-select:focus { border-color: var(--accent); }
  .format-select:disabled { opacity: 0.6; cursor: not-allowed; }

  .merge-export-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.45rem 0.9rem;
    border-radius: 6px;
    border: 1px solid var(--accent);
    background: var(--accent-bg);
    color: var(--accent-light);
    font-size: 0.8rem;
    font-weight: 700;
    cursor: pointer;
    transition: background 0.2s, transform 0.1s;
  }
  .merge-export-btn:hover:not(:disabled) { background: var(--accent-subtle); }
  .merge-export-btn:active:not(:disabled) { transform: scale(0.97); }
  .merge-export-btn:disabled { opacity: 0.65; cursor: not-allowed; }

  .delete-group-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.45rem 0.9rem;
    border-radius: 6px;
    border: 1px solid var(--border-light);
    background: var(--bg-hover);
    color: var(--text-secondary);
    font-size: 0.8rem;
    font-weight: 700;
    cursor: pointer;
    transition: background 0.2s, color 0.2s, border-color 0.2s, transform 0.1s;
  }
  .delete-group-btn:hover:not(:disabled) {
    background: #3a1a1a;
    color: #f44336;
    border-color: #f44336;
  }
  .delete-group-btn:active:not(:disabled) { transform: scale(0.97); }
  .delete-group-btn:disabled { opacity: 0.65; cursor: not-allowed; }

  .btn-icon :global(svg) {
    width: 14px;
    height: 14px;
    display: block;
  }

  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid var(--accent-light);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .toast {
    position: fixed;
    bottom: 60px;
    left: 50%;
    transform: translateX(-50%);
    padding: 12px 24px;
    border-radius: 8px;
    color: white;
    font-weight: 600;
    z-index: 1000;
    animation: toastIn 0.3s ease, toastOut 0.3s ease 2.7s forwards;
  }
  .toast.success { background: #4caf50; }
  .toast.error { background: #f44336; }

  @keyframes toastIn { from { opacity: 0; transform: translateX(-50%) translateY(20px); } }
  @keyframes toastOut { to { opacity: 0; transform: translateX(-50%) translateY(-20px); } }
  @keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

  @media (max-width: 600px) {
    .export-page { padding: 0.5rem; }
    .export-section { padding: 0.75rem; }
    .export-merge-actions { width: 100%; flex-direction: column; align-items: stretch; gap: 0.75rem; }
    .merge-export-btn { width: 100%; justify-content: center; }
    .delete-group-btn { width: 100%; justify-content: center; }
    .export-actions-row { flex-direction: column; align-items: stretch; gap: 0.75rem; }
    .format-select { width: 100%; }
  }
</style>
