<script lang="ts">
  import { onDestroy } from 'svelte';
  import ModelManager from './ModelManager.svelte';
  import ModelDownloader from './ModelDownloader.svelte';
  import PipelineEditor from './PipelineEditor.svelte';
  import InterfaceSettings from './InterfaceSettings.svelte';
  import { IconModel, IconDownload, IconPresets, IconLogs, IconClose, IconRefresh, IconSettings } from './icons';

  interface Props {
    subtab?: string;
    onsubtabchange?: (tab: string) => void;
  }

  let { subtab = 'models', onsubtabchange }: Props = $props();

  // No-op handlers for sub-component props
  const noop = () => {};
  const noopStart = (_config: any) => {};

  // ---- Logs state (from App.svelte) ----
  const API_BASE = '';
  let logs = $state<Array<{nano: number, level: string, service: string, message: string}>>([]);
  let logDetail = $state<{nano: number, level: string, service: string, message: string} | null>(null);
  let logTab = $state<'events' | 'services'>('events');
  let serviceLogs = $state<Array<{nano: number, level: string, service: string, message: string}>>([]);
  let serviceLogsLoading = $state(false);
  let serviceLogLimit = $state(50);
  let displayed = $derived(serviceLogLimit > 0 ? serviceLogs.slice(0, serviceLogLimit) : serviceLogs);
  let logsPollTimer: ReturnType<typeof setInterval> | null = null;

  async function loadServiceLogs() {
    serviceLogsLoading = true;
    try {
      const [eventsRes, svcRes] = await Promise.all([
        fetch(`${API_BASE}/api/logs`),
        fetch(`${API_BASE}/api/logs/services`)
      ]);
      const events = await eventsRes.json();
      const svcLogs = await svcRes.json();
      serviceLogs = [...events, ...svcLogs].sort((a, b) => b.nano - a.nano);
    } catch {
      serviceLogs = [];
    }
    serviceLogsLoading = false;
  }

  async function loadLogs() {
    try {
      const res = await fetch(`${API_BASE}/api/logs`);
      const allLogs = await res.json();
      logs = allLogs.filter((e: any) => !(e.service === 'pipeline' && e.level === 'info'));
    } catch {
      logs = [];
    }
  }

  function stopLogsPolling() {
    if (logsPollTimer) { clearInterval(logsPollTimer); logsPollTimer = null; }
  }

  function startLogsPolling() {
    stopLogsPolling();
    if (logTab === 'events') {
      loadLogs();
      logsPollTimer = setInterval(loadLogs, 3000);
    } else if (logTab === 'services') {
      loadServiceLogs();
    }
  }

  $effect(() => {
    if (subtab === 'logs') {
      startLogsPolling();
      return stopLogsPolling;
    }
  });

  $effect(() => {
    logTab;
    if (subtab === 'logs') {
      startLogsPolling();
      return stopLogsPolling;
    }
  });

  function handleSubtabChange(tab: string) {
    if (onsubtabchange) {
      onsubtabchange(tab);
    }
  }

  function copyToClipboard(text: string) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).catch(() => fallbackCopy(text));
    } else {
      fallbackCopy(text);
    }
  }

  function fallbackCopy(text: string) {
    const ta = document.createElement('textarea');
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    ta.style.top = '-9999px';
    document.body.appendChild(ta);
    ta.value = text;
    ta.focus();
    ta.select();
    try {
      document.execCommand('copy');
    } catch {
      // silently fail
    }
    document.body.removeChild(ta);
  }

  onDestroy(() => {
    stopLogsPolling();
  });
</script>

<div class="settings-panel">
  <!-- Sub-tabs centered at top -->
  <div class="settings-tabs">
    <button
      class="settings-tab"
      class:active={subtab === 'models'}
      onclick={() => handleSubtabChange('models')}
    >{@html IconModel} <span>Modelos</span></button>
    <button
      class="settings-tab"
      class:active={subtab === 'download'}
      onclick={() => handleSubtabChange('download')}
    >{@html IconDownload} <span>Descargar</span></button>
    <button
      class="settings-tab"
      class:active={subtab === 'presets'}
      onclick={() => handleSubtabChange('presets')}
    >{@html IconPresets} <span>Presets</span></button>
    <button
      class="settings-tab"
      class:active={subtab === 'logs'}
      onclick={() => handleSubtabChange('logs')}
    >{@html IconLogs} <span>Registros</span></button>
    <button
      class="settings-tab"
      class:active={subtab === 'interface'}
      onclick={() => handleSubtabChange('interface')}
    >{@html IconSettings} <span>Interfaz</span></button>
  </div>

  <!-- Body content -->
  <div class="settings-body">
    {#if subtab === 'models'}
      <div class="subtab-content">
        <ModelManager onclose={noop} initialModel={undefined} />
      </div>
    {:else if subtab === 'download'}
      <div class="subtab-content">
        <ModelDownloader onclose={noop} />
      </div>
    {:else if subtab === 'presets'}
      <div class="subtab-content">
        <PipelineEditor disabled={false} hasFiles={false} onstart={noopStart} />
      </div>
    {:else if subtab === 'logs'}
      <div class="logs-container">
        <div class="logs-header">
          <div class="log-tabs">
            <button class="log-tab" class:active={logTab === 'events'} onclick={() => logTab = 'events'}>Eventos</button>
            <button class="log-tab" class:active={logTab === 'services'} onclick={() => { logTab = 'services'; loadServiceLogs(); }}>Servicios</button>
          </div>
          {#if logTab === 'services'}
            <select
              value={serviceLogLimit}
              onchange={(e) => serviceLogLimit = parseInt((e.target as HTMLSelectElement).value)}
              class="log-filter"
            >
              <option value={50}>Últimos 50</option>
              <option value={100}>Últimos 100</option>
              <option value={500}>Últimos 500</option>
              <option value={0}>Todos</option>
            </select>
          {/if}
          <button class="btn-refresh" onclick={() => logTab === 'events' ? loadLogs() : loadServiceLogs()} title="Refrescar">{@html IconRefresh}</button>
        </div>
        <div class="logs-list">
          {#if logTab === 'events'}
            {#if logs.length === 0}
              <p class="logs-empty">No hay registros todavía.</p>
            {:else}
              {#each logs as log}
                <div
                  class="log-row log-{log.level}"
                  onclick={() => logDetail = log}
                >
                  <span class="log-time">{new Date(log.nano / 1e6).toLocaleString()}</span>
                  <span class="log-service" style="color: {log.service === 'pipeline' ? '#ff9800' : log.service === 'inference' ? '#9c27b0' : '#6c757d'}">{log.service}</span>
                  <span class="log-level">{log.level === 'error' ? '🔴' : log.level === 'success' ? '🟢' : '⚪'}</span>
                  <span class="log-msg">{log.message.slice(0, 80)}{log.message.length > 80 ? '...' : ''}</span>
                </div>
              {/each}
            {/if}
          {:else}
            {#if serviceLogsLoading}
              <p class="logs-empty">Cargando logs de servicios...</p>
            {:else if serviceLogs.length === 0}
              <p class="logs-empty">No se pudieron cargar los logs de servicios.</p>
            {:else}
              {#each displayed as log}
                <div
                  class="log-row log-{log.level}"
                  onclick={() => logDetail = log}
                >
                  <span class="log-time">{new Date(log.nano / 1e6).toLocaleString()}</span>
                  <span class="log-service" style="color: {log.service === 'pipeline' ? '#ff9800' : log.service === 'backend' ? '#2196f3' : log.service === 'onda' ? '#9c27b0' : '#6c757d'}">{log.service}</span>
                  <span class="log-level">{log.level === 'error' ? '🔴' : log.level === 'success' ? '🟢' : '⚪'}</span>
                  <span class="log-msg">{log.message.slice(0, 100)}{log.message.length > 100 ? '...' : ''}</span>
                </div>
              {/each}
            {/if}
          {/if}
        </div>
      </div>
    {:else if subtab === 'interface'}
      <div class="subtab-content">
        <InterfaceSettings />
      </div>
    {/if}
  </div>
</div>

<!-- Log detail overlay -->
{#if logDetail}
  <div class="logs-overlay" onclick={() => logDetail = null}>
    <div class="log-detail-panel" onclick={(e) => e.stopPropagation()}>
      <div class="logs-header">
        <h2>Detalle del evento</h2>
        <button class="btn-icon" onclick={() => logDetail = null}>{@html IconClose}</button>
      </div>
      <div class="log-detail-meta">
        <span class="log-detail-level" class:log-error={logDetail.level === 'error'} class:log-success={logDetail.level === 'success'}>
          {logDetail.level === 'error' ? '🔴 Error' : logDetail.level === 'success' ? '🟢 Éxito' : '⚪ Info'}
        </span>
        <span class="log-detail-service">Servicio: {logDetail.service}</span>
        <span class="log-detail-time">{new Date(logDetail.nano / 1e6).toLocaleString()}</span>
      </div>
      <pre class="log-detail-msg">{logDetail.message}</pre>
      <div class="log-detail-actions">
        <button class="btn-icon" onclick={() => copyToClipboard(logDetail!.message)}>📋 Copiar</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .settings-panel {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    background: var(--bg-primary);
  }

  /* ── Sub-tabs ── */
  .settings-tabs {
    display: flex;
    justify-content: center;
    gap: 4px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-primary);
    flex-shrink: 0;
  }

  .settings-tab {
    background: transparent;
    border: 1px solid transparent;
    color: var(--text-secondary);
    padding: 6px 16px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    transition: all 0.15s ease;
    white-space: nowrap;
  }

  .settings-tab:hover {
    color: var(--text-secondary);
    border-color: var(--border-light);
    background: rgba(108, 92, 231, 0.08);
  }

  .settings-tab.active {
    background: rgba(108, 92, 231, 0.18);
    border-color: #6c5ce7;
    color: #c8bfff;
  }

  /* ── Body ── */
  .settings-body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .subtab-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  /* ── Override child component fullscreen when embedded ── */
  :global(.subtab-content .fullscreen) {
    position: relative !important;
    top: auto !important;
    left: auto !important;
    right: auto !important;
    bottom: auto !important;
    background: transparent !important;
    z-index: auto !important;
    animation: none !important;
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  :global(.subtab-content .fullscreen-header) {
    display: none !important;
  }

  :global(.subtab-content .fullscreen-body) {
    flex: 1;
    overflow-y: auto;
    padding: 0 !important;
  }

  :global(.subtab-content .fullscreen-body .loading-text) {
    padding: 20px;
  }

  /* ── Logs ── */
  .logs-container {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .logs-header {
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .log-tabs {
    display: flex;
    gap: 4px;
  }

  .log-tab {
    background: transparent;
    border: 1px solid var(--border-light);
    color: var(--text-secondary);
    padding: 4px 12px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
    transition: all 0.15s ease;
  }

  .log-tab.active {
    background: rgba(108, 92, 231, 0.18);
    border-color: #6c5ce7;
    color: #c8bfff;
  }

  .log-filter {
    background: #1e1e30;
    border: 1px solid var(--border-light);
    color: var(--text-secondary);
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 12px;
    cursor: pointer;
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

  .btn-refresh:hover {
    background: #333;
    color: #fff;
  }

  .logs-list {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
  }

  .logs-empty {
    color: var(--text-secondary);
    text-align: center;
    padding: 40px;
    font-size: 14px;
  }

  .log-row {
    display: flex;
    gap: 10px;
    padding: 8px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    border-left: 3px solid transparent;
    margin-bottom: 2px;
    transition: background 0.15s ease;
  }

  .log-row:hover {
    background: rgba(128, 128, 128, 0.1);
  }

  .log-row.log-error {
    border-left-color: #dc3545;
  }

  .log-row.log-success {
    border-left-color: #28a745;
  }

  .log-row.log-info {
    border-left-color: #6c757d;
  }

  .log-time {
    color: var(--text-secondary);
    white-space: nowrap;
    min-width: 140px;
    font-family: monospace;
    font-size: 11px;
  }

  .log-service {
    font-size: 11px;
    min-width: 70px;
    font-weight: bold;
    flex-shrink: 0;
  }

  .log-level {
    flex-shrink: 0;
    width: 20px;
    text-align: center;
  }

  .log-msg {
    color: var(--text-primary);
    word-break: break-word;
    flex: 1;
  }

  .btn-icon {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    padding: 6px 14px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    transition: all 0.15s ease;
  }

  .btn-icon:hover {
    background: #333;
    color: #fff;
  }

  /* ── Log detail overlay ── */
  :global(.logs-overlay) {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: fadeIn 0.15s ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .log-detail-panel {
    background: #1e1e2e;
    border-radius: 12px;
    width: 90vw;
    max-width: 900px;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
  }

  .log-detail-panel .logs-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
  }

  .log-detail-panel .logs-header h2 {
    margin: 0;
    color: var(--text-primary);
    font-size: 18px;
  }

  .log-detail-meta {
    display: flex;
    gap: 16px;
    padding: 12px 20px;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
    color: var(--text-secondary);
  }

  .log-detail-level {
    font-weight: bold;
  }

  :global(.log-error) {
    color: #dc3545;
  }

  :global(.log-success) {
    color: #28a745;
  }

  .log-detail-service {
    color: var(--text-secondary);
  }

  .log-detail-time {
    color: var(--text-secondary);
    margin-left: auto;
  }

  .log-detail-msg {
    flex: 1;
    overflow: auto;
    padding: 20px;
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: 'Courier New', monospace;
    font-size: 13px;
    color: var(--text-primary);
    line-height: 1.5;
  }

  .log-detail-actions {
    display: flex;
    justify-content: flex-end;
    padding: 12px 20px;
    border-top: 1px solid var(--border);
  }
</style>
