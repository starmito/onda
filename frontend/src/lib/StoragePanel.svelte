<script lang="ts">
  import { onMount } from 'svelte';
  import { API_BASE, getStorageConfig, setStorageConfig, setExportDir, setConfigDir, type StorageConfig } from './api';
  import { IconRefresh, IconTrash, IconFolder } from './icons';

  interface FolderUsage {
    files: number;
    bytes: number;
  }

  interface ModelsUsage {
    entries: number;
    bytes: number;
  }

  interface CacheUsage {
    files: number;
    bytes: number;
  }

  interface UsageResponse {
    folders: Record<string, FolderUsage>;
    free_bytes: number;
    models?: ModelsUsage;
    cache?: CacheUsage;
  }

  interface CleanResponse {
    action: string;
    files: number;
    bytes: number;
  }

  let usage = $state<UsageResponse | null>(null);
  let config = $state<StorageConfig | null>(null);
  let configLoading = $state(false);
  let loading = $state(false);
  let cleaning = $state(false);
  let message = $state<string | null>(null);
  let error = $state<string | null>(null);
  let rootInput = $state('');
  let rootError = $state<string | null>(null);
  let rootSuccess = $state<string | null>(null);
  let savingRoot = $state(false);
  let exportInput = $state('');
  let exportError = $state<string | null>(null);
  let exportSuccess = $state<string | null>(null);
  let savingExport = $state(false);
  let configInput = $state('');
  let configError = $state<string | null>(null);
  let configSuccess = $state<string | null>(null);
  let savingConfig = $state(false);

  const folderOrder = ['input', 'input_rubberband', 'daw-data', 'output', 'models', 'cache', 'logs'];
  const folderLabels: Record<string, string> = {
    input: 'Cola / subidas',
    input_rubberband: 'Subidas de tono',
    'daw-data': 'Proyectos DAW',
    output: 'Resultados',
    models: 'Modelos IA',
    cache: 'Caché de modelos',
    logs: 'Registros',
  };

  const configFolderOrder = ['input', 'output', 'daw-data', 'input_rubberband', 'config', 'logs', 'models'];
  const configFolderLabels: Record<string, string> = {
    input: 'Entradas / cola',
    output: 'Resultados',
    'daw-data': 'Proyectos DAW',
    input_rubberband: 'Subidas de tono',
    config: 'Configuración',
    logs: 'Registros',
    models: 'Modelos IA',
  };

  const sourceLabels: Record<string, string> = {
    env: 'definido por el contenedor',
    settings: 'elegido por ti',
    default: 'por defecto',
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

  async function loadConfig() {
    configLoading = true;
    rootError = null;
    exportError = null;
    configError = null;
    try {
      config = await getStorageConfig();
      rootInput = config.current_root;
      exportInput = config.export_dir;
      configInput = config.config_dir;
    } catch (e: any) {
      rootError = e.message || 'No se pudo cargar la configuración del directorio de trabajo';
    } finally {
      configLoading = false;
    }
  }

  async function saveRoot() {
    if (!config) return;
    savingRoot = true;
    rootError = null;
    rootSuccess = null;
    try {
      const updated = await setStorageConfig(rootInput);
      config = updated;
      rootInput = updated.current_root;
      rootSuccess = `A partir de ahora se trabaja en ${updated.current_root}. Los modelos, logs y nuevos procesos usarán esta ruta.`;
      await loadConfig();
      await loadUsage();
    } catch (e: any) {
      rootError = e.message || 'No se pudo guardar el directorio de trabajo';
      // Do not mutate config or rootInput on error so the UI stays unchanged.
    } finally {
      savingRoot = false;
    }
  }

  function chooseCandidate(path: string) {
    rootInput = path;
  }

  async function saveExport() {
    if (!config) return;
    savingExport = true;
    exportError = null;
    exportSuccess = null;
    try {
      const updated = await setExportDir(exportInput);
      config = updated;
      exportInput = updated.export_dir;
      exportSuccess = `Carpeta de exportaciones actualizada a ${updated.export_dir || 'la ubicación por defecto'}.`;
      await loadConfig();
    } catch (e: any) {
      exportError = e.message || 'No se pudo guardar la carpeta de exportaciones';
      // Do not mutate config or exportInput on error so the UI stays unchanged.
    } finally {
      savingExport = false;
    }
  }

  function chooseExportCandidate(path: string) {
    exportInput = path;
  }

  async function saveConfig() {
    if (!config) return;
    savingConfig = true;
    configError = null;
    configSuccess = null;
    try {
      const updated = await setConfigDir(configInput);
      config = updated;
      configInput = updated.config_dir;
      configSuccess = `Carpeta de configuración actualizada a ${updated.config_dir || 'la ubicación por defecto'}. Los ajustes, presets y perfiles se leerán y escribirán ahí.`;
      await loadConfig();
    } catch (e: any) {
      configError = e.message || 'No se pudo guardar la carpeta de configuración';
      // Do not mutate config or configInput on error so the UI stays unchanged.
    } finally {
      savingConfig = false;
    }
  }

  function chooseConfigCandidate(path: string) {
    configInput = path;
  }

  function supportsNativeFolderPicker(): boolean {
    if (typeof window === 'undefined') return false;
    return !!(window as any).__TAURI__;
  }

  async function pickExportFolder() {
    const tauri = (window as any).__TAURI__;
    if (!tauri?.core?.invoke) return;
    exportError = null;
    try {
      const path: string | undefined = await tauri.core.invoke('select_folder');
      if (path) exportInput = path;
    } catch (e: any) {
      exportError = e.message || 'No se pudo elegir la carpeta';
    }
  }

  async function pickConfigFolder() {
    const tauri = (window as any).__TAURI__;
    if (!tauri?.core?.invoke) return;
    configError = null;
    try {
      const path: string | undefined = await tauri.core.invoke('select_folder');
      if (path) configInput = path;
    } catch (e: any) {
      configError = e.message || 'No se pudo elegir la carpeta';
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
      await loadConfig();
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
    loadConfig();
  });
</script>

<div class="storage-panel">
  <div class="storage-header">
    <h2>{@html IconFolder} Almacenamiento</h2>
    <button class="btn-refresh" onclick={() => { loadUsage(); loadConfig(); }} disabled={loading || configLoading} title="Refrescar">
      {@html IconRefresh}
    </button>
  </div>

  {#if error}
    <p class="storage-error">{error}</p>
  {/if}
  {#if message}
    <p class="storage-success">{message}</p>
  {/if}

  <section class="storage-section">
    <h3>Directorio de trabajo</h3>
    {#if rootError}
      <p class="storage-error">{rootError}</p>
    {/if}
    {#if rootSuccess}
      <p class="storage-success">{rootSuccess}</p>
    {/if}

    {#if config}
      <div class="root-summary">
        <div class="root-row">
          <span class="root-label">Raíz actual</span>
          <code class="root-path">{config.current_root}</code>
        </div>
        <div class="root-row">
          <span class="root-label">Origen</span>
          <span>{sourceLabels[config.source] ?? config.source}</span>
        </div>
        <div class="root-row">
          <span class="root-label">Estado</span>
          <span class="root-status">
            {#if config.exists && config.writable}
              <span class="status-ok">✅ Existe y se puede escribir</span>
            {:else if config.exists}
              <span class="status-warn">⚠️ Existe pero no se puede escribir</span>
            {:else}
              <span class="status-warn">⚠️ No existe o no es accesible</span>
            {/if}
          </span>
        </div>
        {#if config.note}
          <div class="root-row">
            <span class="root-label">Nota</span>
            <span class="root-note">{config.note}</span>
          </div>
        {/if}
      </div>

      <table class="storage-table config-table">
        <thead>
          <tr>
            <th>Carpeta</th>
            <th>Archivos</th>
            <th>Tamaño</th>
          </tr>
        </thead>
        <tbody>
          {#each configFolderOrder as key}
            {@const f = config.folders[key] ?? { exists: false, files: 0, bytes: 0 }}
            <tr>
              <td>
                {configFolderLabels[key] ?? key}
                {#if !f.exists}<span class="folder-missing">(no creada)</span>{/if}
              </td>
              <td>{f.files}</td>
              <td>{formatBytes(f.bytes)}</td>
            </tr>
          {/each}
        </tbody>
      </table>

      <div class="root-editor">
        <label for="root-path" class="root-label">Nueva ruta de trabajo</label>
        <div class="root-input-row">
          <input
            id="root-path"
            type="text"
            class="root-input"
            bind:value={rootInput}
            disabled={savingRoot}
            placeholder="/ruta/absoluta/al/directorio"
          />
          <button class="btn-primary" onclick={saveRoot} disabled={savingRoot || rootInput === config.current_root}>
            Guardar
          </button>
        </div>

        <!--
          ENGANCHE FASE 11 (app empaquetada):
          En la versión empaquetada este botón abrirá el explorador nativo del SO
          para elegir libremente cualquier carpeta. En el navegador no tenemos
          acceso al sistema de archivos, así que solo mostramos las rutas candidatas
          que expone el backend. En modo contenedor la elección está limitada al
          volumen montado (/app).
        -->
        <div class="candidate-picker">
          <span class="root-label">Elegir carpeta…</span>
          <select onchange={(e) => chooseCandidate(e.currentTarget.value)} disabled={savingRoot}>
            <option value="">-- selecciona una carpeta visible --</option>
            {#each config.candidates as candidate}
              <option value={candidate}>{candidate}</option>
            {/each}
          </select>
          <p class="storage-hint">
            En la app empaquetada este selector se sustituirá por el explorador nativo.
            Desde navegador solo están disponibles las rutas visibles para el backend.
          </p>
        </div>
      </div>
    {:else if configLoading}
      <p class="storage-empty">Cargando configuración de almacenamiento…</p>
    {/if}
  </section>

  <section class="storage-section">
    <h3>Carpeta de destino de las exportaciones</h3>
    <p class="storage-hint">Aquí se guardan los archivos resultantes de Unir y exportar, las exportaciones del DAW y los MIDI.</p>

    {#if exportError}
      <p class="storage-error">{exportError}</p>
    {/if}
    {#if exportSuccess}
      <p class="storage-success">{exportSuccess}</p>
    {/if}

    {#if config}
      <div class="root-summary">
        <div class="root-row">
          <span class="root-label">Carpeta actual</span>
          <code class="root-path">{config.export_dir || '(ubicación por defecto)'}</code>
        </div>
        <div class="root-row">
          <span class="root-label">Origen</span>
          <span>{sourceLabels[config.export_source] ?? config.export_source}</span>
        </div>
        <div class="root-row">
          <span class="root-label">Estado</span>
          <span class="root-status">
            {#if config.export_dir === ''}
              <span class="status-info">ℹ️ Por defecto: cada exportación se guarda en su ubicación habitual.</span>
            {:else if config.export_exists && config.export_writable}
              <span class="status-ok">✅ Existe y se puede escribir</span>
            {:else if config.export_exists}
              <span class="status-warn">⚠️ Existe pero no se puede escribir</span>
            {:else}
              <span class="status-warn">⚠️ No existe o no es accesible</span>
            {/if}
          </span>
        </div>
      </div>

      <div class="root-editor">
        <label for="export-dir-path" class="root-label">Nueva carpeta de exportaciones</label>
        <div class="root-input-row">
          <input
            id="export-dir-path"
            type="text"
            class="root-input"
            bind:value={exportInput}
            disabled={savingExport}
            placeholder="/ruta/absoluta/de/exportaciones"
          />
          <button class="btn-primary" onclick={saveExport} disabled={savingExport || exportInput === (config.export_dir || '')}>
            Guardar
          </button>
        </div>

        {#if supportsNativeFolderPicker()}
          <button class="btn-secondary" onclick={pickExportFolder} disabled={savingExport}>
            {@html IconFolder} Elegir carpeta…
          </button>
        {:else}
          <p class="storage-hint">
            En navegador no está disponible un explorador de carpetas del servidor.
            Elige entre las rutas visibles o escribe la ruta a mano.
          </p>
        {/if}

        <div class="candidate-picker">
          <span class="root-label">Carpetas visibles</span>
          <select onchange={(e) => chooseExportCandidate(e.currentTarget.value)} disabled={savingExport}>
            <option value="">-- selecciona una carpeta visible --</option>
            {#each config.candidates as candidate}
              <option value={candidate}>{candidate}</option>
            {/each}
          </select>
          <p class="storage-hint">
            En la app empaquetada este selector se sustituirá por el explorador nativo.
            Desde navegador solo están disponibles las rutas visibles para el backend.
          </p>
        </div>
      </div>
    {:else if configLoading}
      <p class="storage-empty">Cargando configuración de exportaciones…</p>
    {/if}
  </section>

  <section class="storage-section">
    <h3>Carpeta de configuración</h3>
    <p class="storage-hint">Aquí se guardan los presets, los ajustes de la interfaz, los perfiles de exportación y las configuraciones de modelos. Debe estar dentro de la raíz de datos para que viaje con el volumen montado.</p>

    {#if configError}
      <p class="storage-error">{configError}</p>
    {/if}
    {#if configSuccess}
      <p class="storage-success">{configSuccess}</p>
    {/if}

    {#if config}
      <div class="root-summary">
        <div class="root-row">
          <span class="root-label">Carpeta actual</span>
          <code class="root-path">{config.config_dir || '(ubicación por defecto)'}</code>
        </div>
        <div class="root-row">
          <span class="root-label">Origen</span>
          <span>{sourceLabels[config.config_source] ?? config.config_source}</span>
        </div>
        <div class="root-row">
          <span class="root-label">Estado</span>
          <span class="root-status">
            {#if config.config_dir === ''}
              <span class="status-info">ℹ️ Por defecto: se usa <code>{config.current_root}/config</code>.</span>
            {:else if config.config_exists && config.config_writable}
              <span class="status-ok">✅ Existe y se puede escribir</span>
            {:else if config.config_exists}
              <span class="status-warn">⚠️ Existe pero no se puede escribir</span>
            {:else}
              <span class="status-warn">⚠️ No existe o no es accesible</span>
            {/if}
          </span>
        </div>
      </div>

      <div class="root-editor">
        <label for="config-dir-path" class="root-label">Nueva carpeta de configuración</label>
        <div class="root-input-row">
          <input
            id="config-dir-path"
            type="text"
            class="root-input"
            bind:value={configInput}
            disabled={savingConfig}
            placeholder="/ruta/absoluta/dentro/de/la/raíz"
          />
          <button class="btn-primary" onclick={saveConfig} disabled={savingConfig || configInput === (config.config_dir || '')}>
            Guardar
          </button>
        </div>

        {#if supportsNativeFolderPicker()}
          <button class="btn-secondary" onclick={pickConfigFolder} disabled={savingConfig}>
            {@html IconFolder} Elegir carpeta…
          </button>
        {:else}
          <p class="storage-hint">
            En navegador no está disponible un explorador de carpetas del servidor.
            Elige entre las rutas visibles o escribe la ruta a mano.
          </p>
        {/if}

        <div class="candidate-picker">
          <span class="root-label">Carpetas visibles</span>
          <select onchange={(e) => chooseConfigCandidate(e.currentTarget.value)} disabled={savingConfig}>
            <option value="">-- selecciona una carpeta visible --</option>
            {#each config.candidates as candidate}
              <option value={candidate}>{candidate}</option>
            {/each}
          </select>
          <p class="storage-hint">
            En la app empaquetada este selector se sustituirá por el explorador nativo.
            Desde navegador solo están disponibles las rutas visibles para el backend.
          </p>
        </div>
      </div>
    {:else if configLoading}
      <p class="storage-empty">Cargando configuración de la carpeta de configuración…</p>
    {/if}
  </section>

  <section class="storage-section">
    <h3>Uso de disco</h3>
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
            {@const isModels = key === 'models'}
            {@const isCache = key === 'cache'}
            {@const modelU = isModels && usage.models ? usage.models : null}
            {@const cacheU = isCache && usage.cache ? usage.cache : null}
            <tr>
              <td>{folderLabels[key] ?? key}</td>
              <td>{cacheU ? cacheU.files : (modelU ? modelU.entries : u.files)}</td>
              <td>{formatBytes(cacheU ? cacheU.bytes : (modelU ? modelU.bytes : u.bytes))}</td>
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
  </section>

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
    <p class="storage-hint storage-hint-strong">
      La limpieza <strong>solo</strong> actúa sobre temporales y ediciones del DAW.
      <strong>Nunca</strong> borra tus originales, los imports ni los resultados finales
      de la separación.
    </p>
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

  .storage-section {
    margin-bottom: 28px;
  }

  .storage-section h3 {
    margin: 0 0 12px;
    font-size: 15px;
    color: var(--text-primary);
  }

  .storage-error {
    color: #ff6b6b;
    margin-bottom: 12px;
  }

  .storage-success {
    color: #51cf66;
    margin-bottom: 12px;
  }

  .root-summary {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
    margin-bottom: 16px;
  }

  .root-row {
    display: flex;
    gap: 12px;
    margin-bottom: 8px;
    align-items: flex-start;
  }

  .root-row:last-child {
    margin-bottom: 0;
  }

  .root-label {
    color: var(--text-secondary);
    font-size: 13px;
    min-width: 110px;
    flex-shrink: 0;
  }

  .root-path {
    font-size: 13px;
    word-break: break-all;
    background: var(--bg);
    padding: 2px 6px;
    border-radius: 4px;
  }

  .root-status {
    font-size: 13px;
  }

  .status-ok {
    color: #51cf66;
  }

  .status-warn {
    color: #ffa94d;
  }

  .status-info {
    color: var(--text-secondary);
  }

  .root-note {
    font-size: 13px;
    color: var(--text-secondary);
    font-style: italic;
  }

  .root-editor {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .root-input-row {
    display: flex;
    gap: 10px;
    align-items: center;
  }

  .root-input {
    flex: 1;
    background: var(--bg);
    border: 1px solid var(--border-light);
    color: var(--text-primary);
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 13px;
  }

  .root-input:disabled {
    opacity: 0.6;
  }

  .btn-primary {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: #fff;
    padding: 8px 16px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    transition: all 0.15s ease;
  }

  .btn-primary:hover:not(:disabled) {
    filter: brightness(1.1);
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-secondary {
    background: transparent;
    border: 1px solid var(--accent);
    color: var(--accent-light);
    padding: 8px 16px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    transition: all 0.15s ease;
  }

  .btn-secondary:hover:not(:disabled) {
    background: var(--accent-bg);
  }

  .btn-secondary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .candidate-picker {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .candidate-picker select {
    background: var(--bg);
    border: 1px solid var(--border-light);
    color: var(--text-primary);
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 13px;
  }

  .candidate-picker select:disabled {
    opacity: 0.6;
  }

  .storage-table {
    width: 100%;
    border-collapse: collapse;
    margin-bottom: 16px;
  }

  .config-table {
    margin-bottom: 16px;
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

  .folder-missing {
    color: var(--text-secondary);
    font-size: 12px;
    margin-left: 6px;
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

  .storage-hint-strong {
    max-width: 640px;
    line-height: 1.5;
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
