<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import Sidebar from './lib/Sidebar.svelte';
  import PipelineView from './lib/PipelineView.svelte';
  import PitchPage from './lib/PitchPage.svelte';
  import SettingsPanel from './lib/SettingsPanel.svelte';
  import PlaceholderPage from './lib/PlaceholderPage.svelte';
  import HelpPage from './lib/HelpPage.svelte';
  import ResultsPanel from './lib/ResultsPanel.svelte';
  import PresetsPanel from './lib/PresetsPanel.svelte';
  import type { ResultStem } from './lib/types';
  import { detectStemType } from './lib/types';
  import { separateAudio, uploadAudio, getQueueStatus, getResults, getInputs, deleteInput, getHealth, getPresets, getDefaultPreset, clearQueue, cancelQueue } from './lib/api';
  import type { QueueJob } from './lib/api';
  import { IconOnda, IconStar, IconVoiceRemove, IconSeparate, IconInstruments, IconUser } from './lib/icons';


  interface QueueFile {
    file: File;
    id: string;
    status: string;
    checked: boolean;
    progress?: number;
    path?: string;
    errorMsg?: string;
    current_step?: number;
    total_steps?: number;
    step_name?: string;
  }
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
  let pipelineStatus = $state<'idle'|'running'|'done'|'error'>('idle');
  let pipelineStep = $state('');
  let pipelineSong = $state('');
  let currentProgress = $state(0);
  let pipelineEta = $state('');
  let inferenceDevice = $state('');
  let savedPresets = $state<{name: string, config: any}[]>([]);
  let selectedPresetName = $state('');

  // ---- Queue state ----
  let queueJobs = $state<QueueJob[]>([]);
  let queuePollingTimer: ReturnType<typeof setInterval> | null = null;
  let processedDoneSongs = $state<Set<string>>(new Set());
  let activeSongNames = $state<Set<string>>(new Set()); // songs submitted in current batch

  // ---- Health / Version from backend ----
  let healthVersion = $state('');

  // Toast
  let toastMessage = $state('');
  let toastType = $state<'success' | 'error'>('success');
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Persistent error banner
  let errorBanner = $state<{ message: string } | null>(null);

  // ---- New layout state ----
  let activeTab = $state('personalizado');
  let sidebarCollapsed = $state(false);
  let settingsSubTab = $state('models');
  let activeTabName = $derived(activeTab);

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

  // Load model list + persisted data on mount
  onMount(() => {
    // ── Load persisted accent color and theme ──
    const savedAccent = localStorage.getItem('onda-accent');
    if (savedAccent) {
      const body = document.body;
      body.style.setProperty('--accent', savedAccent);
      // Calculate lighter/darker variants
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

    // ── Load persisted font size ──
    const savedFontSize = localStorage.getItem('onda-font-size');
    if (savedFontSize) {
      const root = document.documentElement;
      const sizes = { small: '12px', medium: '14px', large: '16px' };
      root.style.fontSize = sizes[savedFontSize as keyof typeof sizes] || '14px';
    }

    // ── Load persisted UI scale ──
    const savedScale = localStorage.getItem('onda-scale');
    if (savedScale) {
      document.body.style.zoom = `${savedScale}%`;
    }

    // ── Load version from health endpoint ──
    getHealth()
      .then((h) => {
        if (h?.version) healthVersion = h.version;
      })
      .catch(() => {}); // silent fail

    // ── Load persisted results from filesystem (/output/) ──
    getResults()
      .then((groups) => {
        console.log('getResults response:', groups.length, 'songs');
        if (groups.length > 0) {
          const loadedResults: ResultStem[] = [];
          for (const group of groups) {
            for (const f of group.files) {
              loadedResults.push({
                name: f.name,
                path: f.path,
                song: group.song,
                stemType: detectStemType(f.name),
              });
            }
          }
          results = loadedResults;
          pipelineStatus = 'done';
          currentProgress = 1;
          console.log('Loaded existing results from filesystem:', results.length, 'stems');
        }
      })
      .catch((err) => {
        console.error('Failed to load results from filesystem:', err);
      });

    // ── Load persisted inputs from filesystem (/input/) ──
    getInputs()
      .then((inputs) => {
        console.log('getInputs response:', inputs.length, 'files');
        if (inputs.length > 0) {
          // Create QueueFile entries for pre-existing input files
          const existingPaths = new Set(queueFiles.map(q => q.path));
          const newQueueFiles: QueueFile[] = [];
          for (const input of inputs) {
            // Avoid duplicates if files were already added via dropzone
            if (!existingPaths.has(input.path)) {
              newQueueFiles.push({
                file: new File([], input.name), // placeholder File (name-only)
                id: crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}-${input.name}`,
                status: 'waiting',
                checked: true,
                path: input.path,
              });
            }
          }
          if (newQueueFiles.length > 0) {
            queueFiles = [...queueFiles, ...newQueueFiles];
            console.log('Restored', newQueueFiles.length, 'inputs from filesystem');
          }
        }
      })
      .catch((err) => {
        console.error('Failed to load inputs from filesystem:', err);
      });

    // ── Restore active queue jobs ──
    getQueueStatus()
      .then((status) => {
        queueJobs = status.jobs || [];
        // Restore results for already-done jobs
        const activeJobs = status.jobs?.filter(j => j.status !== 'done' && j.status !== 'error') || [];
        if (activeJobs.length > 0) {
          console.log('Restoring', activeJobs.length, 'active queue jobs');
          separating = true;
          pipelineStatus = 'running';
          startQueuePolling();
        }
        // Also accumulate results from any done jobs in the queue
        for (const job of (status.jobs || [])) {
          if (job.status === 'done' && job.files && job.files.length > 0) {
            const alreadyLoaded = new Set(results.map(r => `${r.song}/${r.name}`));
            const newResults: ResultStem[] = job.files
              .filter((f: any) => !alreadyLoaded.has(`${job.song}/${f.name}`))
              .map((f: any) => ({
                name: f.name,
                path: f.path,
                song: job.song,
                stemType: detectStemType(f.name),
              }));
            if (newResults.length > 0) {
              results = [...results, ...newResults];
            }
          }
        }
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
  });

  // Cleanup timers on unmount
  onDestroy(() => {
    if (queuePollingTimer) clearInterval(queuePollingTimer);
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
        checked: true,
      };
      queueFiles = [...queueFiles, qf];
      try {
        const res = await uploadAudio(f);
        queueFiles = queueFiles.map(q =>
          q.id === id ? { ...q, status: 'waiting', path: res.path } : q
        );
      } catch (err: any) {
        queueFiles = queueFiles.map(q =>
          q.id === id ? { ...q, status: 'error', errorMsg: err.message || 'Upload failed' } : q
        );
      }
    }
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
    queueFiles = queueFiles.map((qf) =>
      qf.id === id ? { ...qf, checked: !qf.checked } : qf,
    );
  }

  function handleToggleAll() {
    const allChecked = queueFiles.every(qf => qf.checked);
    queueFiles = queueFiles.map(qf => ({ ...qf, checked: !allChecked }));
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

    const checked = queueFiles.filter((qf) => qf.checked && qf.status !== 'done');
    if (checked.length === 0) {
      if (queueFiles.length > 0) {
        showToast('✅ Marca al menos un archivo en la cola', 'success');
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

      // Enqueue each uploaded file via separateAudio
      for (const { qf, path } of uploaded) {
        // Track song name for total progress
        const songName = path.split('/').pop()?.replace(/\.[^.]+$/, '') || '';
        activeSongNames.add(songName);
        try {
          const opts: any = {
            preset,
            input: path,
          };
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

  function handleCancel() {
    cancelQueue().catch(() => {});
    if (queuePollingTimer) {
      clearInterval(queuePollingTimer);
      queuePollingTimer = null;
    }
    separating = false;
    pipelineStatus = 'idle';
    pipelineStep = '';
    currentProgress = 0;
    pipelineEta = '';
    inferenceDevice = '';
    queueJobs = [];
    processedDoneSongs = new Set();
    activeSongNames = new Set();
    // Reset queue files so they can be re-processed cleanly
    queueFiles = queueFiles.map(qf => ({ ...qf, status: 'waiting', progress: 0, errorMsg: undefined }));
    showToast('⏹ Proceso cancelado', 'success');
  }

  function startQueuePolling() {
    if (queuePollingTimer) clearInterval(queuePollingTimer);
    pollStartTime = Date.now();

    queuePollingTimer = setInterval(async () => {
      try {
        const status = await getQueueStatus();
        queueJobs = status.jobs || [];

        // Update progress UI from processing job
        const processingJob = queueJobs.find(j => j.status === 'processing');
        if (processingJob) {
          pipelineSong = processingJob.song;
          pipelineStep = processingJob.step_name || 'processing';
          if (processingJob.eta) pipelineEta = processingJob.eta;
          if (processingJob.device) inferenceDevice = processingJob.device;
        }

        // Calculate total progress across ALL submitted songs in this batch
        if (queueJobs.length > 0 && activeSongNames.size > 0) {
          let totalSteps = 0;
          let completedSteps = 0;
          for (const job of queueJobs) {
            // Only count jobs from the current batch
            if (!activeSongNames.has(job.song)) continue;
            const steps = job.total_steps || 1;
            totalSteps += steps;
            if (job.status === 'done') {
              completedSteps += steps;
            } else if (job.status === 'processing') {
              completedSteps += (job.current_step || 1) - 1 + (job.progress || 0) / 100;
            }
            // waiting/error jobs contribute 0
          }
          if (totalSteps > 0) {
            currentProgress = completedSteps / totalSteps;
          }
        }

        // Check for newly done jobs → accumulate results
        for (const job of queueJobs) {
          if (job.status === 'done' && !processedDoneSongs.has(job.song)) {
            processedDoneSongs.add(job.song);
            if (job.files && job.files.length > 0) {
              const newResults: ResultStem[] = job.files.map((f: any) => ({
                name: f.name,
                path: f.path,
                song: job.song,
                stemType: detectStemType(f.name),
              }));
              results = [...results, ...newResults]; // accumulate, don't replace
            }
          }
          if (job.status === 'error' && job.error && !processedDoneSongs.has(job.song)) {
            processedDoneSongs.add(job.song);
            showToast(`Error en "${job.song}": ${job.error.slice(0, 200)}`, 'error');
          }
        }

        // Update queue file statuses to match job states (including per-track progress)
        for (const job of queueJobs) {
          const qf = queueFiles.find(
            (q) => q.path && q.path.includes(job.song.replace(/\.[^.]+$/, '')),
          );
          if (qf) {
            qf.status = job.status;
            qf.progress = job.status === 'done' ? 100 : (job.progress ?? 0);
            qf.current_step = job.current_step;
            qf.total_steps = job.total_steps;
            qf.step_name = job.step_name;
          }
        }

        // All jobs done or errored → stop polling
        const allSettled = queueJobs.every(
          (j) => j.status === 'done' || j.status === 'error',
        );
        if (allSettled && queueJobs.length > 0) {
          clearInterval(queuePollingTimer!);
          queuePollingTimer = null;
          separating = false;
          pipelineStatus = queueJobs.some((j) => j.status === 'error') ? 'error' : 'done';
          pipelineStep = queueJobs.some((j) => j.status === 'error') ? 'Error' : 'Completado';
          currentProgress = queueJobs.some((j) => j.status === 'error') ? 0 : 1;
        }

        // Timeout: if queue is empty but we expected jobs, show error after 10s
        if (queueJobs.length === 0 && activeSongNames.size > 0 && queuePollingTimer) {
          const elapsed = Date.now() - pollStartTime;
          if (elapsed > 10000) {
            clearInterval(queuePollingTimer);
            queuePollingTimer = null;
            separating = false;
            pipelineStatus = 'error';
            pipelineStep = 'Error al encolar';
            currentProgress = 0;
            showToast('Error: Los trabajos no se encolaron correctamente', 'error');
          }
        }
      } catch (e) {
        // Silently keep polling on transient network errors
      }
    }, 500);
  }

  let pollStartTime = 0;

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
            stemType: detectStemType(f.name),
          });
        }
      }
      results = allStems;
    } catch {
      // silently ignore
    }
  }

  // ---- ResultsPanel delete callbacks ---- 
  function handleStemDeleted(_song: string, _name: string, path: string) {
    results = results.filter(s => s.path !== path);
  }

  function handleGroupDeleted(song: string) {
    results = results.filter(s => s.song !== song);
    // If no results left, reset pipeline status
    if (results.length === 0) {
      pipelineStatus = 'idle';
    }
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
      ontabchange={(tab) => activeTab = tab}
    />

    <div class="main-area">
      <header class="app-header">
        <h1>{@html IconOnda} Onda</h1>
        <span class="version">{healthVersion || ''}</span>
      </header>

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
            {separating}
            {pipelineStatus}
            {currentProgress}
            {pipelineStep}
            {pipelineSong}
            {pipelineEta}
            {inferenceDevice}
            hidePresetSelector={true}
            onError={(msg) => showToast(msg, 'error')}
            onQueueChange={(files) => queueFiles = files}
            onStart={handlePipelineStart}
            onCancel={handleCancel}
            onRemoveFile={handleRemoveQueueFile}
            onViewResult={() => activeTab = 'results'}
          />
        {:else if activeTab === 'personalizado'}
          <!-- Personalizado: with preset selector -->
          <PipelineView
            presetName={selectedPresetName}
            displayName={selectedPresetName || 'Personalizado'}
            {queueFiles}
            {savedPresets}
            {separating}
            {pipelineStatus}
            {currentProgress}
            {pipelineStep}
            {pipelineSong}
            {pipelineEta}
            {inferenceDevice}
            hidePresetSelector={false}
            onPresetChange={(name) => selectedPresetName = name}
            onError={(msg) => showToast(msg, 'error')}
            onQueueChange={(files) => queueFiles = files}
            onStart={handlePipelineStart}
            onCancel={handleCancel}
            onRemoveFile={handleRemoveQueueFile}
            onViewResult={() => activeTab = 'results'}
          />
        {:else if activeTab === 'pitch'}
          <PitchPage results={results} onResultsChange={handleRefreshResults} />
        {:else if ['bpm', 'daw'].includes(activeTab)}
          <PlaceholderPage tabId={activeTab} />
        {:else if activeTab === 'results'}
          <!-- ResultsPanel -->
          <section class="results">
            <ResultsPanel files={results} onstemdeleted={handleStemDeleted} ongroupdeleted={handleGroupDeleted} />
          </section>
        {:else}
          <!-- PipelineView con el preset -->
          <PipelineView
            presetName={activeTab}
            displayName={activeTabName}
            {queueFiles}
            {savedPresets}
            {separating}
            {pipelineStatus}
            {currentProgress}
            {pipelineStep}
            {pipelineSong}
            {pipelineEta}
            {inferenceDevice}
            onQueueChange={(files) => queueFiles = files}
            onStart={handlePipelineStart}
            onCancel={handleCancel}
            onRemoveFile={handleRemoveQueueFile}
            onViewResult={() => activeTab = 'results'}
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
        <button class="btn-icon" title="Copiar error" onclick={() => copyToClipboard(errorBanner!.message)}>Copiar</button>
        <button class="btn-icon" title="Cerrar" onclick={() => errorBanner = null}>✕</button>
      </div>
    </div>
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
