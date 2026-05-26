<script lang="ts">
  import DropZone from './lib/DropZone.svelte';
  import PresetSelector from './lib/PresetSelector.svelte';
  import ProgressBar from './lib/ProgressBar.svelte';
  import ResultsList from './lib/ResultsList.svelte';
  import { getModels, separateAudio, getStatus, uploadAudio } from './lib/api';

  let file = $state<File | null>(null);
  let presets = $state<Record<string, any>>({});
  let separating = $state(false);
  let results = $state<{ name: string; path: string }[]>([]);
  let progressRef = $state<any>(null);
  let modelsError = $state(false);

  // Cargar presets al montar
  $effect(() => {
    getModels()
      .then((p) => (presets = p))
      .catch(() => {
        modelsError = true;
      });
  });

  function handleFile(f: File) {
    file = f;
    results = [];
  }

  async function handleSeparate(preset: string) {
    if (!file) return;
    separating = true;
    results = [];

    try {
      // 1. Upload file first
      const uploaded = await uploadAudio(file);
      // 2. Start separation with the server-side path
      await separateAudio(preset, uploaded.path);
      progressRef?.start();
    } catch (err: any) {
      alert('Error: ' + err.message);
      separating = false;
    }
  }

  function handleComplete() {
    separating = false;

    getStatus()
      .then((status) => {
        if (status.files && status.files.length > 0) {
          results = status.files;
        } else if (file) {
          // Fallback if backend didn't return files
          const base = file.name.replace(/\.[^.]+$/, '');
          results = [
            { name: `${base}_vocals.wav`, path: '' },
            { name: `${base}_instrumental.wav`, path: '' },
          ];
        }
      })
      .catch(console.error);
  }
</script>

<main>
  <header>
    <h1>🎵 Onda</h1>
    <span class="version">v2.0.0-alpha</span>
  </header>

  <section class="upload">
    <DropZone onfile={handleFile} />
  </section>

  {#if file}
    <section class="controls">
      <p class="file-name">📁 {file.name}</p>
      <PresetSelector {presets} disabled={separating} onseparate={handleSeparate} {modelsError} />
    </section>
  {/if}

  {#if separating}
    <section class="progress">
      <ProgressBar oncomplete={handleComplete} bind:this={progressRef} />
    </section>
  {/if}

  {#if results.length > 0}
    <section class="results">
      <ResultsList files={results} />
    </section>
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

  .upload {
    width: 100%;
  }

  .controls {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .file-name {
    margin: 0;
    font-size: 0.95rem;
    color: #00d4ff;
    font-weight: 500;
    word-break: break-all;
  }

  .progress {
    width: 100%;
  }

  .results {
    width: 100%;
  }

  /* Transiciones suaves entre estados */
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
