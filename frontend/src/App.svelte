<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import Sidebar from './lib/Sidebar.svelte';
  import PipelineView from './lib/PipelineView.svelte';
  import PitchPage from './lib/PitchPage.svelte';
  import SettingsPanel from './lib/SettingsPanel.svelte';
  import PlaceholderPage from './lib/PlaceholderPage.svelte';
  import DAWWorkspace from './lib/DAWWorkspace.svelte';
  import MIDIPage from './lib/MIDIPage.svelte';
  import SpectrogramPage from './lib/SpectrogramPage.svelte';
  import BpmPage from './lib/BpmPage.svelte';
  import ExportPage from './lib/ExportPage.svelte';
  import HelpPage from './lib/HelpPage.svelte';
  import PresetsPanel from './lib/PresetsPanel.svelte';
  import type { ResultStem } from './lib/types';
  import { detectStemType } from './lib/types';
  import { separateAudio, uploadAudio, getQueueStatus, getResults, getInputs, deleteInput, getHealth, getGpuInfo, getPresets, getDefaultPreset, clearQueue, cancelQueue, loadUISettings, type InputEntry, type ResultsGroup } from './lib/api';
  import type { QueueJob } from './lib/api';
  import { deriveQueueFileStatus, resolveOutputGroupName, songNameForQueueFile } from './lib/queueState';
  import { decideCompletion, hasActiveJob } from './lib/queueCompletion';
  import { formatEta } from './lib/time';
  import { IconOnda, IconStar, IconVoiceRemove, IconSeparate, IconInstruments, IconUser } from './lib/icons';
  import { getDefaultChecked, applyDefaultChecked, withToggledCheck, withToggledAll } from './lib/queueDefaults';
  import type { QueueFile } from './lib/queueDefaults';


  interface PipelineConfigType {
    preset?: string;
    steps?: Array<{
      id: string;
      model: string;
      type: string;
      enabled: boolean;
      stems: Record<string, { action: string; target?: string }>;
    }>;
  }

  // ---- State ----
  let queueFiles = $state<QueueFile[]>([]);
  let separating = $state(false);
  let results = $state<ResultStem[]>([]);
  let resultGroups = $state<ResultsGroup[]>([]);
  let pipelineStatus = $state<'idle'|'running'|'done'|'error'>('idle');
  let pipelineStep = $state('');
  let pipelineSong = $state('');
  let currentProgress = $state(0);
  let pipelineEta = $state('');
  let inferenceDevice = $state('');
  let pipelineModel = $state('');
  let pipelineFlags = $state('');
  let savedPresets = $state<{name: string, config: any}[]>([]);
  let selectedPresetName = $state('');

  // ---- Queue state ----
  let queueJobs = $state<QueueJob[]>([]);
  let queuePollingTimer: ReturnType<typeof setInterval> | null = null;
  let processedDoneSongs = $state<Set<string>>(new Set());
  let activeSongNames = $state<Set<string>>(new Set()); // songs submitted in current batch
  let emptyQueueTicks = $state(0);
  const EMPTY_QUEUE_THRESHOLD = 3; // tolerate transient empty status ticks

  // Completion confirmation state: require consecutive settled polls plus a
  // grace period before declaring "Completado" so a single poll never decides.
  let settledTicks = $state(0);
  let settledGraceStart: number | null = $state(null);
  const REQUIRED_SETTLED_TICKS = 2;
  const SETTLED_GRACE_MS = 12000; // keep polling a bit after the first settled poll

  // ---- Live inputs refresh state ----
  let inputsRefreshTimer: ReturnType<typeof setInterval> | null = null;
  const INPUTS_REFRESH_INTERVAL_MS = 20000; // 20 s

  // ---- Live results refresh state ----
  let resultsRefreshTimer: ReturnType<typeof setInterval> | null = null;
  const RESULTS_REFRESH_INTERVAL_MS = 5000; // 5 s

  function isQueueVisible(tab: string): boolean {
    return tab === 'personalizado' || isPresetTab(tab);
  }

  // ---- Safeguard: if the UI flag gets stuck but there is no real work, fix it next tick ----
  $effect(() => {
    if (!separating) return;
		const hasActiveJob = queueJobs.some(j => j.status === 'waiting' || j.status === 'processing' || j.status === 'blocked_no_gpu');
		const hasActiveFile = queueFiles.some(qf => qf.status === 'uploading' || qf.status === 'processing' || qf.status === 'blocked_no_gpu');
    if (!hasActiveJob && !hasActiveFile) {
      separating = false;
      if (pipelineStatus === 'running') {
        pipelineStatus = 'idle';
      }
    }
  });

  // ---- Live input refresh: react to tab navigation and external uploads ----
  $effect(() => {
    activeTab;
    syncInputsPolling();
  });

  // ---- Live results refresh: keep the "done" state in sync with disk ----
  $effect(() => {
    activeTab;
    syncResultsPolling();
  });

  // ---- Health / Version / GPU from backend ----
  let healthVersion = $state('');
  const appVersion = $state(import.meta.env.VITE_ONDA_VERSION || '');
  let gpuType = $state<'cuda' | 'cpu' | ''>('');
  let gpuWarning = $state('');
  let gpuDetail = $state('');
  let gpuUsableByTorch = $state(true);
  let gpuDriverOk = $state(false);
  let cpuWarningDismissed = $state(false);

  // Toast
  let toastMessage = $state('');
  let toastType = $state<'success' | 'error'>('success');
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Persistent error banner
  let errorBanner = $state<{ message: string; log?: string } | null>(null);
  let errorLogDetail = $state<string | null>(null);

  // ---- New layout state ----
  let activeTab = $state('personalizado');
  let sidebarCollapsed = $state(false);
  let settingsSubTab = $state('models');
  let activeTabName = $derived(activeTab);

  /** Visible global CPU warning derived from health + GPU info endpoints */
  let showCpuWarning = $derived((gpuType === 'cpu' || !gpuUsableByTorch) && !cpuWarningDismissed);
  let cpuWarningText = $derived(
    gpuDriverOk && !gpuUsableByTorch
      ? 'La máquina tiene GPU pero torch no la ve. La inferencia se ejecutará en CPU y será mucho más lenta.'
      : gpuWarning || gpuDetail || 'No se detectó GPU usable. La inferencia puede tardar minutos en lugar de segundos.'
  );

  /** Icon mapping for locked (built-in) presets */
  const BUILTIN_ICONS: Record<string, string> = {
    'Separador Voces Total': IconStar,
    'Eliminador de Voz': IconVoiceRemove,
    'Separador Completo': IconSeparate,
    'Separador solo instrumentos': IconInstruments,
  };

  /** Sidebar items derived from savedPresets */
  let sidebarPresets = $derived(
    savedPresets.map(p => ({
      id: p.name,
      name: p.name,
      icon: BUILTIN_ICONS[p.name] || IconUser,
    }))
  );

  /** Check if a tab ID corresponds to a known preset */
  function isPresetTab(tabId: string): boolean {
    return savedPresets.some(p => p.name === tabId);
  }

  function copyToClipboard(text: string) {
    // navigator.clipboard requires HTTPS or localhost — fallback for HTTP
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).catch(() => fallbackCopy(text));
    } else {
      fallbackCopy(text);
    }
  }

  function fallbackCopy(text: string) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    ta.style.top = '-9999px';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    try {
      document.execCommand('copy');
    } catch {
      // silently fail
    }
    document.body.removeChild(ta);
  }

  function showToast(message: string, type: 'success' | 'error') {
    if (type === 'error') {
      errorBanner = { message };
    } else {
      toastMessage = message;
      toastType = type;
      if (toastTimer) clearTimeout(toastTimer);
      toastTimer = setTimeout(() => {
        toastMessage = '';
      }, 3000);
    }
  }

  /** Merge server-side input files into the queue without losing row state. */
  function mergeDiskInputs(inputs: InputEntry[]) {
    if (inputs.length === 0) return;

    const byPath = new Set(queueFiles.map(q => q.path).filter(Boolean));
    const uploadingNames = new Set(
      queueFiles.filter(q => q.status === 'uploading' && !q.path).map(q => q.file.name),
    );

    const newQueueFiles: QueueFile[] = [];
    for (const input of inputs) {
      if (byPath.has(input.path)) continue;
      // Avoid adding a server file that matches an in-progress local upload
      if (uploadingNames.has(input.name)) continue;
      newQueueFiles.push({
        file: new File([], input.name),
        id: crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}-${input.name}`,
        status: 'waiting',
        checked: getDefaultChecked('waiting'),
        userTouched: false,
        path: input.path,
      });
    }

    if (newQueueFiles.length > 0) {
      queueFiles = [...queueFiles, ...newQueueFiles];
      console.log('Merged', newQueueFiles.length, 'inputs from disk');
    }
    syncQueueFileStatusFromDisk();
  }

  async function refreshInputsFromDisk() {
    try {
      const inputs = await getInputs();
      mergeDiskInputs(inputs);
    } catch (err) {
      console.error('Failed to refresh inputs from disk:', err);
    }
  }

  function startInputsPolling() {
    if (inputsRefreshTimer) return;
    inputsRefreshTimer = setInterval(() => {
      refreshInputsFromDisk();
    }, INPUTS_REFRESH_INTERVAL_MS);
  }

  function stopInputsPolling() {
    if (inputsRefreshTimer) {
      clearInterval(inputsRefreshTimer);
      inputsRefreshTimer = null;
    }
  }

  function syncInputsPolling() {
    if (isQueueVisible(activeTab) && document.visibilityState !== 'hidden') {
      startInputsPolling();
    } else {
      stopInputsPolling();
    }
  }

  function startResultsPolling() {
    if (resultsRefreshTimer) return;
    refreshResultsFromDisk();
    resultsRefreshTimer = setInterval(() => {
      refreshResultsFromDisk();
    }, RESULTS_REFRESH_INTERVAL_MS);
  }

  function stopResultsPolling() {
    if (resultsRefreshTimer) {
      clearInterval(resultsRefreshTimer);
      resultsRefreshTimer = null;
    }
  }

  function syncResultsPolling() {
    if (isQueueVisible(activeTab) && document.visibilityState !== 'hidden') {
      startResultsPolling();
    } else {
      stopResultsPolling();
    }
  }

  function handleVisibilityChange() {
    syncInputsPolling();
    syncResultsPolling();
  }

  /** Fallback: load UI settings from localStorage */
  function applyLocalStorageSettings() {
    const savedAccent = localStorage.getItem('onda-accent');
    if (savedAccent) {
      const body = document.body;
      body.style.setProperty('--accent', savedAccent);
      const num = parseInt(savedAccent.replace('#', ''), 16);
      const r = Math.min(255, Math.max(0, (num >> 16)));
      const g = Math.min(255, Math.max(0, ((num >> 8) & 0xff)));
      const b = Math.min(255, Math.max(0, (num & 0xff)));
      const lightR = Math.min(255, r + 40);
      const lightG = Math.min(255, g + 40);
      const lightB = Math.min(255, b + 40);
      body.style.setProperty('--accent-light', `rgb(${lightR}, ${lightG}, ${lightB})`);
      body.style.setProperty('--accent-dark', `rgb(${Math.max(0, r - 30)}, ${Math.max(0, g - 30)}, ${Math.max(0, b - 30)})`);
      body.style.setProperty('--accent-glow', savedAccent + '4d');
      body.style.setProperty('--accent-subtle', savedAccent + '14');
      body.style.setProperty('--accent-bg', savedAccent + '22');
      body.style.setProperty('--accent-border', savedAccent + '33');
      body.style.accentColor = savedAccent;
    }
    const savedTheme = localStorage.getItem('onda-theme');
    if (savedTheme === 'light') {
      document.body.classList.add('light-theme');
    }
    const savedFontSize = localStorage.getItem('onda-font-size');
    if (savedFontSize) {
      const root = document.documentElement;
      const sizes = { small: '12px', medium: '14px', large: '16px' };
      root.style.fontSize = sizes[savedFontSize as keyof typeof sizes] || '14px';
    }
    const savedScale = localStorage.getItem('onda-scale');
    if (savedScale) {
      document.body.style.zoom = `${savedScale}%`;
    }
  }

  // Load model list + persisted data on mount
  onMount(() => {
    // ── Load persisted UI settings (accent, theme, fontSize, scale) ──
    // Try API first, fallback to localStorage
    loadUISettings().then(settings => {
      if (settings) {
        // Apply accent from API
        if (settings.accent) {
          const body = document.body;
          body.style.setProperty('--accent', settings.accent);
          const num = parseInt(settings.accent.replace('#', ''), 16);
          const r = Math.min(255, Math.max(0, (num >> 16)));
          const g = Math.min(255, Math.max(0, ((num >> 8) & 0xff)));
          const b = Math.min(255, Math.max(0, (num & 0xff)));
          const lightR = Math.min(255, r + 40);
          const lightG = Math.min(255, g + 40);
          const lightB = Math.min(255, b + 40);
          body.style.setProperty('--accent-light', `rgb(${lightR}, ${lightG}, ${lightB})`);
          body.style.setProperty('--accent-dark', `rgb(${Math.max(0, r - 30)}, ${Math.max(0, g - 30)}, ${Math.max(0, b - 30)})`);
          body.style.setProperty('--accent-glow', settings.accent + '4d');
          body.style.setProperty('--accent-subtle', settings.accent + '14');
          body.style.setProperty('--accent-bg', settings.accent + '22');
          body.style.setProperty('--accent-border', settings.accent + '33');
          body.style.accentColor = settings.accent;
        }
        // Apply theme from API
        if (settings.theme === 'light') {
          document.body.classList.add('light-theme');
        }
        // Apply font size from API
        if (settings.fontSize) {
          const root = document.documentElement;
          const sizes = { small: '12px', medium: '14px', large: '16px' };
          root.style.fontSize = sizes[settings.fontSize as keyof typeof sizes] || '14px';
        }
        // Apply scale from API
        if (settings.scale) {
          document.body.style.zoom = `${settings.scale}%`;
        }
        return; // API applied, skip localStorage
      }
      // Fallback to localStorage
      applyLocalStorageSettings();
    }).catch(() => {
      // API failed, fallback to localStorage
      applyLocalStorageSettings();
    });

    // ── Load version / GPU health from backend ──
    getHealth()
      .then((h) => {
        if (h?.version) healthVersion = h.version;
        if (h?.gpu?.type) gpuType = h.gpu.type;
        if (h?.gpu?.warning) gpuWarning = h.gpu.warning;
        if (h?.gpu?.detail) gpuDetail = h.gpu.detail;
        if (h?.gpu && typeof h.gpu.usable_by_torch === 'boolean') {
          gpuUsableByTorch = h.gpu.usable_by_torch;
        }
        // Fallback: derive type from gpu.ok if type not present
        if (h?.gpu && !h.gpu.type) {
          gpuType = h.gpu.ok ? 'cuda' : 'cpu';
        }
      })
      .catch(() => {}); // silent fail

    // ── Distinguish "GPU visible to nvidia-smi" vs "GPU usable by torch" ──
    getGpuInfo()
      .then((gpu) => {
        gpuDriverOk = gpu.ok;
        gpuUsableByTorch = gpu.usable_by_torch;
        if (gpu.torch_info && !gpuDetail) gpuDetail = gpu.torch_info;
      })
      .catch(() => {}); // silent fail

    // ── Load persisted results from filesystem (/output/) ──
    refreshResultsFromDisk()
      .then(() => {
        if (results.length > 0) {
          pipelineStatus = 'done';
          currentProgress = 1;
          console.log('Loaded existing results from filesystem:', results.length, 'stems');
        }
      })
      .catch((err) => {
        console.error('Failed to load results from filesystem:', err);
      });

    // ── Load persisted inputs from filesystem (/input/) ──
    refreshInputsFromDisk();

    // ── Restore active queue jobs ──
    getQueueStatus()
      .then((status) => {
        queueJobs = status.jobs || [];
        // Restore results for already-done jobs
        const activeJobs = status.jobs?.filter(j => j.status === 'waiting' || j.status === 'processing') || [];
        if (activeJobs.length > 0) {
          console.log('Restoring', activeJobs.length, 'active queue jobs');
          separating = true;
          pipelineStatus = 'running';
          startQueuePolling();
        }
        // Rebuild results from the current authoritative job.files, replacing
        // any previous stems for songs that are now done.
        rebuildResultsFromJobs(status.jobs || []);
      })
      .catch((err) => {
        console.error('Failed to restore queue status:', err);
      });

    // ── Load presets ──
    getPresets().then(data => {
      const list = Object.entries(data).map(([name, p]: [string, any]) => ({
        name,
        config: {
          preset: name,
          steps: p.steps || [],
        }
      }));
      savedPresets = list;
      getDefaultPreset().then(data => {
        if (data?.name && savedPresets.some(p => p.name === data.name)) {
          selectedPresetName = data.name;
        }
      });
    }).catch(() => {});

    // ── Start live input/results refresh while the queue view is visible ──
    syncInputsPolling();
    syncResultsPolling();
    document.addEventListener('visibilitychange', handleVisibilityChange);
  });

  // Cleanup timers on unmount
  onDestroy(() => {
    if (queuePollingTimer) clearInterval(queuePollingTimer);
    stopInputsPolling();
    stopResultsPolling();
    document.removeEventListener('visibilitychange', handleVisibilityChange);
  });

  // ---- Presets refresh (called when editor closes) ----
  function refreshPresets() {
    getPresets().then(data => {
      const list = Object.entries(data).map(([name, p]: [string, any]) => ({
        name,
        config: {
          preset: name,
          steps: p.steps || [],
        }
      }));
      savedPresets = list;
    }).catch(() => {});
  }

  // ---- File Queue handlers ----
  async function handleFilesAdded(newFiles: File[]) {
    for (const f of newFiles) {
      const id = crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
      const qf: QueueFile = {
        file: f,
        id,
        status: 'uploading',
        checked: getDefaultChecked('uploading'),
        userTouched: false,
      };
      queueFiles = [...queueFiles, qf];
      try {
        const res = await uploadAudio(f);
        queueFiles = queueFiles.map(q => {
          if (q.id !== id) return q;
          const checked = q.userTouched ? q.checked : getDefaultChecked('waiting');
          return { ...q, status: 'waiting', path: res.path, checked };
        });
      } catch (err: any) {
        queueFiles = queueFiles.map(q => {
          if (q.id !== id) return q;
          const checked = q.userTouched ? q.checked : getDefaultChecked('error');
          return { ...q, status: 'error', errorMsg: err.message || 'Upload failed', checked };
        });
      }
    }
    // Pick up files uploaded from other tabs / sources without reloading
    await refreshInputsFromDisk();
  }

  function handleDropZoneFile(f: File) {
    handleFilesAdded([f]);
  }

  function handleClearQueue() {
    queueFiles = [];
    results = [];
    queueJobs = [];
    if (queuePollingTimer) {
      clearInterval(queuePollingTimer);
      queuePollingTimer = null;
    }
    processedDoneSongs = new Set();
    showToast('Cola limpiada', 'success');
  }

  function handleToggleQueueFile(id: string) {
    queueFiles = withToggledCheck(queueFiles, id);
  }

  function handleToggleAll() {
    queueFiles = withToggledAll(queueFiles);
  }

  // ---- Pipeline start ----
  async function handlePipelineStart(config: PipelineConfigType) {
    // Clear any existing polling
    if (queuePollingTimer) {
      clearInterval(queuePollingTimer);
      queuePollingTimer = null;
    }

    // Clear queue on backend before starting new jobs
    try {
      await clearQueue();
    } catch (e) {
      // Non-fatal — continue even if clear fails
    }

    const checked = queueFiles.filter((qf) => qf.checked);
    if (checked.length === 0) {
      if (queueFiles.length > 0) {
        showToast('Marca al menos un archivo en la cola', 'warning');
      }
      return;
    }

    separating = true;
    pipelineStatus = 'running';
    pipelineStep = '';
    currentProgress = 0;
    pipelineEta = '';
    inferenceDevice = '';
    queueJobs = [];
    processedDoneSongs = new Set();
    activeSongNames = new Set();
    emptyQueueTicks = 0;

    // Mark checked files as uploading
    for (const qf of checked) {
      qf.status = 'uploading';
      qf.progress = 0;
    }

    try {
      // Upload all checked files (skip if already on server)
      const uploaded: { qf: QueueFile; path: string }[] = [];
      for (const qf of checked) {
        // If file already has a server path (restored from filesystem), skip upload
        if (qf.path) {
          qf.status = 'processing';
          uploaded.push({ qf, path: qf.path });
          continue;
        }
        try {
          const res = await uploadAudio(qf.file);
          qf.status = 'processing';
          qf.path = res.path;
          uploaded.push({ qf, path: res.path });
        } catch (err: any) {
          qf.status = 'error';
          qf.errorMsg = err.message;
        }
      }

      if (uploaded.length === 0) {
        separating = false;
        showToast('No files uploaded successfully.', 'error');
        return;
      }

      const preset = config.preset || '';
      const reservedOutputs = new Set<string>();

      // Enqueue each uploaded file via separateAudio.
      // When the song already has a stem group on disk, create a fresh copy
      // (e.g. "song (copia01)") so we never overwrite existing stems.
      for (const { qf, path } of uploaded) {
        const songName = songNameForQueueFile(qf);
        activeSongNames.add(songName);
        const outputName = resolveOutputGroupName(songName, resultGroups, reservedOutputs);
        reservedOutputs.add(outputName);
        try {
          const opts: any = {
            preset,
            input: path,
          };
          if (outputName !== songName) {
            opts.output = outputName;
          }
          if (config.steps && config.steps.length > 0) {
            opts.steps = config.steps;
          }
          await separateAudio(opts);
        } catch (err: any) {
          qf.status = 'error';
          qf.errorMsg = err.message;
        }
      }

      pipelineSong = uploaded[0]?.qf.file.name || '';

      // Start queue polling
      startQueuePolling();
    } catch (err: any) {
      showToast('Pipeline error: ' + err.message, 'error');
      separating = false;
    }
  }

  function formatJobFailureMessage(job: QueueJob): string {
    const d = job.failure_details;
    if (!d) return job.error || 'Error desconocido';
    if (d.step === 'device') {
      return `Error en dispositivo para "${job.song}": no hay GPU usable. ${d.error || ''}`.trim();
    }
    let msg = `Error en "${job.song}" — paso ${d.step} (código ${d.exit_code})`;
    if (d.error) msg += `: ${d.error}`;
    return msg;
  }

  /** Convert flat stems back into groups for disk-state checks. */
  function resultsToGroups(stems: ResultStem[]): ResultsGroup[] {
    const bySong = new Map<string, ResultsGroup>();
    for (const r of stems) {
      if (!bySong.has(r.song)) {
        bySong.set(r.song, { song: r.song, files: [] });
      }
      bySong.get(r.song)!.files.push({ name: r.name, path: r.path });
    }
    return Array.from(bySong.values());
  }

  /** Sync queue rows with the current disk + backend state. */
  function syncQueueFileStatusFromDisk() {
    queueFiles = queueFiles.map(qf => {
      const derived = deriveQueueFileStatus(qf, resultGroups, queueJobs);
      const statusChanged = qf.status !== derived.status;
      const progressChanged = qf.progress !== derived.progress;
      if (!statusChanged && !progressChanged) return qf;
      const checked = qf.userTouched ? qf.checked : derived.checked;
      return {
        ...qf,
        status: derived.status,
        progress: derived.progress,
        checked,
      };
    });
  }

  async function refreshResultsFromDisk() {
    try {
      const groups = await getResults();
      resultGroups = groups;
      const loadedResults: ResultStem[] = [];
      for (const group of groups) {
        for (const f of group.files) {
          loadedResults.push({
            name: f.name,
            path: f.path,
            song: group.song,
            stemType: detectStemType(f.name, group.song),
          });
        }
      }
      results = loadedResults;
      syncQueueFileStatusFromDisk();
    } catch (err) {
      console.error('Failed to refresh results from disk:', err);
    }
  }

  function resetPipelineUI() {
    separating = false;
    pipelineStatus = 'idle';
    pipelineStep = '';
    currentProgress = 0;
    pipelineEta = '';
    inferenceDevice = '';
    pipelineModel = '';
    pipelineFlags = '';
    queueJobs = [];
    processedDoneSongs = new Set();
    activeSongNames = new Set();
    settledTicks = 0;
    settledGraceStart = null;
    // Preserve done/error rows, reset the rest so they can be re-processed cleanly
    queueFiles = queueFiles.map(qf =>
      qf.status === 'done' || qf.status === 'error'
        ? qf
        : { ...qf, status: 'waiting', progress: 0, errorMsg: undefined }
    );
  }

  async function handleCancel() {
    // Ask backend to cancel first, then sync local state
    try {
      await cancelQueue();
    } catch {
      // ignore — we will sync anyway
    }

    // Optimistically reset UI so the button responds immediately
    if (queuePollingTimer) {
      clearInterval(queuePollingTimer);
      queuePollingTimer = null;
    }
    emptyQueueTicks = 0;
    resetPipelineUI();

    // Re-sync with backend so external cancellations (API / another client) are reflected
    await syncQueueStatus();
    if (queueJobs.some(j => j.status === 'waiting' || j.status === 'processing' || j.status === 'blocked_no_gpu')) {
      startQueuePolling();
    }

    showToast('⏹ Proceso cancelado', 'success');
  }

  /** Fetch backend queue state and update the UI accordingly. */
  async function syncQueueStatus(): Promise<boolean> {
    try {
      const status = await getQueueStatus();
      const jobs = status.jobs || [];
      queueJobs = jobs;

      // Update progress UI from the processing job
      const processingJob = jobs.find(j => j.status === 'processing');
      if (processingJob) {
        pipelineSong = processingJob.song;
        pipelineStep = processingJob.step_name || 'processing';
        pipelineEta = formatEta(processingJob.eta);
        inferenceDevice = processingJob.device || '';
        pipelineModel = processingJob.current_model || '';
        pipelineFlags = processingJob.current_flags || '';
      } else {
        // Avoid leaving a stale ETA when the job finishes or disappears.
        pipelineEta = '';
      }

      // Calculate total progress across the active batch using the backend's
      // per-job progress. The backend already aggregates step progress honestly,
      // so the frontend must not recalculate it from current_step/total_steps.
      const songsToCount = activeSongNames.size > 0 ? activeSongNames : new Set(jobs.map(j => j.song));
      if (jobs.length > 0 && songsToCount.size > 0) {
        let totalProgress = 0;
        let count = 0;
        for (const job of jobs) {
          if (!songsToCount.has(job.song)) continue;
          totalProgress += job.progress ?? 0;
          count++;
        }
        if (count > 0) {
          currentProgress = totalProgress / (count * 100);
        }
      }

      // Rebuild results from the current authoritative job.files and surface
      // per-job errors once per song.
      rebuildResultsFromJobs(jobs);
      for (const job of jobs) {
        if (job.status === 'error' && !processedDoneSongs.has(job.song)) {
          processedDoneSongs.add(job.song);
          const message = formatJobFailureMessage(job);
          const log = job.failure_details?.stderr || job.error || '';
          errorBanner = { message, log };
        }
      }

      // Reflect each job state in its queue row, including backend error messages
      queueFiles = queueFiles.map(qf => {
        const qfSong = songNameForQueueFile(qf);
        const job = jobs.find(j => j.song === qfSong || j.song.startsWith(qfSong));
        if (!job) return qf;
        const statusChanged = qf.status !== job.status;
        const checked = statusChanged && !qf.userTouched
          ? getDefaultChecked(job.status)
          : qf.checked;
        return {
          ...qf,
          status: job.status,
          checked,
          progress: job.status === 'done' ? 100 : (job.progress ?? 0),
          current_step: job.current_step,
          total_steps: job.total_steps,
          step_name: job.step_name,
          errorMsg: job.status === 'error' ? (job.failure_details?.error || job.error || qf.errorMsg) : qf.errorMsg,
        };
      });

      // Disk is the source of truth: refresh rows before deciding completion.
      syncQueueFileStatusFromDisk();

      const completion = decideCompletion({
        jobs,
        queueFiles,
        settledTicks,
        graceStartTime: settledGraceStart,
        requiredSettledTicks: REQUIRED_SETTLED_TICKS,
        gracePeriodMs: SETTLED_GRACE_MS,
        now: Date.now(),
      });
      settledTicks = completion.settledTicks;
      settledGraceStart = completion.graceStartTime;

      const hasPending = jobs.some(j => j.status === 'waiting' || j.status === 'processing' || j.status === 'blocked_no_gpu');

      if (jobs.length === 0) {
        // Backend queue may be transiently empty while jobs start; wait a few ticks
        // before stopping polling so a single empty response doesn't kill the loop.
        emptyQueueTicks++;
        if (emptyQueueTicks >= EMPTY_QUEUE_THRESHOLD) {
          if (queuePollingTimer) {
            clearInterval(queuePollingTimer);
            queuePollingTimer = null;
          }
          resetPipelineUI();
          emptyQueueTicks = 0;
        }
      } else {
        emptyQueueTicks = 0;
      }

      if (completion.confirmed) {
        if (queuePollingTimer) {
          clearInterval(queuePollingTimer);
          queuePollingTimer = null;
        }
        separating = false;
        const hasError = jobs.some(j => j.status === 'error');
        pipelineStatus = hasError ? 'error' : 'done';
        pipelineStep = hasError ? 'Error' : 'Completado';
        currentProgress = hasError ? 0 : 1;
        activeSongNames = new Set();
      } else if (!separating && completion.hasActive) {
        // Real work appeared from outside (another client / API)
        separating = true;
        pipelineStatus = 'running';
      } else if (!completion.hasActive && separating && !completion.settled) {
        // Safeguard: no real work in progress but flag is still set (e.g. after a
        // blocked job is cancelled externally). Reset it on the next tick.
        separating = false;
      }

      // Keep polling alive while there is any non-terminal job (including blocked)
      // or while we are in the post-settlement grace period.
      // The polling loop itself is started by startQueuePolling / handleCancel.
      if (!hasPending && queuePollingTimer && !completion.settled) {
        clearInterval(queuePollingTimer);
        queuePollingTimer = null;
      }

      return completion.hasActive;
    } catch (e) {
      // Keep polling on transient network errors; return true so callers don't think we are done
      return true;
    }
  }

  function startQueuePolling() {
    if (queuePollingTimer) clearInterval(queuePollingTimer);
    // Immediate sync, then keep polling every 500 ms
    syncQueueStatus();
    queuePollingTimer = setInterval(() => {
      syncQueueStatus();
    }, 500);
  }

  // ---- Refresh results from backend (e.g., after pitch shift) ----
  async function handleRefreshResults() {
    try {
      const groups = await getResults();
      const allStems: ResultStem[] = [];
      for (const g of groups) {
        for (const f of g.files) {
          allStems.push({
            name: f.name,
            path: f.path,
            song: g.song,
            stemType: detectStemType(f.name, g.song),
          });
        }
      }
      results = allStems;
      resultGroups = resultsToGroups(results);
    } catch {
      // silently ignore
    }
  }

  // Rebuild the results list from the current authoritative job.files.
  // Stems for any song reported as done are replaced entirely, so
  // intermediate/discarded stems that disappeared from the backend do not
  // become ghosts in the UI.  Songs not present in the queue are left intact
  // (e.g. results loaded from disk on mount).  Mute/solo/volume state is keyed
  // by song/name in the player store and survives the rebuild.
  function rebuildResultsFromJobs(jobs: QueueJob[]) {
    const doneJobs = jobs.filter(j => j.status === 'done' && j.files && j.files.length > 0);
    if (doneJobs.length === 0) return;

    const songsToReplace = new Set(doneJobs.map(j => j.song));
    const keptResults = results.filter(r => !songsToReplace.has(r.song));

    const rebuilt: ResultStem[] = [];
    for (const job of doneJobs) {
      for (const f of job.files!) {
        rebuilt.push({
          name: f.name,
          path: f.path,
          song: job.song,
          stemType: detectStemType(f.name, job.song),
        });
      }
    }

    results = [...keptResults, ...rebuilt];
    resultGroups = resultsToGroups(results);
  }

  // ---- DropZone + FileQueue helpers ----
  function handleDropZoneDragOver(e: DragEvent) {
    e.preventDefault();
  }

  function handleDropZoneDrop(e: DragEvent) {
    e.preventDefault();
    const files = e.dataTransfer?.files;
    if (files && files.length > 0) {
      for (let i = 0; i < files.length; i++) {
        handleDropZoneFile(files[i]);
      }
    }
  }

  function handleDropZoneClick() {
    const input = document.getElementById('dropzone-input') as HTMLInputElement;
    input?.click();
  }

  function handleDropZoneInput(e: Event) {
    const input = e.target as HTMLInputElement;
    if (input.files) {
      handleFilesAdded(Array.from(input.files));
      input.value = '';
    }
  }

  async function handleRemoveQueueFile(id: string) {
    const qf = queueFiles.find((q) => q.id === id);
    if (!qf) return;

    // If the file is already on the server (has a path), delete it physically
    if (qf.path) {
      try {
        await deleteInput(qf.file.name);
        queueFiles = queueFiles.filter((q) => q.id !== id);
      } catch (err: any) {
        showToast('Error al borrar archivo: ' + (err.message || 'unknown'), 'error');
      }
    } else {
      // File was only dragged in but not uploaded yet — just remove from list
      queueFiles = queueFiles.filter((q) => q.id !== id);
    }

    // Stop tracking this song as part of the current batch so it doesn't leave ghost progress
    const removedSong = songNameForQueueFile(qf);
    if (activeSongNames.has(removedSong)) {
      activeSongNames.delete(removedSong);
      activeSongNames = activeSongNames;
    }

    // Sync with backend so the queue view never shows stale / ghost jobs
    await syncQueueStatus();
  }

  function statusBadgeClass(status: string): string {
    switch (status) {
      case 'done': return 'badge badge-green';
      case 'error': return 'badge badge-red';
      case 'processing': return 'badge badge-yellow';
      case 'uploading': return 'badge badge-blue';
      default: return 'badge';
    }
  }

  function getJobForQueueFile(qf: QueueFile): QueueJob | undefined {
    return queueJobs.find(j => {
      // Match by song name (strip extension from qf.file.name)
      const qfSong = qf.file.name.replace(/\.[^.]+$/, '');
      return j.song === qfSong || j.song.startsWith(qfSong);
    });
  }
</script>

<main>
  <div class="app-layout">
    <Sidebar
      activeTab={activeTab}
      collapsed={sidebarCollapsed}
      presets={sidebarPresets}
      ontoggle={() => sidebarCollapsed = !sidebarCollapsed}
      ontabchange={(tab) => {
        activeTab = tab;
        if (isQueueVisible(tab)) {
          queueFiles = applyDefaultChecked(queueFiles);
        }
      }}
    />

    <div class="main-area">
      <header class="app-header">
        <h1>{@html IconOnda} Onda</h1>
        <span class="version">{appVersion || healthVersion || ''}</span>
        {#if gpuType}
          <span class="gpu-label" class:cpu={gpuType === 'cpu'}>
            {#if gpuType === 'cuda'}⚡ CUDA{:else}⚠️ CPU{/if}
          </span>
        {/if}
      </header>

      {#if showCpuWarning}
        <div class="cpu-warning" role="alert">
          <div class="cpu-warning-icon">⚠️</div>
          <div class="cpu-warning-body">
            <strong>Ejecutando en CPU — la separación será mucho más lenta</strong>
            <span>{cpuWarningText}</span>
          </div>
          <button class="cpu-warning-close" onclick={() => cpuWarningDismissed = true} aria-label="Cerrar aviso">✕</button>
        </div>
      {/if}

      <div class="content">
        {#if activeTab === 'settings'}
          <SettingsPanel subtab={settingsSubTab} onsubtabchange={(t) => settingsSubTab = t} onpresetschange={refreshPresets} />
        {:else if activeTab === 'help'}
          <HelpPage />
        {:else if isPresetTab(activeTab)}
          <!-- Built-in preset: dropzone + queue + execute direct + results -->
          <PipelineView
            presetName={activeTab}
            displayName={activeTab}
            {queueFiles}
            {savedPresets}
            {queueJobs}
            {separating}
            {pipelineStatus}
            {currentProgress}
            {pipelineStep}
            {pipelineSong}
            {pipelineEta}
            {inferenceDevice}
            {pipelineModel}
            {pipelineFlags}
            hidePresetSelector={true}
            onError={(msg) => showToast(msg, 'error')}
            onQueueChange={(files) => queueFiles = files}
            onStart={handlePipelineStart}
            onCancel={handleCancel}
            onRemoveFile={handleRemoveQueueFile}
            onViewResult={() => activeTab = 'pitch'}
          />
        {:else if activeTab === 'personalizado'}
          <!-- Personalizado: with preset selector -->
          <PipelineView
            presetName={selectedPresetName}
            displayName={selectedPresetName || 'Personalizado'}
            {queueFiles}
            {savedPresets}
            {queueJobs}
            {separating}
            {pipelineStatus}
            {currentProgress}
            {pipelineStep}
            {pipelineSong}
            {pipelineEta}
            {inferenceDevice}
            {pipelineModel}
            {pipelineFlags}
            hidePresetSelector={false}
            onPresetChange={(name) => selectedPresetName = name}
            onError={(msg) => showToast(msg, 'error')}
            onQueueChange={(files) => queueFiles = files}
            onStart={handlePipelineStart}
            onCancel={handleCancel}
            onRemoveFile={handleRemoveQueueFile}
            onViewResult={() => activeTab = 'pitch'}
          />
        {:else if activeTab === 'pitch'}
          <PitchPage results={results} onResultsChange={handleRefreshResults} />
        {:else if activeTab === 'daw'}
          <DAWWorkspace onError={(msg) => showToast(msg, 'error')} />
        {:else if activeTab === 'midi'}
          <MIDIPage />
        {:else if activeTab === 'spectrogram'}
          <SpectrogramPage />
        {:else if activeTab === 'bpm'}
          <BpmPage onUpload={refreshInputsFromDisk} onNotify={(msg, type) => showToast(msg, type)} />
        {:else if activeTab === 'export'}
          <ExportPage />
        {:else}
          <!-- PipelineView con el preset -->
          <PipelineView
            presetName={activeTab}
            displayName={activeTabName}
            {queueFiles}
            {savedPresets}
            {queueJobs}
            {separating}
            {pipelineStatus}
            {currentProgress}
            {pipelineStep}
            {pipelineSong}
            {pipelineEta}
            {inferenceDevice}
            {pipelineModel}
            {pipelineFlags}
            onQueueChange={(files) => queueFiles = files}
            onStart={handlePipelineStart}
            onCancel={handleCancel}
            onRemoveFile={handleRemoveQueueFile}
            onViewResult={() => activeTab = 'pitch'}
          />
        {/if}
      </div>
    </div>
  </div>

  <!-- Toast -->
  {#if toastMessage}
    <div class="toast {toastType}">{toastMessage}</div>
  {/if}

  <!-- Error Banner -->
  {#if errorBanner}
    <div class="error-banner">
      <span class="error-banner-text">{errorBanner.message}</span>
      <div class="error-banner-actions">
        {#if errorBanner.log}
          <button class="btn-icon" title="Ver log completo" onclick={() => errorLogDetail = errorLogDetail ? null : errorBanner!.log || null}>
            {errorLogDetail ? 'Ocultar log' : 'Ver log'}
          </button>
        {/if}
        <button class="btn-icon" title="Copiar error" onclick={() => copyToClipboard(errorBanner!.message)}>Copiar</button>
        <button class="btn-icon" title="Cerrar" onclick={() => { errorBanner = null; errorLogDetail = null; }}>✕</button>
      </div>
    </div>
    {#if errorLogDetail}
      <pre class="error-log-detail">{errorLogDetail}</pre>
    {/if}
  {/if}
</main>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto,
      Oxygen-Sans, Ubuntu, Cantarell, 'Helvetica Neue', sans-serif;
    min-height: 100vh;

    /* ---- Accent colors (can be changed dynamically) ---- */
    --accent: #6c5ce7;
    --accent-light: #a29bfe;
    --accent-dark: #5a4bd6;
    --accent-glow: rgba(108, 92, 231, 0.3);
    --accent-subtle: rgba(108, 92, 231, 0.08);
    --accent-bg: rgba(108, 92, 231, 0.12);
    --accent-border: rgba(108, 92, 231, 0.2);

    /* ---- Full theme palette (dark theme by default) ---- */
    --bg-primary: #0a0a14;
    --bg-sidebar: #1e1e2a;
    --bg-card: #252535;
    --bg-surface: #1a1a2e;
    --bg-hover: #2a2a3e;
    --bg-active: #3a3a5e;
    --text-primary: #e0e0e0;
    --text-secondary: #888;
    --text-muted: #555;
    --border: #2a2a4a;
    --border-light: #444;
  }

  /* ---- Light theme ---- */
  :global(body.light-theme) {
    --bg-primary: #f0f0f4;
    --bg-sidebar: #ffffff;
    --bg-card: #e8e8ee;
    --bg-surface: #fafafa;
    --bg-hover: #e0e0e0;
    --bg-active: #d0d0dd;
    --text-primary: #222222;
    --text-secondary: #666666;
    --text-muted: #999999;
    --border: #d0d0d0;
    --border-light: #bbbbbb;
  }

  main {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100vh;
    padding: 0;
    gap: 0;
  }

  .app-header h1 {
    margin: 0;
    font-size: 1.2rem;
    font-weight: 700;
    background: linear-gradient(135deg, var(--accent), var(--accent-light));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .version {
    font-size: 0.8rem;
    color: var(--text-muted);
    font-weight: 500;
    letter-spacing: 0.5px;
  }

  .gpu-label {
    font-size: 0.75rem;
    color: var(--accent);
    font-weight: 600;
    padding: 0.15rem 0.5rem;
    border: 1px solid var(--accent-border);
    border-radius: 4px;
    background: var(--accent-subtle);
  }
  .gpu-label.cpu {
    color: #ff9800;
    border-color: rgba(255, 152, 0, 0.3);
    background: rgba(255, 152, 0, 0.1);
  }

  /* CPU Warning Banner */
  .cpu-warning {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.75rem;
    padding: 0.75rem 1.25rem;
    background: rgba(255, 152, 0, 0.14);
    border-bottom: 2px solid rgba(255, 152, 0, 0.35);
    color: #ffb74d;
    flex-shrink: 0;
  }
  .cpu-warning-icon {
    font-size: 1.4rem;
    line-height: 1;
    flex-shrink: 0;
  }
  .cpu-warning-body {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    text-align: center;
  }
  .cpu-warning-body strong {
    font-size: 0.95rem;
    font-weight: 700;
  }
  .cpu-warning-body span {
    font-size: 0.8rem;
    font-weight: 500;
    color: #ffcc80;
  }
  .cpu-warning-close {
    background: rgba(255, 152, 0, 0.15);
    border: 1px solid rgba(255, 152, 0, 0.3);
    color: #ffb74d;
    font-size: 0.9rem;
    padding: 0.2rem 0.55rem;
    border-radius: 4px;
    cursor: pointer;
    line-height: 1;
    flex-shrink: 0;
  }
  .cpu-warning-close:hover {
    background: rgba(255, 152, 0, 0.3);
  }

  .btn-gear {
    margin-left: auto;
    background: none;
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-secondary);
    font-size: 1.1rem;
    padding: 0.25rem 0.5rem;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .btn-gear:hover {
    color: var(--accent);
    border-color: var(--accent);
  }

  /* DropZone */
  .dropzone-section {
    width: 100%;
  }

  .dropzone {
    width: 100%;
    box-sizing: border-box;
    border: 2px dashed var(--border);
    border-radius: 12px;
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
  .dropzone-icon {
    font-size: 2rem;
  }
  .dropzone-text {
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--text-primary);
  }
  .dropzone-hint {
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  /* FileQueue */
  .queue-section {
    width: 100%;
  }
  .queue-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }
  .queue-title {
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--text-primary);
  }
  .btn-clear {
    padding: 0.3rem 0.8rem;
    background: #2a1a1a;
    border: 1px solid #4a2a2a;
    border-radius: 6px;
    color: #e57373;
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
  }
  .btn-clear:hover {
    background: #3a1a1a;
  }
  .queue-list {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }
  .queue-columns-header {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 12px;
    background: rgba(128,128,128,0.08);
    border-bottom: 1px solid var(--border);
    font-size: 11px; font-weight: 600;
    text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--text-secondary);
  }
  .queue-columns-header input[type="checkbox"] {
    flex-shrink: 0; width: 16px; height: 16px;
    cursor: pointer; accent-color: var(--accent);
  }
  .col-title { flex: 1; }
  .col-progress { width: 180px; text-align: center; }
  .col-status { width: 90px; text-align: center; }
  .col-action { width: 32px; }
  .queue-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    font-size: 0.85rem;
  }
  .queue-row input[type="checkbox"] {
    accent-color: var(--accent);
    flex-shrink: 0;
  }
  .queue-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    color: var(--text-primary);
  }
  .queue-step {
    font-size: 0.7rem;
    color: var(--accent-light);
    font-weight: 600;
    flex-shrink: 0;
    white-space: nowrap;
  }
  .queue-progress-bar-wrap {
    width: 60px;
    height: 5px;
    background: var(--bg-primary);
    border-radius: 3px;
    overflow: hidden;
    flex-shrink: 0;
  }
  .queue-progress-bar-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), var(--accent-light));
    border-radius: 3px;
    transition: width 0.3s ease;
  }
  .queue-progress-pct {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-weight: 600;
    flex-shrink: 0;
    min-width: 2.5rem;
    text-align: right;
  }
  .badge {
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
    font-size: 0.65rem;
    font-weight: 700;
    text-transform: uppercase;
    flex-shrink: 0;
    background: #2a2a4a;
    color: var(--text-secondary);
  }
  .badge-green { background: #1b3a1b; color: #81c784; }
  .badge-red { background: #3a1b1b; color: #e57373; }
  .badge-yellow { background: #3a3a1b; color: #ffd54f; }
  .badge-blue { background: #1b2a3a; color: #64b5f6; }
  .btn-remove {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 0.8rem;
    cursor: pointer;
    padding: 0.1rem 0.3rem;
  }
  .btn-remove:hover {
    color: #e57373;
  }

  /* Sections */


  .editor-section {
    width: 100%;
  }



  .progress-section {
    width: 100%;
  }

  .results {
    width: 100%;
  }

  /* Progress card */
  .progress-card {
    width: 100%;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }
  .progress-header {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .progress-status {
    font-weight: 700;
    color: var(--accent-light);
    text-transform: uppercase;
    font-size: 0.8rem;
  }
  .progress-step {
    font-size: 0.8rem;
    color: var(--text-primary);
  }
  .progress-bar-wrap {
    width: 100%;
    height: 8px;
    background: var(--bg-primary);
    border-radius: 4px;
    overflow: hidden;
  }
  .progress-bar-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), var(--accent-light));
    border-radius: 4px;
    transition: width 0.3s ease;
  }
  .progress-meta {
    display: flex;
    gap: 1rem;
    font-size: 0.75rem;
    color: var(--text-secondary);
  }
  .progress-pct {
    font-weight: 700;
    color: var(--accent-light);
  }
  .progress-eta {
    color: #ffb74d;
  }
  .progress-device {
    color: #81c784;
    font-weight: 600;
  }

  /* Toast */
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
  .toast.success {
    background: #4caf50;
  }
  .toast.error {
    background: #f44336;
  }
  @keyframes toastIn {
    from {
      opacity: 0;
      transform: translateX(-50%) translateY(20px);
    }
  }
  @keyframes toastOut {
    to {
      opacity: 0;
      transform: translateX(-50%) translateY(-20px);
    }
  }

  /* Smooth transitions between states */
  section {
    animation: fadeIn 0.3s ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  /* Responsive */
  @media (max-width: 600px) {
    header h1 {
      font-size: 1.5rem;
    }
  }

  /* Error Banner */
  .error-banner {
    position: fixed;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    background: #dc3545;
    color: white;
    padding: 12px 20px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    gap: 16px;
    z-index: 10000;
    max-width: 90vw;
    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
  }
  .error-banner-text {
    flex: 1;
    word-break: break-word;
    max-height: 150px;
    overflow-y: auto;
  }
  .error-banner-actions {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
  }
  .btn-icon {
    background: rgba(255,255,255,0.2);
    border: 1px solid rgba(255,255,255,0.3);
    color: white;
    padding: 6px 10px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 16px;
  }
  .btn-icon:hover {
    background: rgba(255,255,255,0.3);
  }
  .error-log-detail {
    position: fixed;
    bottom: 80px;
    left: 50%;
    transform: translateX(-50%);
    background: #1a1a2e;
    color: #e0e0e0;
    border: 1px solid #2a2a4a;
    border-radius: 8px;
    padding: 16px;
    max-width: 90vw;
    max-height: 40vh;
    overflow: auto;
    white-space: pre-wrap;
    z-index: 9999;
    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
  }

  .btn-refresh {
    background: rgba(255,255,255,0.1);
    border: 1px solid rgba(255,255,255,0.2);
    color: white;
    padding: 6px 10px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 16px;
    transition: background 0.2s;
  }
  .btn-refresh:hover {
    background: rgba(255,255,255,0.25);
  }

  /* Logs Panel */
  .logs-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
  }
  .logs-header h2 { margin: 0; color: var(--text-primary); font-size: 18px; }
  .logs-list {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
  }
  .logs-empty { color: var(--text-secondary); text-align: center; padding: 40px; }
  .log-row {
    display: flex;
    gap: 10px;
    padding: 8px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    border-left: 3px solid transparent;
    margin-bottom: 2px;
  }
  .log-row:hover { background: rgba(128,128,128,0.1); }
  .log-row.log-error { border-left-color: #dc3545; }
  .log-row.log-success { border-left-color: #28a745; }
  .log-row.log-info { border-left-color: #6c757d; }
  .log-time { color: var(--text-secondary); white-space: nowrap; min-width: 140px; font-family: monospace; font-size: 11px; }
  .log-level { flex-shrink: 0; width: 20px; text-align: center; }
  .log-msg { color: var(--text-primary); word-break: break-word; flex: 1; }

  .log-tabs {
    display: flex;
    gap: 4px;
    margin: 0 16px;
  }
  .log-tab {
    background: transparent;
    border: 1px solid var(--border-light);
    color: var(--text-secondary);
    padding: 4px 12px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
  }
  .log-tab.active {
    background: #333;
    color: #fff;
    border-color: var(--text-muted);
  }
  .log-service {
    font-size: 11px;
    min-width: 70px;
    font-weight: bold;
    flex-shrink: 0;
  }

  .log-detail-panel {
    background: #1e1e2e;
    border-radius: 12px;
    width: 90vw;
    max-width: 900px;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 8px 32px rgba(0,0,0,0.5);
  }
  .log-detail-meta {
    display: flex;
    gap: 16px;
    padding: 12px 20px;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
    color: var(--text-secondary);
  }
  .log-detail-level { font-weight: bold; }
  .log-error { color: #dc3545; }
  .log-success { color: #28a745; }
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
/* Fullscreen panels (ModelManager, PresetEditor, Logs) */
.fullscreen {
  position: fixed; top: 0; left: 0; right: 0; bottom: 0;
  background: var(--bg-primary); z-index: 900;
  display: flex; flex-direction: column;
  animation: fadeIn 0.2s ease;
}
.fullscreen-header {
  display: flex; align-items: center; gap: 1rem;
  padding: 0.75rem 1.25rem;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}
.fullscreen-header h2 {
  margin: 0; font-size: 1.1rem; color: var(--text-primary);
  flex: 1; text-align: center;
}
.fullscreen-body {
  flex: 1; overflow-y: auto; padding: 1.25rem;
}

.logs-overlay {
  position: fixed; top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(0,0,0,0.6); z-index: 10000;
  display: flex; align-items: center; justify-content: center;
}
.btn-close {
  background: transparent; border: 1px solid var(--border); color: var(--text-secondary);
  font-size: 18px; width: 32px; height: 32px; border-radius: 6px;
  cursor: pointer; display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.btn-close:hover { background: rgba(255,255,255,0.1); color: #fff; }

  /* ===== New layout styles ===== */
  .app-layout {
    display: flex;
    flex: 1;
    min-height: 0;
    width: 100%;
  }
  .main-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow: hidden;
  }
  .app-header {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    padding: 0.75rem 1.5rem;
    flex-shrink: 0;
    border-bottom: 2px solid transparent;
    border-image: linear-gradient(90deg, var(--accent-glow), var(--accent-subtle)) 1;
  }
  .content {
    flex: 1;
    padding: 1.5rem;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }
</style>
