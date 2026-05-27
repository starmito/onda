<script lang="ts">
  import DropZone from './lib/DropZone.svelte';
  import PipelineConfig from './lib/PipelineConfig.svelte';
  import type { PipelineConfig as PipelineConfigType } from './lib/PipelineConfig.svelte';
  import PitchControl from './lib/PitchControl.svelte';
  import FileQueue from './lib/FileQueue.svelte';
  import type { QueueFile } from './lib/FileQueue.svelte';
  import ProgressBar from './lib/ProgressBar.svelte';
  import ResultsPanel from './lib/ResultsPanel.svelte';
  import type { ResultStem } from './lib/types';
  import { detectStemType } from './lib/types';
  import HealthBar from './lib/HealthBar.svelte';
  import BackendControls from './lib/BackendControls.svelte';
  import PresetSelector from './lib/PresetSelector.svelte';
  import GpuMonitor from './lib/GpuMonitor.svelte';
  import ModelConfigScreen from './lib/ModelConfigScreen.svelte';
  import { getModels, separateAudio, getStatus, uploadAudio, getLocalModels } from './lib/api';
  import type { LocalModel, StatusResponse } from './lib/api';

  // ---- State ----
  let queueFiles = $state<QueueFile[]>([]);
  let presets = $state<Record<string, any>>({});
  let separating = $state(false);
  let results = $state<ResultStem[]>([]);
  let modelsError = $state(false);
  let pitchValue = $state(0);
  let pipelineStatus = $state<'idle'|'running'|'done'|'error'>('idle');
  let pipelineStep = $state('');
  let pipelineSong = $state('');
  let pipelineEta = $state(0);
  let currentProgress = $state(0);
  let pipelineError = $state('');
  let pipelineModel = $state('');
  let pollingTimer: ReturnType<typeof setInterval> | null = null;

  // Toast
  let toastMessage = $state('');
  let toastType = $state<'success' | 'error'>('success');
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  function showToast(message: string, type: 'success' | 'error') {
    toastMessage = message;
    toastType = type;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toastMessage = '';
    }, 3000);
  }

  // Advanced model config
  let modelConfig = $state({
    vocalModel: '',
    stemModel: '',
    drumsModel: '',
    bassModel: '',
    otherModel: '',
    vocalOverlap: 4,
  });
  let modelInfos = $state<LocalModel[]>([]);
  let showModelConfig = $state(false);

  // Load presets + model list on mount
  $effect(() => {
    getModels()
      .then((p) => (presets = p))
      .catch(() => {
        modelsError = true;
      });
    getLocalModels()
      .then((res) => (modelInfos = res.models || []))
      .catch(() => {}); // silent fail — dropdowns just stay empty

    // Cargar resultados existentes al iniciar (separación previa completada)
    getStatus()
      .then((status) => {
        console.log('getStatus response:', status.status, status.files?.length);
        if (
          status.status === 'done' &&
          status.files &&
          status.files.length > 0
        ) {
          loadResults(status);
          pipelineStep = 'completado';
          currentProgress = 1;
          console.log('Loaded existing results:', results.length, results);
        } else {
          console.log('No results to load - status:', status.status, 'files:', status.files?.length);
        }
      })
      .catch((err) => {
        console.error('Failed to load existing results:', err);
      });
  });

  // Debug: trace results reactivity for production diagnosis
  $inspect('results changed:', results.length, results);

  // ---- File Queue handlers ----
  function handleFilesAdded(newFiles: File[]) {
    const newItems: QueueFile[] = newFiles.map((f) => ({
      file: f,
      id: crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`,
      status: 'waiting',
      checked: true,
    }));
    queueFiles = [...queueFiles, ...newItems];
  }

  function handleDropZoneFile(f: File) {
    handleFilesAdded([f]);
  }

  function handleClearQueue() {
    queueFiles = [];
    results = [];
    showToast('Cola limpiada', 'success');
  }

  function handleToggleQueueFile(id: string) {
    queueFiles = queueFiles.map((qf) =>
      qf.id === id ? { ...qf, checked: !qf.checked } : qf,
    );
  }

  function handlePitchApply(pitch: number) {
    pitchValue = pitch;
  }

  // ---- Preset start (from PresetSelector) ----
  function handlePresetStart(preset: string) {
    const p = presets[preset];
    if (!p) {
      alert('Preset not found: ' + preset);
      return;
    }
    handlePipelineStart({
      preset,
      viperx: !!p.vocalModel,
      viperxKeep: 'both',
      demucs: !!p.stemModel,
      demucsKeep: ['drums', 'bass', 'other', 'vocals'],
    });
  }

  // ---- Per-step handlers (PipelineConfig individual buttons) ----
  async function handleViperxOnly(config: PipelineConfigType) {
    await handlePipelineStart({ ...config, demucs: false });
    // Force refresh after pipeline completes
    setTimeout(async () => {
      try {
        const status = await getStatus();
        if (status.status === 'done' && status.files && status.files.length > 0) {
          loadResults(status);
        }
      } catch { /* ignore transient errors */ }
    }, 5000);
  }

  async function handleDemucsOnly(config: PipelineConfigType) {
    await handlePipelineStart({ ...config, viperx: false });
    // Force refresh after pipeline completes
    setTimeout(async () => {
      try {
        const status = await getStatus();
        if (status.status === 'done' && status.files && status.files.length > 0) {
          loadResults(status);
        }
      } catch { /* ignore transient errors */ }
    }, 5000);
  }

  // ---- Pipeline start ----
  async function handlePipelineStart(config: PipelineConfigType) {
    // Clear any existing polling
    if (pollingTimer) {
      clearInterval(pollingTimer);
      pollingTimer = null;
    }

    const checked = queueFiles.filter((qf) => qf.checked && qf.status !== 'done');
    if (checked.length === 0) {
      alert('No checked files in queue.');
      return;
    }

    separating = true;
    results = [];
    pipelineStatus = 'idle';
    pipelineStep = '';
    pipelineError = '';
    currentProgress = 0;
    pipelineEta = 0;

    // Mark checked files as uploading
    for (const qf of checked) {
      qf.status = 'uploading';
      qf.progress = 0;
    }

    try {
      // Upload all checked files
      const uploaded: { qf: QueueFile; path: string }[] = [];
      for (const qf of checked) {
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
        alert('No files uploaded successfully.');
        return;
      }

      // Use the preset passed from PresetSelector, or fall back to 'balance'
      const preset = config.preset || (Object.keys(presets).length > 0 ? 'balance' : 'htdemucs');

      // Start separation for each uploaded file
      for (const { qf, path } of uploaded) {
        try {
          qf.status = 'processing';
          qf.progress = 0;

          await separateAudio({
            preset,
            input: path,
            pitch: pitchValue !== 0 ? pitchValue : undefined,
            viperx: config.viperx,
            viperx_keep: config.viperxKeep,
            demucs: config.demucs,
            demucs_keep: config.demucsKeep,
          });
        } catch (err: any) {
          qf.status = 'error';
          qf.errorMsg = err.message;
        }
      }

      // Start polling /api/status every 500ms
      pipelineStatus = 'running';
      pipelineSong = uploaded[0]?.qf.file.name || '';

      pollingTimer = setInterval(async () => {
        try {
          const status = await getStatus();
          pipelineStep = status.step || '';
          pipelineModel = status.vocal_model || status.stem_model || '';
          pipelineEta = status.eta || 0;
          currentProgress = status.progress || 0;

          // Update progress on the first uploaded file
          if (uploaded.length > 0) {
            uploaded[0].qf.progress = status.progress || 0;
          }

          if (status.status === 'done') {
            pipelineStatus = 'done';
            clearInterval(pollingTimer!);
            pollingTimer = null;

            loadResults(status);
            separating = false;
            // Marcar archivos de la cola como done
            if (uploaded.length > 0) {
              for (const uf of uploaded) {
                uf.qf.status = 'done';
                uf.qf.progress = 1;
              }
            }
          } else if (status.status === 'error') {
            pipelineStatus = 'error';
            pipelineError = status.error || 'Unknown error';
            clearInterval(pollingTimer!);
            pollingTimer = null;
            separating = false;
            if (uploaded.length > 0) {
              uploaded[0].qf.status = 'error';
              uploaded[0].qf.errorMsg = status.error || 'Unknown error';
            }
          }
        } catch (e) {
          // keep polling on transient network errors
        }
      }, 500);
    } catch (err: any) {
      alert('Pipeline error: ' + err.message);
      separating = false;
    }
  }

  function extractSongFromName(name: string): string {
    return name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, '');
  }

  function loadResults(status: StatusResponse) {
    if (status.files && status.files.length > 0) {
      const newResults: ResultStem[] = status.files.map((f: any) => ({
        name: f.name,
        path: f.path,
        song: status.song || extractSongFromName(f.name),
        stemType: detectStemType(f.name),
      }));
      results = [...newResults];
      pipelineStatus = 'done';
    }
  }

  // ---- ResultsPanel delete callbacks ---- 
  async function handleStemDeleted() {
    try {
      const status = await getStatus();
      if (status.files && status.files.length > 0) {
        const newResults: ResultStem[] = status.files.map((f: any) => ({
          name: f.name,
          path: f.path,
          song: status.song || f.name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, ''),
          stemType: detectStemType(f.name),
        }));
        results = [...newResults];
      } else {
        results = [];
        pipelineStatus = 'idle';
      }
    } catch (err) {
      console.error('Failed to refresh results after stem delete:', err);
    }
  }

  async function handleGroupDeleted() {
    try {
      const status = await getStatus();
      if (status.files && status.files.length > 0) {
        const newResults: ResultStem[] = status.files.map((f: any) => ({
          name: f.name,
          path: f.path,
          song: status.song || f.name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, ''),
          stemType: detectStemType(f.name),
        }));
        results = [...newResults];
      } else {
        results = [];
        pipelineStatus = 'idle';
      }
    } catch (err) {
      console.error('Failed to refresh results after group delete:', err);
    }
  }
</script>

<main>
  {#if showModelConfig}
    <ModelConfigScreen
      modelInfos={modelInfos}
      onclose={() => (showModelConfig = false)}
    />
  {:else}
  <header>
    <h1>🎵 Onda</h1>
    <span class="version">v2.0.0-alpha</span>
    <div class="header-right">
      <GpuMonitor />
      <HealthBar />
      <BackendControls />
    </div>
  </header>

  <section class="upload">
    <DropZone onfile={handleDropZoneFile} />
  </section>

  {#if queueFiles.length > 0}
    <section class="queue-section">
      <FileQueue
        files={queueFiles}
        disabled={separating}
        overallProgress={currentProgress}
        overallEta={pipelineEta}
        onaddfiles={handleFilesAdded}
        onclear={handleClearQueue}
        ontoggle={handleToggleQueueFile}
      />
    </section>
  {/if}

  <section class="controls">
    <PresetSelector
      presets={presets}
      disabled={separating}
      onseparate={(preset: string) => {
        handlePresetStart(preset);
      }}
      onselect={(key: string) => {
        // track selected preset for downstream use
        console.debug('Preset selected:', key);
      }}
      modelsError={modelsError}
    />
    <button
      class="advanced-config-btn"
      onclick={() => (showModelConfig = true)}
    >
      ⚙️ Configuración avanzada
    </button>
    <PipelineConfig
      disabled={separating}
      onstart={handlePipelineStart}
      onviperxonly={handleViperxOnly}
      ondemucsonly={handleDemucsOnly}
    />
    <PitchControl value={pitchValue} disabled={separating} onapply={handlePitchApply} />
  </section>

  {#if pipelineStatus !== 'idle'}
    <section class="progress">
      <ProgressBar status={pipelineStatus} step={pipelineStep} model={pipelineModel} progress={currentProgress} error={pipelineError} />
    </section>
  {/if}

  {#if results.length > 0}
    <section class="results">
      <ResultsPanel files={results} onstemdeleted={handleStemDeleted} ongroupdeleted={handleGroupDeleted} />
    </section>
  {/if}
  {/if}

  {#if toastMessage}
    <div class="toast {toastType}">{toastMessage}</div>
  {/if}
</main>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    background: #0a0a14;
    color: #e0e0e0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto,
      Oxygen-Sans, Ubuntu, Cantarell, 'Helvetica Neue', sans-serif;
    min-height: 100vh;
  }

  main {
    display: flex;
    flex-direction: column;
    align-items: center;
    max-width: 800px;
    margin: 0 auto;
    padding: 2rem 1.5rem 4rem;
    gap: 1.5rem;
  }

  header {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    padding: 0.75rem 0 0.5rem;
    width: 100%;
    border-bottom: 2px solid transparent;
    border-image: linear-gradient(
        90deg,
        rgba(0, 212, 255, 0.3),
        rgba(0, 212, 255, 0.05)
      )
      1;
  }

  header h1 {
    margin: 0;
    font-size: 1.75rem;
    font-weight: 700;
    background: linear-gradient(135deg, #00d4ff, #b388ff);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .version {
    font-size: 0.8rem;
    color: #555;
    font-weight: 500;
    letter-spacing: 0.5px;
  }

  .header-right {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .upload {
    width: 100%;
  }

  .queue-section {
    width: 100%;
  }

  .controls {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .progress {
    width: 100%;
  }

  .results {
    width: 100%;
  }

  .advanced-config-btn {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    background: #1a1a2e;
    border: none;
    border-radius: 8px;
    color: #e0e0e0;
    font-size: 0.95rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
  }
  .advanced-config-btn:hover {
    background: #22223a;
  }

  /* Toast */
  .toast {
    position: fixed;
    bottom: 20px;
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
    from {
      opacity: 0;
      transform: translateY(8px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  /* Responsive */
  @media (max-width: 600px) {
    main {
      padding: 1rem 1rem 3rem;
      gap: 1rem;
    }

    header h1 {
      font-size: 1.5rem;
    }
  }
</style>
