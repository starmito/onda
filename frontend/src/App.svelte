<script lang="ts">
  import DropZone from './lib/DropZone.svelte';
  import PipelineConfig from './lib/PipelineConfig.svelte';
  import type { PipelineConfig as PipelineConfigType } from './lib/PipelineConfig.svelte';
  import PitchControl from './lib/PitchControl.svelte';
  import FileQueue from './lib/FileQueue.svelte';
  import type { QueueFile } from './lib/FileQueue.svelte';
  import ProgressBar from './lib/ProgressBar.svelte';
  import ResultsPanel from './lib/ResultsPanel.svelte';
  import type { ResultStem } from './lib/ResultsPanel.svelte';
  import HealthBar from './lib/HealthBar.svelte';
  import BackendControls from './lib/BackendControls.svelte';
  import PresetSelector from './lib/PresetSelector.svelte';
  import ModelConfig from './lib/ModelConfig.svelte';
  import GpuMonitor from './lib/GpuMonitor.svelte';
  import VramCalculator from './lib/VramCalculator.svelte';
  import type { Preset } from './lib/VramCalculator.svelte';
  import ModelLoader from './lib/ModelLoader.svelte';
  import { getModels, separateAudio, getStatus, uploadAudio, getModelList } from './lib/api';
  import type { ModelInfo } from './lib/api';

  // ---- State ----
  let queueFiles = $state<QueueFile[]>([]);
  let presets = $state<Record<string, any>>({});
  let separating = $state(false);
  let results = $state<ResultStem[]>([]);
  let progressRef = $state<any>(null);
  let modelsError = $state(false);
  let pitchValue = $state(0);

  // Advanced model config
  let modelConfig = $state({
    vocalModel: '',
    stemModel: '',
    drumsModel: '',
    bassModel: '',
    otherModel: '',
    vocalOverlap: 4,
  });
  let modelInfos = $state<ModelInfo[]>([]);

  // VramCalculator integration
  let selectedPresetKey = $state('');
  let selectedPresetData = $derived.by((): Preset | null => {
    if (!selectedPresetKey || !presets[selectedPresetKey]) return null;
    const p = presets[selectedPresetKey];
    return {
      key: selectedPresetKey,
      name: p.name,
      description: p.description,
      models: {
        vocal: modelConfig.vocalModel,
        stems: modelConfig.stemModel,
        drums: modelConfig.drumsModel,
        bass: modelConfig.bassModel,
        other: modelConfig.otherModel,
      },
    };
  });

  // Load presets + model list on mount
  $effect(() => {
    getModels()
      .then((p) => (presets = p))
      .catch(() => {
        modelsError = true;
      });
    getModelList()
      .then((m) => (modelInfos = m))
      .catch(() => {}); // silent fail — dropdowns just stay empty
  });

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
    handlePipelineStart({
      viperx: false,
      viperxKeep: 'both',
      demucs: true,
      demucsKeep: ['drums', 'bass', 'other', 'vocals'],
    });
  }

  // ---- Pipeline start ----
  async function handlePipelineStart(config: PipelineConfigType) {
    const checked = queueFiles.filter((qf) => qf.checked && qf.status !== 'done');
    if (checked.length === 0) {
      alert('No checked files in queue.');
      return;
    }

    separating = true;
    results = [];

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

      // Use the first preset (htdemucs) as default, or the first available
      const presetKeys = Object.keys(presets);
      const preset = presetKeys.length > 0 ? presetKeys[0] : 'htdemucs';

      // Start separation for each uploaded file
      for (const { qf, path } of uploaded) {
        try {
          progressRef?.start();
          await separateAudio({
            preset,
            input: path,
            pitch: pitchValue !== 0 ? pitchValue : undefined,
            viperx: config.viperx,
            viperx_keep: config.viperxKeep,
            demucs: config.demucs,
            demucs_keep: config.demucsKeep,
          });
          qf.status = 'done';
          qf.progress = 1;
        } catch (err: any) {
          qf.status = 'error';
          qf.errorMsg = err.message;
        }
      }
    } catch (err: any) {
      alert('Pipeline error: ' + err.message);
    } finally {
      separating = false;
      // Fetch results
      handleComplete();
    }
  }

  function handleComplete() {
    getStatus()
      .then((status) => {
        if (status.files && status.files.length > 0) {
          results = status.files.map((f) => ({
            name: f.name,
            path: f.path,
            song: status.song || extractSongFromName(f.name),
          }));
        } else {
          // Fallback from queue
          const doneFiles = queueFiles.filter((qf) => qf.status === 'done' && qf.path);
          results = doneFiles.map((qf) => ({
            name: qf.file.name.replace(/\.[^.]+$/, '') + '_vocals.wav',
            path: qf.path || '',
            song: qf.file.name.replace(/\.[^.]+$/, ''),
          }));
        }
      })
      .catch(console.error);
  }

  function extractSongFromName(name: string): string {
    return name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, '');
  }
</script>

<main>
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
        selectedPresetKey = key;
      }}
      modelsError={modelsError}
    />
    <ModelConfig
      models={modelInfos}
      config={modelConfig}
      onchange={(cfg) => (modelConfig = cfg)}
    />
    <VramCalculator preset={selectedPresetData} />
    <PipelineConfig disabled={separating} onstart={handlePipelineStart} />
    <PitchControl value={pitchValue} disabled={separating} onapply={handlePitchApply} />
  </section>

  {#if separating}
    <section class="progress">
      <ProgressBar oncomplete={handleComplete} bind:this={progressRef} />
    </section>
  {/if}

  {#if results.length > 0}
    <section class="results">
      <ResultsPanel files={results} />
    </section>
  {/if}

  <section class="model-loader">
    <ModelLoader />
  </section>
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

  .model-loader {
    width: 100%;
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
