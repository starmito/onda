<script lang="ts">
  import { midiParse, midiExport, uploadAudioDAW, type MidiTrack, type MidiNote } from './api';
  import PianoRoll from './PianoRoll.svelte';
  import { IconUpload, IconDownload } from './icons';

  let tracks = $state<MidiTrack[]>([]);
  let bpm = $state(120);
  let hiddenTracks = $state<Set<number>>(new Set());
  let loading = $state(false);
  let error = $state('');
  let fileInput: HTMLInputElement;

  const visibleTracks = $derived(tracks.filter((t) => !hiddenTracks.has(t.index)));
  const visibleNotes = $derived(
    visibleTracks
      .flatMap((t) => t.notes)
      .sort((a, b) => a.start_ms - b.start_ms),
  );

  function reset() {
    tracks = [];
    bpm = 120;
    hiddenTracks = new Set();
    error = '';
  }

  async function handleFile(file: File) {
    if (!file.name.toLowerCase().endsWith('.mid') && !file.name.toLowerCase().endsWith('.midi')) {
      error = 'Selecciona un archivo MIDI (.mid o .midi)';
      return;
    }
    loading = true;
    error = '';
    try {
      const uploaded = await uploadAudioDAW(file);
      const parsed = await midiParse(uploaded.file);
      tracks = parsed.tracks;
      bpm = parsed.bpm || 120;
      hiddenTracks = new Set();
    } catch (err: any) {
      error = err?.message || 'Error al importar el archivo MIDI';
      tracks = [];
    } finally {
      loading = false;
    }
  }

  function handleInputChange(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file) {
      handleFile(file);
    }
    input.value = '';
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    const files = e.dataTransfer?.files;
    if (files && files.length > 0) {
      handleFile(files[0]);
    }
  }

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
  }

  function toggleTrack(index: number) {
    const next = new Set(hiddenTracks);
    if (next.has(index)) {
      next.delete(index);
    } else {
      next.add(index);
    }
    hiddenTracks = next;
  }

  async function handleExport() {
    if (tracks.length === 0) return;
    try {
      const blob = await midiExport(tracks, bpm);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'onda-export.mid';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err: any) {
      error = err?.message || 'Error al exportar el MIDI';
    }
  }

  function handleNotesChange(newNotes: MidiNote[]) {
    // Basic update propagation: rewrite notes back to first visible track for now.
    // In a fully editable piano roll this would map per-note track identity.
    if (visibleTracks.length === 0) return;
    // Keep hidden tracks intact; merge visible notes into their original tracks by start/key.
    const noteById = new Map<string, MidiNote>();
    for (const n of newNotes) {
      noteById.set(`${n.key}-${n.start_ms}`, n);
    }
    tracks = tracks.map((t) => {
      if (hiddenTracks.has(t.index)) return t;
      return {
        ...t,
        notes: t.notes.map((n) => noteById.get(`${n.key}-${n.start_ms}`) || n),
      };
    });
  }
</script>

<section class="midi-page">
  <header class="midi-header">
    <h2>Piano Roll MIDI</h2>
    <div class="midi-actions">
      <button class="btn-midi" onclick={() => fileInput?.click()} disabled={loading}>
        <span class="icon">{@html IconUpload}</span>
        Importar MIDI
      </button>
      <button class="btn-midi" onclick={handleExport} disabled={tracks.length === 0 || loading}>
        <span class="icon">{@html IconDownload}</span>
        Exportar MIDI
      </button>
      <input
        bind:this={fileInput}
        type="file"
        accept=".mid,.midi,audio/midi,audio/x-midi"
        onchange={handleInputChange}
        class="file-input"
      />
    </div>
  </header>

  {#if error}
    <div class="midi-error">
      <span>{error}</span>
      <button class="btn-close-error" onclick={() => (error = '')}>✕</button>
    </div>
  {/if}

  {#if loading}
    <div class="midi-loading">
      <span class="spinner"></span>
      <span>Importando MIDI…</span>
    </div>
  {/if}

  {#if tracks.length > 0}
    <div class="midi-meta">
      {#if tracks.length > 1}
        <div class="track-chips">
          {#each tracks as track (track.index)}
            <button
              class="track-chip"
              class:active={!hiddenTracks.has(track.index)}
              onclick={() => toggleTrack(track.index)}
              title={track.name || `Pista ${track.index + 1}`}
            >
              <span class="chip-dot" class:hidden={hiddenTracks.has(track.index)}></span>
              {track.name || `Pista ${track.index + 1}`}
              <span class="chip-count">({track.notes.length})</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <div class="piano-roll-wrapper">
      <PianoRoll notes={visibleNotes} {bpm} onNotesChange={handleNotesChange} />
    </div>
  {:else if !loading}
    <div
      class="midi-empty"
      role="button"
      tabindex="0"
      onclick={() => fileInput?.click()}
      onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') fileInput?.click(); }}
      ondrop={handleDrop}
      ondragover={handleDragOver}
    >
      <span class="empty-icon">🎹</span>
      <p class="empty-title">Importa o arrastra un archivo MIDI para empezar</p>
      <p class="empty-hint">Soporta archivos .mid y .midi</p>
    </div>
  {/if}
</section>

<style>
  .midi-page {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .midi-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .midi-header h2 {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 700;
    color: var(--text-primary);
  }

  .midi-actions {
    display: flex;
    gap: 0.5rem;
  }

  .btn-midi {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.55rem 0.9rem;
    border: 1px solid var(--accent-border);
    border-radius: 6px;
    background: var(--accent-bg);
    color: var(--text-primary);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn-midi:hover:not(:disabled) {
    background: var(--accent-subtle);
    border-color: var(--accent);
  }

  .btn-midi:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-midi .icon :global(svg) {
    width: 16px;
    height: 16px;
    display: block;
  }

  .file-input {
    display: none;
  }

  .midi-meta {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .track-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
  }

  .track-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.35rem 0.65rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--bg-surface);
    color: var(--text-secondary);
    font-size: 0.75rem;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, color 0.15s;
  }

  .track-chip.active {
    border-color: var(--accent-border);
    background: var(--accent-bg);
    color: var(--text-primary);
  }

  .track-chip:hover {
    border-color: var(--accent);
  }

  .chip-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent-light);
  }

  .chip-dot.hidden {
    background: var(--text-muted);
  }

  .chip-count {
    color: var(--text-muted);
  }

  .piano-roll-wrapper {
    flex: 1;
    min-height: 0;
  }

  .midi-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    flex: 1;
    min-height: 280px;
    border: 2px dashed var(--border);
    border-radius: 12px;
    background: var(--bg-primary);
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    text-align: center;
  }

  .midi-empty:hover {
    border-color: var(--accent);
    background: var(--bg-hover);
  }

  .empty-icon {
    font-size: 3rem;
  }

  .empty-title {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .empty-hint {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .midi-loading {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.6rem;
    padding: 1rem;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .spinner {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .midi-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.75rem 1rem;
    background: rgba(244, 67, 54, 0.12);
    border: 1px solid rgba(244, 67, 54, 0.25);
    border-radius: 8px;
    color: #e57373;
    font-size: 0.85rem;
  }

  .btn-close-error {
    background: rgba(244, 67, 54, 0.15);
    border: 1px solid rgba(244, 67, 54, 0.25);
    color: #e57373;
    border-radius: 4px;
    cursor: pointer;
    line-height: 1;
    padding: 0.15rem 0.4rem;
  }

  .btn-close-error:hover {
    background: rgba(244, 67, 54, 0.25);
  }
</style>
