<script lang="ts">
  import { onMount } from 'svelte';
  import { API_BASE } from './api';
  import { IconRefresh, IconTrash, IconFolder } from './icons';

  interface FolderUsage {
    files: number;
    bytes: number;
  }

  interface UsageResponse {
    folders: Record<string, FolderUsage>;
    free_bytes: number;
  }

  interface CleanResponse {
    action: string;
    files: number;
    bytes: number;
  }

  let usage = $state<UsageResponse | null>(null);
  let loading = $state(false);
  let cleaning = $state(false);
  let message = $state<string | null>(null);
  let error = $state<string | null>(null);

  const folderOrder = ['input', 'input_rubberband', 'daw-data', 'output', 'models', 'logs'];
  const folderLabels: Record<string, string> = {
    input: 'Cola / subidas',
    input_rubberband: 'Subidas de tono',
    'daw-data': 'Proyectos DAW',
    output: 'Resultados',
    models: 'Modelos IA',
    logs: 'Registros',
  };

  async function loadUsage() {
    loading = true;
    error = null;
    try {
      const res = await fetch(`${API_BASE}/api/storage/usage`);
      if (!res.ok) throw new Error(`Error ${res.status}`);
      usage = await res.json() as UsageResponse;
    } catch (e: any) {
      error = e.message || 'No se pudo cargar el uso de almacenamiento';
    } finally {
      loading = false;
    }
  }

  async function clean(action: string, label: string) {
    const confirmed = window.confirm(`¿Borrar ${label}? Esta acción no se puede deshacer.`);
    if (!confirmed) return;

    cleaning = true;
    error = null;
    message = null;
    try {
      const res = await fetch(`${API_BASE}/api/storage/clean`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({} as { error?: string }));
        throw new Error(data.error || `Error ${res.status}`);
      }
      const result = await res.json() as CleanResponse;
      message = `Limpieza completada: se liberaron ${formatBytes(result.bytes)} en ${result.files} archivo(s).`;
      await loadUsage();
    } catch (e: any) {
      error = e.message || 'Error al limpiar';
    } finally {
      cleaning = false;
    }
  }

  function formatBytes(n: number): string {
    if (n === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let size = n;
    while (size >= 1024 && i < units.length - 1) {
      size /= 1024;
      i++;
    }
    return `${size.toFixed(2)} ${units[i]}`;
  }

  onMount(() => {
    loadUsage();
  });
</script>

<div class="storage-panel">
  <div class="storage-header">
    <h2>{@html IconFolder} Almacenamiento</h2>
    <button class="btn-refresh" onclick={loadUsage} disabled={loading} title="Refrescar">
      {@html IconRefresh}
    </button>
  </div>

  {#if error}
    <p class="storage-error">{error}</p>
  {/if}
  {#if message}
    <p class="storage-success">{message}</p>
  {/if}

  {#if usage}
    <table class="storage-table">
      <thead>
        <tr>
          <th>Carpeta</th>
          <th>Archivos</th>
          <th>Tamaño</th>
        </tr>
      </thead>
      <tbody>
        {#each folderOrder as key}
          {@const u = usage.folders[key] ?? { files: 0, bytes: 0 }}
          <tr>
            <td>{folderLabels[key] ?? key}</td>
            <td>{u.files}</td>
            <td>{formatBytes(u.bytes)}</td>
          </tr>
        {/each}
      </tbody>
      <tfoot>
        <tr>
          <td><strong>Espacio libre en disco</strong></td>
          <td></td>
          <td><strong>{formatBytes(usage.free_bytes)}</strong></td>
        </tr>
      </tfoot>
    </table>
  {:else if loading}
    <p class="storage-empty">Cargando uso de almacenamiento...</p>
  {/if}

  <div class="storage-actions">
    <h3>Limpieza manual</h3>
    <div class="storage-buttons">
      <button onclick={() => clean('tmp', 'los temporales del DAW')} disabled={cleaning}>
        {@html IconTrash} Borrar temporales
      </button>
      <button onclick={() => clean('orphan-edits', 'las ediciones huérfanas')} disabled={cleaning}>
        {@html IconTrash} Borrar ediciones huérfanas
      </button>
      <button onclick={() => clean('all-edits', 'TODAS las ediciones')} disabled={cleaning}>
        {@html IconTrash} Borrar todas las ediciones
      </button>
    </div>
    <p class="storage-hint">Estas acciones siempre conservan los originales y los imports.</p>
  </div>
</div>

<style>
  .storage-panel {
    padding: 20px;
    color: var(--text-primary);
  }

  .storage-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
  }

  .storage-header h2 {
    margin: 0;
    font-size: 18px;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .storage-error {
    color: #ff6b6b;
    margin-bottom: 12px;
  }

  .storage-success {
    color: #51cf66;
    margin-bottom: 12px;
  }

  .storage-table {
    width: 100%;
    border-collapse: collapse;
    margin-bottom: 24px;
  }

  .storage-table th,
  .storage-table td {
    text-align: left;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }

  .storage-table th {
    color: var(--text-secondary);
    font-weight: 600;
    font-size: 13px;
  }

  .storage-table td {
    font-size: 14px;
  }

  .storage-table tfoot td {
    border-top: 2px solid var(--border);
    border-bottom: none;
  }

  .storage-actions h3 {
    margin: 0 0 12px;
    font-size: 15px;
  }

  .storage-buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
  }

  .storage-buttons button {
    background: var(--accent-bg);
    border: 1px solid var(--accent);
    color: var(--accent-light);
    padding: 8px 14px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    display: flex;
    align-items: center;
    gap: 6px;
    transition: all 0.15s ease;
  }

  .storage-buttons button:hover:not(:disabled) {
    background: var(--accent);
    color: #fff;
  }

  .storage-buttons button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .storage-hint {
    color: var(--text-secondary);
    font-size: 12px;
    margin-top: 10px;
  }

  .storage-empty {
    color: var(--text-secondary);
    padding: 20px 0;
  }

  .btn-refresh {
    background: transparent;
    border: 1px solid var(--border-light);
    color: var(--text-secondary);
    padding: 4px 10px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.15s ease;
  }

  .btn-refresh:hover:not(:disabled) {
    background: #333;
    color: #fff;
  }

  .btn-refresh:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
