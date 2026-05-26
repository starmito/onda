<script lang="ts">
  import { downloadUrl, deleteSong } from './api';
  import AudioControls from './AudioControls.svelte';
  import type { StemTrack } from './AudioControls.svelte';

  export interface ResultStem {
    name: string;
    path: string;
    song: string;
  }

  export interface SongGroup {
    song: string;
    stems: {
      name: string;
      path: string;
      muted: boolean;
      solo: boolean;
      volume: number;
    }[];
  }

  let {
    files = [],
  }: {
    files?: ResultStem[];
  } = $props();

  // Group files by song
  let songGroups = $derived(groupFiles(files));

  // State for mute/solo/volume per stem
  let stemStates = $state<Record<string, { muted: boolean; solo: boolean; volume: number }>>({});

  // Waveform canvas refs
  let canvasRefs = $state<Record<string, HTMLCanvasElement>>({});
  let waveformDrawn = $state<Set<string>>(new Set());

  function groupFiles(f: ResultStem[]): SongGroup[] {
    const map = new Map<string, ResultStem[]>();
    for (const stem of f) {
      const song = stem.song || extractSong(stem.name, stem.path);
      if (!map.has(song)) map.set(song, []);
      map.get(song)!.push(stem);
    }
    return Array.from(map.entries()).map(([song, stems]) => ({
      song,
      stems: stems.map((s) => {
        const key = stemKey(s);
        if (!stemStates[key]) {
          stemStates[key] = { muted: false, solo: false, volume: 100 };
        }
        return { ...s, ...stemStates[key] };
      }),
    }));
  }

  function extractSong(name: string, path: string): string {
    // Try to extract song name from path or filename
    if (path) {
      const parts = path.split('/');
      if (parts.length >= 2) return parts[parts.length - 2];
    }
    // Fallback: remove common stem suffixes
    return name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, '');
  }

  function stemKey(s: ResultStem): string {
    return `${s.song || 'unknown'}/${s.name}`;
  }

  function stemEmoji(name: string): string {
    const n = name.toLowerCase();
    if (n.includes('drum') || n.includes('bater')) return '🥁';
    if (n.includes('bass') || n.includes('bajo')) return '🎸';
    if (n.includes('vocal') || n.includes('voice')) return '🎤';
    if (n.includes('other') || n.includes('otro')) return '🎹';
    if (n.includes('instrumental')) return '🎼';
    return '🎵';
  }

  function toggleMute(song: string, name: string) {
    const key = `${song}/${name}`;
    stemStates[key] = {
      ...stemStates[key],
      muted: !(stemStates[key]?.muted ?? false),
    };
  }

  function toggleSolo(song: string, name: string) {
    const key = `${song}/${name}`;
    stemStates[key] = {
      ...stemStates[key],
      solo: !(stemStates[key]?.solo ?? false),
    };
  }

  function setVolume(song: string, name: string, vol: number) {
    const key = `${song}/${name}`;
    stemStates[key] = {
      ...stemStates[key],
      volume: vol,
    };
  }

  function handleDelete(song: string) {
    if (confirm(`Delete all files for "${song}"?`)) {
      deleteSong(song).catch((err) => alert('Delete failed: ' + err.message));
    }
  }

  function handleVolumeChange(e: Event, song: string, name: string) {
    setVolume(song, name, parseInt((e.target as HTMLInputElement).value));
  }

  function handleExport(song: string) {
    // Download all stems for a song
    const group = songGroups.find((g) => g.song === song);
    if (!group) return;
    for (const stem of group.stems) {
      const url = downloadUrl(song, stem.name);
      const a = document.createElement('a');
      a.href = url;
      a.download = stem.name;
      a.click();
    }
  }

  // Build flat stem list for AudioControls
  let allStems = $derived(
    songGroups.flatMap((g) =>
      g.stems.map((s) => ({
        name: s.name,
        path: s.path,
        song: g.song,
        muted: stemStates[`${g.song}/${s.name}`]?.muted ?? false,
        solo: stemStates[`${g.song}/${s.name}`]?.solo ?? false,
        volume: stemStates[`${g.song}/${s.name}`]?.volume ?? 100,
      })),
    ),
  );

  function drawWaveform(song: string, name: string, canvas: HTMLCanvasElement) {
    const key = `${song}/${name}`;
    if (waveformDrawn.has(key)) return;
    waveformDrawn.add(key);

    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = '#1a1a2e';
    ctx.fillRect(0, 0, w, h);

    // Draw placeholder waveform (random bars since we don't decode audio just for visual)
    ctx.fillStyle = '#00d4ff';
    const barCount = 40;
    const barWidth = w / barCount;
    for (let i = 0; i < barCount; i++) {
      const randomHeight = Math.random() * h * 0.8 + h * 0.1;
      const x = i * barWidth + 1;
      const y = (h - randomHeight) / 2;
      ctx.fillRect(x, y, barWidth - 2, randomHeight);
    }
  }

  function waveformAction(node: HTMLCanvasElement, params: { song: string; name: string }) {
    canvasRefs[`${params.song}/${params.name}`] = node;
    drawWaveform(params.song, params.name, node);
  }
</script>

{#if songGroups.length > 0}
  <div class="results-panel">
    <h2 class="results-title">📀 Results</h2>

    <AudioControls stems={allStems} />

    {#each songGroups as group (group.song)}
      <div class="song-group">
        <div class="song-header">
          <h3 class="song-name">🎵 {group.song}</h3>
          <div class="song-actions">
            <button class="song-btn export-btn" onclick={() => handleExport(group.song)} title="Download all stems">
              ⬇ Export
            </button>
            <button class="song-btn delete-btn" onclick={() => handleDelete(group.song)} title="Delete song">
              🗑
            </button>
          </div>
        </div>

        <div class="stems-list">
          {#each group.stems as stem (stem.name)}
            {@const key = `${group.song}/${stem.name}`}
            {@const state = stemStates[key] ?? { muted: false, solo: false, volume: 100 }}
            <div class="stem-row" class:muted={state.muted}>
              <!-- Waveform -->
              <canvas
                class="waveform-mini"
                width="200"
                height="32"
                use:waveformAction={{ song: group.song, name: stem.name }}
              ></canvas>

              <!-- Stem info -->
              <span class="stem-emoji">{stemEmoji(stem.name)}</span>
              <span class="stem-name" title={stem.name}>{stem.name}</span>

              <!-- Controls -->
              <div class="stem-controls">
                <button
                  class="stem-btn mute-btn"
                  class:active={state.muted}
                  onclick={() => toggleMute(group.song, stem.name)}
                  title="Mute"
                >
                  M
                </button>
                <button
                  class="stem-btn solo-btn"
                  class:active={state.solo}
                  onclick={() => toggleSolo(group.song, stem.name)}
                  title="Solo"
                >
                  S
                </button>
                <div class="vol-slider-wrap">
                  <input
                    type="range"
                    min="0"
                    max="100"
                    value={state.volume}
                    oninput={(e) => handleVolumeChange(e, group.song, stem.name)}
                    class="vol-slider"
                    title={`Volume: ${state.volume}%`}
                  />
                  <span class="vol-label">{state.volume}</span>
                </div>
                <a
                  class="stem-btn dl-btn"
                  href={downloadUrl(group.song, stem.name)}
                  download={stem.name}
                  title="Download"
                >
                  ⬇
                </a>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/each}
  </div>
{/if}

<style>
  .results-panel {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
  }

  .results-title {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 600;
    color: #e0e0e0;
  }

  .song-group {
    background: #1a1a2e;
    border-radius: 8px;
    padding: 0.75rem 1rem;
    animation: fadeIn 0.3s ease;
  }

  .song-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid #2a2a3e;
    margin-bottom: 0.5rem;
  }

  .song-name {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: #00d4ff;
    word-break: break-word;
  }

  .song-actions {
    display: flex;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .song-btn {
    padding: 0.3rem 0.6rem;
    border-radius: 5px;
    border: 1px solid #444;
    background: #2a2a3e;
    color: #ccc;
    font-size: 0.75rem;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
    white-space: nowrap;
  }
  .song-btn:hover {
    background: #333355;
    border-color: #666;
  }
  .export-btn:hover { color: #00d4ff; border-color: #00d4ff; }
  .delete-btn:hover { color: #f44336; border-color: #f44336; }

  .stems-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .stem-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.4rem 0.5rem;
    border-radius: 6px;
    background: #111;
    transition: background 0.2s, opacity 0.2s;
  }
  .stem-row:hover {
    background: #1a1a30;
  }
  .stem-row.muted {
    opacity: 0.45;
  }

  .waveform-mini {
    border-radius: 3px;
    flex-shrink: 0;
    display: block;
  }

  .stem-emoji {
    font-size: 1rem;
    flex-shrink: 0;
  }

  .stem-name {
    flex: 1;
    font-size: 0.85rem;
    color: #e0e0e0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .stem-controls {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    flex-shrink: 0;
  }

  .stem-btn {
    width: 28px;
    height: 28px;
    border-radius: 4px;
    border: 1px solid #444;
    background: #2a2a3e;
    color: #888;
    font-size: 0.7rem;
    font-weight: 700;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s;
    text-decoration: none;
    padding: 0;
  }
  .stem-btn:hover {
    background: #333355;
    border-color: #666;
    color: #ccc;
  }
  .stem-btn.active {
    background: #00d4ff;
    color: #0a0a14;
    border-color: #00d4ff;
  }
  .mute-btn.active {
    background: #f44336;
    border-color: #f44336;
  }
  .solo-btn.active {
    background: #ff9800;
    border-color: #ff9800;
  }
  .dl-btn {
    color: #00d4ff;
    font-size: 0.8rem;
  }
  .dl-btn:hover {
    color: #00d4ff;
    border-color: #00d4ff;
  }

  .vol-slider-wrap {
    display: flex;
    align-items: center;
    gap: 0.2rem;
  }

  .vol-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 60px;
    height: 4px;
    border-radius: 2px;
    background: #2a2a3e;
    outline: none;
    cursor: pointer;
  }
  .vol-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: #00d4ff;
    cursor: pointer;
    border: 2px solid #0a0a14;
  }

  .vol-label {
    font-size: 0.7rem;
    color: #888;
    width: 24px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  @media (max-width: 600px) {
    .stem-row {
      flex-wrap: wrap;
      gap: 0.3rem;
    }
    .waveform-mini {
      width: 100%;
      order: -1;
    }
    .vol-slider {
      width: 40px;
    }
  }
</style>
