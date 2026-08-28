<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { MidiNote } from './api';

  interface Props {
    notes: MidiNote[];
    bpm?: number;
    snap?: number;
    startNote?: number;
    endNote?: number;
    readonly?: boolean;
    onNotesChange?: (notes: MidiNote[]) => void;
  }

  let {
    notes = [],
    bpm = 120,
    snap = 16,
    startNote = 24,
    endNote = 96,
    readonly = false,
    onNotesChange,
  }: Props = $props();

  const PIXELS_PER_BEAT = 120;
  const NOTE_HEIGHT = 14;
  const TOTAL_BEATS = 16;
  const KEYBOARD_WIDTH = 64;

  let canvasEl: HTMLCanvasElement;
  let containerEl: HTMLDivElement;

  const NOTE_NAMES = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B'];
  const BLACK_KEYS = new Set([1, 3, 6, 8, 10]);

  // ---- Playback state ----
  let audioCtx: AudioContext | null = $state(null);
  let isPlaying = $state(false);
  let playheadBeat = $state(-1);
  let playbackStartTime = 0;
  let playbackEndTime = 0;
  let activeNodes: { osc: OscillatorNode; gain: GainNode }[] = [];
  let animationFrameId: number | null = null;
  let stopTimeoutId: ReturnType<typeof setTimeout> | null = null;

  function noteName(key: number): string {
    const name = NOTE_NAMES[key % 12];
    const octave = Math.floor(key / 12) - 1;
    return `${name}${octave}`;
  }

  function isBlackKey(key: number): boolean {
    return BLACK_KEYS.has(key % 12);
  }

  function isC(key: number): boolean {
    return key % 12 === 0;
  }

  function frequencyForKey(key: number): number {
    // A4 = 69 = 440Hz
    return 440 * Math.pow(2, (key - 69) / 12);
  }

  async function ensureAudioContext(): Promise<AudioContext> {
    if (!audioCtx) {
      const AudioCtx = window.AudioContext || (window as any).webkitAudioContext;
      audioCtx = new AudioCtx();
    }
    if (audioCtx.state === 'suspended') {
      await audioCtx.resume();
    }
    return audioCtx;
  }

  function playTone(key: number, duration = 0.5) {
    try {
      ensureAudioContext().then((ctx) => {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.type = 'sine';
        osc.frequency.value = frequencyForKey(key);
        osc.connect(gain);
        gain.connect(ctx.destination);
        const now = ctx.currentTime;
        gain.gain.setValueAtTime(0.3, now);
        gain.gain.exponentialRampToValueAtTime(0.001, now + duration);
        osc.start(now);
        osc.stop(now + duration);
        setTimeout(() => {
          try {
            osc.disconnect();
            gain.disconnect();
          } catch {
            // ignore cleanup errors
          }
        }, Math.ceil((duration + 0.1) * 1000));
      });
    } catch {
      // ignore audio errors
    }
  }

  function stopPlayback() {
    if (stopTimeoutId) {
      clearTimeout(stopTimeoutId);
      stopTimeoutId = null;
    }
    if (animationFrameId) {
      cancelAnimationFrame(animationFrameId);
      animationFrameId = null;
    }
    isPlaying = false;
    playheadBeat = -1;

    for (const node of activeNodes) {
      try {
        node.osc.stop();
        node.osc.disconnect();
        node.gain.disconnect();
      } catch {
        // ignore
      }
    }
    activeNodes = [];
  }

  function scheduleStop() {
    if (!audioCtx) return;
    const remaining = Math.max(0, (playbackEndTime - audioCtx.currentTime) * 1000);
    stopTimeoutId = setTimeout(() => {
      stopPlayback();
    }, remaining + 100);
  }

  function animatePlayhead() {
    if (!isPlaying || !audioCtx) return;
    const elapsed = audioCtx.currentTime - playbackStartTime;
    playheadBeat = elapsed * (bpm / 60);

    if (audioCtx.currentTime < playbackEndTime + 0.1) {
      animationFrameId = requestAnimationFrame(animatePlayhead);
    } else {
      animationFrameId = requestAnimationFrame(() => stopPlayback());
    }
  }

  async function playSequence() {
    if (notes.length === 0) return;
    stopPlayback();
    const ctx = await ensureAudioContext();

    isPlaying = true;
    playbackStartTime = ctx.currentTime + 0.05;
    playbackEndTime = playbackStartTime;
    activeNodes = [];

    for (const note of notes) {
      const t0 = playbackStartTime + note.start_ms / 1000;
      const duration = Math.max(0.05, (note.end_ms - note.start_ms) / 1000);
      const t1 = t0 + duration;

      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'triangle';
      osc.frequency.value = frequencyForKey(note.key);

      const velocity = Math.min(127, Math.max(0, note.velocity || 80));
      const peakGain = 0.25 * (velocity / 127);

      gain.gain.setValueAtTime(0, t0);
      gain.gain.linearRampToValueAtTime(peakGain, t0 + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.001, t1);

      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(t0);
      osc.stop(t1);

      activeNodes.push({ osc, gain });
      if (t1 > playbackEndTime) playbackEndTime = t1;
    }

    scheduleStop();
    animatePlayhead();
  }

  function draw(
    currentNotes: MidiNote[],
    currentBpm: number,
    currentStart: number,
    currentEnd: number,
    currentPlayheadBeat: number,
  ) {
    if (!canvasEl) return;
    const ctx = canvasEl.getContext('2d');
    if (!ctx) return;

    const noteCount = currentEnd - currentStart;
    const width = KEYBOARD_WIDTH + TOTAL_BEATS * PIXELS_PER_BEAT;
    const height = noteCount * NOTE_HEIGHT;

    // Update canvas size for crisp rendering
    const dpr = window.devicePixelRatio || 1;
    canvasEl.width = width * dpr;
    canvasEl.height = height * dpr;
    canvasEl.style.width = `${width}px`;
    canvasEl.style.height = `${height}px`;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    // Background
    ctx.fillStyle = '#1a1a2e';
    ctx.fillRect(0, 0, width, height);

    // Keyboard + note separators
    for (let key = currentStart; key < currentEnd; key++) {
      const y = (currentEnd - key - 1) * NOTE_HEIGHT;
      const black = isBlackKey(key);

      // White key background
      if (!black) {
        ctx.fillStyle = '#262640';
        ctx.fillRect(0, y, KEYBOARD_WIDTH, NOTE_HEIGHT);
      }

      // Black key left marker
      if (black) {
        ctx.fillStyle = '#333366';
        ctx.fillRect(0, y, 24, NOTE_HEIGHT);
      }

      // Horizontal separator line
      ctx.strokeStyle = '#333355';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(0, y + NOTE_HEIGHT - 0.5);
      ctx.lineTo(width, y + NOTE_HEIGHT - 0.5);
      ctx.stroke();

      // Note name on C
      if (isC(key)) {
        ctx.fillStyle = '#8888aa';
        ctx.font = '10px monospace';
        ctx.textAlign = 'left';
        ctx.textBaseline = 'middle';
        ctx.fillText(noteName(key), 6, y + NOTE_HEIGHT / 2 + 1);
      }
    }

    // Vertical grid lines (beats + subdivisions)
    const msPerBeat = 60000 / currentBpm;
    ctx.strokeStyle = '#2a2a4a';
    ctx.lineWidth = 1;
    for (let beat = 0; beat <= TOTAL_BEATS * snap; beat++) {
      const isMainBeat = beat % snap === 0;
      const x = KEYBOARD_WIDTH + (beat / snap) * PIXELS_PER_BEAT;
      ctx.strokeStyle = isMainBeat ? '#3a3a6a' : '#2a2a4a';
      ctx.lineWidth = isMainBeat ? 1.5 : 0.5;
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, height);
      ctx.stroke();

      if (isMainBeat && beat < TOTAL_BEATS * snap) {
        ctx.fillStyle = '#555577';
        ctx.font = '10px monospace';
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillText(String(beat / snap), x + 4, 4);
      }
    }

    // MIDI notes
    for (const note of currentNotes) {
      if (note.key < currentStart || note.key >= currentEnd) continue;
      const y = (currentEnd - note.key - 1) * NOTE_HEIGHT + 1;
      const startBeat = note.start_ms / msPerBeat;
      const durationBeat = (note.end_ms - note.start_ms) / msPerBeat;
      const x = KEYBOARD_WIDTH + startBeat * PIXELS_PER_BEAT;
      const w = Math.max(2, durationBeat * PIXELS_PER_BEAT - 2);

      const intensity = Math.min(255, Math.max(55, note.velocity * 2));
      ctx.fillStyle = `rgb(80, ${intensity}, 180)`;
      ctx.fillRect(x, y, w, NOTE_HEIGHT - 2);

      // Note border
      ctx.strokeStyle = `rgba(120, ${Math.min(255, intensity + 40)}, 220, 0.8)`;
      ctx.lineWidth = 1;
      ctx.strokeRect(x, y, w, NOTE_HEIGHT - 2);
    }

    // Playback playhead
    if (currentPlayheadBeat >= 0 && currentPlayheadBeat <= TOTAL_BEATS) {
      const px = KEYBOARD_WIDTH + currentPlayheadBeat * PIXELS_PER_BEAT;
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(px, 0);
      ctx.lineTo(px, height);
      ctx.stroke();
    }
  }

  function handleClick(e: MouseEvent) {
    if (!canvasEl || readonly) return;
    const rect = canvasEl.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;

    const msPerBeat = 60000 / bpm;
    // Check click against notes in reverse draw order (top first)
    for (let i = notes.length - 1; i >= 0; i--) {
      const note = notes[i];
      if (note.key < startNote || note.key >= endNote) continue;
      const ny = (endNote - note.key - 1) * NOTE_HEIGHT + 1;
      const startBeat = note.start_ms / msPerBeat;
      const durationBeat = (note.end_ms - note.start_ms) / msPerBeat;
      const nx = KEYBOARD_WIDTH + startBeat * PIXELS_PER_BEAT;
      const nw = Math.max(2, durationBeat * PIXELS_PER_BEAT - 2);
      if (x >= nx && x <= nx + nw && y >= ny && y <= ny + NOTE_HEIGHT - 2) {
        playTone(note.key);
        if (onNotesChange) {
          onNotesChange(notes.map((n, idx) => (idx === i ? { ...n } : n)));
        }
        return;
      }
    }
  }

  onMount(() => {
    draw(notes, bpm, startNote, endNote, playheadBeat);
  });

  $effect(() => {
    // Re-run whenever notes or display parameters change
    draw(notes, bpm, startNote, endNote, playheadBeat);
  });

  onDestroy(() => {
    stopPlayback();
    if (audioCtx) {
      audioCtx.close().catch(() => {});
    }
  });
</script>

<div class="piano-roll-container" bind:this={containerEl}>
  <div class="piano-roll-controls">
    <button
      class="btn-play"
      onclick={playSequence}
      disabled={readonly || notes.length === 0 || isPlaying}
      title="Reproducir secuencia MIDI"
    >
      ▶ Play
    </button>
    <button
      class="btn-stop"
      onclick={stopPlayback}
      disabled={!isPlaying}
      title="Detener reproducción"
    >
      ■ Stop
    </button>
    <span class="play-status">
      {#if isPlaying}Reproduciendo…{:else}{notes.length} notas{/if}
    </span>
    <span class="bpm-badge">BPM {bpm}</span>
    <span class="range-badge">Rango {noteName(startNote)} – {noteName(endNote - 1)}</span>
  </div>
  <div class="piano-roll-scroll">
    <canvas bind:this={canvasEl} onclick={handleClick}>
      Tu navegador no soporta canvas.
    </canvas>
  </div>
  <div class="piano-roll-info">
    <span>{notes.length} notas</span>
    <span>BPM {bpm}</span>
    <span>Rango {noteName(startNote)} – {noteName(endNote - 1)}</span>
  </div>
</div>

<style>
  .piano-roll-container {
    display: flex;
    flex-direction: column;
    width: 100%;
    background: #141425;
    border: 1px solid #2a2a4a;
    border-radius: 8px;
    overflow: hidden;
  }

  .piano-roll-controls {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.75rem;
    background: #1e1e2e;
    border-bottom: 1px solid #2a2a4a;
  }

  .piano-roll-controls button {
    padding: 0.35rem 0.7rem;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .piano-roll-controls button:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: var(--accent);
  }

  .piano-roll-controls button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-play {
    background: var(--accent) !important;
    border-color: var(--accent) !important;
    color: #fff !important;
  }

  .btn-play:hover:not(:disabled) {
    background: var(--accent-dark) !important;
  }

  .play-status {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-left: auto;
  }

  .bpm-badge,
  .range-badge {
    font-size: 0.75rem;
    color: var(--text-muted);
    font-family: monospace;
  }

  .piano-roll-scroll {
    width: 100%;
    max-height: 500px;
    overflow-x: auto;
    overflow-y: auto;
  }

  .piano-roll-scroll canvas {
    display: block;
    cursor: crosshair;
  }

  .piano-roll-info {
    display: flex;
    gap: 1.5rem;
    padding: 0.5rem 0.75rem;
    background: #1e1e2e;
    border-top: 1px solid #2a2a4a;
    font-size: 0.75rem;
    color: #8888aa;
    font-family: monospace;
  }

  .piano-roll-info span {
    white-space: nowrap;
  }
</style>
