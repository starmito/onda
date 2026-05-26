<script lang="ts">
  import { downloadUrl } from './api';

  export interface StemTrack {
    name: string;
    path: string;
    song: string;
    muted: boolean;
    solo: boolean;
    volume: number; // 0-100
  }

  let {
    stems = [],
  }: {
    stems?: StemTrack[];
  } = $props();

  let audioCtx = $state<AudioContext | null>(null);
  let playing = $state(false);
  let paused = $state(false);
  let currentTime = $state(0);
  let duration = $state(0);
  let seekValue = $state(0);

  // Per-stem audio nodes
  let sourceNodes = $state<Map<string, AudioBufferSourceNode>>(new Map());
  let gainNodes = $state<Map<string, GainNode>>(new Map());
  let buffers = $state<Map<string, AudioBuffer>>(new Map());
  let startTime = $state(0);
  let pauseOffset = $state(0);

  let animFrame = $state<number | null>(null);
  let loadedSongs = $state<Set<string>>(new Set());

  function getCtx(): AudioContext {
    if (!audioCtx) {
      audioCtx = new AudioContext();
    }
    return audioCtx;
  }

  async function loadBuffers(song: string) {
    if (loadedSongs.has(song)) return;
    loadedSongs.add(song);

    const ctx = getCtx();
    const songStems = stems.filter((s) => s.song === song);
    for (const stem of songStems) {
      try {
        const url = downloadUrl(stem.song, stem.name);
        const resp = await fetch(url);
        const arrayBuf = await resp.arrayBuffer();
        const audioBuf = await ctx.decodeAudioData(arrayBuf);
        buffers.set(stemKey(stem), audioBuf);
      } catch (err) {
        console.error(`Failed to load ${stem.name}:`, err);
      }
    }
  }

  function stemKey(s: StemTrack): string {
    return `${s.song}/${s.name}`;
  }

  function anySolo(): boolean {
    return stems.some((s) => s.solo);
  }

  function effectiveGain(s: StemTrack): number {
    if (s.muted) return 0;
    if (anySolo() && !s.solo) return 0;
    return s.volume / 100;
  }

  function stopAll() {
    sourceNodes.forEach((src) => {
      try { src.stop(); } catch { /* already stopped */ }
    });
    sourceNodes.clear();
    gainNodes.clear();
    if (animFrame) {
      cancelAnimationFrame(animFrame);
      animFrame = null;
    }
  }

  async function play() {
    const ctx = getCtx();
    if (ctx.state === 'suspended') {
      await ctx.resume();
    }

    const song = stems[0]?.song;
    if (!song) return;

    await loadBuffers(song);

    stopAll();

    const offset = pauseOffset;
    const now = ctx.currentTime;
    startTime = now - offset;

    let maxDur = 0;

    for (const stem of stems) {
      const buf = buffers.get(stemKey(stem));
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;

      const gain = ctx.createGain();
      gain.gain.value = effectiveGain(stem);
      gain.connect(ctx.destination);

      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, offset);

      sourceNodes.set(stemKey(stem), src);
      gainNodes.set(stemKey(stem), gain);
    }

    duration = maxDur;
    playing = true;
    paused = false;

    function tick() {
      if (!playing || paused) return;
      const elapsed = ctx.currentTime - startTime;
      currentTime = elapsed;
      seekValue = duration > 0 ? (elapsed / duration) * 1000 : 0;

      if (elapsed >= duration) {
        stop();
        return;
      }
      animFrame = requestAnimationFrame(tick);
    }
    animFrame = requestAnimationFrame(tick);
  }

  function pause() {
    const ctx = audioCtx;
    if (!ctx) return;
    pauseOffset = ctx.currentTime - startTime;
    ctx.suspend();
    paused = true;
  }

  function resume() {
    const ctx = audioCtx;
    if (!ctx) return;
    ctx.resume();
    paused = false;
  }

  function stop() {
    stopAll();
    audioCtx?.suspend();
    playing = false;
    paused = false;
    currentTime = 0;
    seekValue = 0;
    pauseOffset = 0;
    duration = 0;
  }

  function handleSeek(e: Event) {
    const target = e.target as HTMLInputElement;
    const val = parseFloat(target.value);
    seekValue = val;
    const time = duration * (val / 1000);
    pauseOffset = time;
    if (playing) {
      stopAll();
      startTime = (audioCtx?.currentTime ?? 0) - time;
      // Re-create sources
      playFrom(time);
    }
    currentTime = time;
  }

  async function playFrom(offset: number) {
    const ctx = getCtx();
    if (ctx.state === 'suspended') await ctx.resume();
    const now = ctx.currentTime;
    startTime = now - offset;

    let maxDur = 0;
    for (const stem of stems) {
      const buf = buffers.get(stemKey(stem));
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;
      const effectiveOffset = Math.min(offset, buf.duration);

      const gain = ctx.createGain();
      gain.gain.value = effectiveGain(stem);
      gain.connect(ctx.destination);

      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, effectiveOffset);

      sourceNodes.set(stemKey(stem), src);
      gainNodes.set(stemKey(stem), gain);
    }
    duration = maxDur;
  }

  function updateGains() {
    gainNodes.forEach((gain, key) => {
      const stem = stems.find((s) => stemKey(s) === key);
      if (stem) {
        gain.gain.value = effectiveGain(stem);
      }
    });
  }

  // React to stem mute/solo/volume changes
  $effect(() => {
    // Track changes to stems to update gains
    stems.forEach((s) => {
      // access reactive props
      void s.muted;
      void s.solo;
      void s.volume;
    });
    if (playing) updateGains();
  });

  function formatTime(sec: number): string {
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  function hasStems(): boolean {
    return stems.length > 0;
  }
</script>

<div class="audio-controls">
  {#if hasStems()}
    <div class="transport">
      {#if !playing}
        <button class="ctrl-btn play-btn" onclick={play} aria-label="Play">
          ▶
        </button>
      {:else if paused}
        <button class="ctrl-btn play-btn" onclick={resume} aria-label="Resume">
          ▶
        </button>
        <button class="ctrl-btn stop-btn" onclick={stop} aria-label="Stop">
          ⏹
        </button>
      {:else}
        <button class="ctrl-btn pause-btn" onclick={pause} aria-label="Pause">
          ⏸
        </button>
        <button class="ctrl-btn stop-btn" onclick={stop} aria-label="Stop">
          ⏹
        </button>
      {/if}

      <div class="seek-area">
        <input
          type="range"
          min="0"
          max="1000"
          value={seekValue}
          disabled={!playing && !paused}
          oninput={handleSeek}
          class="seek-slider"
        />
        <span class="time-display">
          {formatTime(currentTime)} / {formatTime(duration)}
        </span>
      </div>
    </div>
  {/if}
</div>

<style>
  .audio-controls {
    padding: 0.5rem 0;
  }

  .transport {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.5rem 1rem;
    background: #1a1a2e;
    border-radius: 8px;
  }

  .ctrl-btn {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    border: none;
    font-size: 1.1rem;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.2s, transform 0.1s;
    flex-shrink: 0;
  }
  .ctrl-btn:active {
    transform: scale(0.95);
  }

  .play-btn {
    background: #00d4ff;
    color: #0a0a14;
  }
  .play-btn:hover {
    background: #00b8e0;
  }
  .pause-btn {
    background: #ff9800;
    color: #0a0a14;
  }
  .pause-btn:hover {
    background: #e68900;
  }
  .stop-btn {
    background: #f44336;
    color: #fff;
  }
  .stop-btn:hover {
    background: #d32f2f;
  }

  .seek-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }

  .seek-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 4px;
    border-radius: 2px;
    background: #2a2a3e;
    outline: none;
    cursor: pointer;
  }
  .seek-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: #00d4ff;
    cursor: pointer;
    border: 2px solid #0a0a14;
  }
  .seek-slider:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .time-display {
    font-size: 0.75rem;
    color: #888;
    font-variant-numeric: tabular-nums;
    text-align: right;
  }
</style>
