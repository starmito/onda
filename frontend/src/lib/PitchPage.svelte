<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { uploadPitchAudio, pitchInputDownloadUrl, deletePitchUpload, pitchStems, downloadUrl, deleteStem as deleteStemApi, getPitchSubgroups, deletePitchSubgroup, deleteSong } from './api';
  import type { ResultStem } from './types';
  import { detectStemType, stemEmoji } from './types';
  import { IconUpload, IconSkipBack, IconSkipForward } from './icons';

  // ── Each uploaded file player (standalone, simple) ──
  interface UploadPlayer {
    id: string;
    name: string;
    status: 'uploading' | 'ready' | 'error';
    errorMsg?: string;
    audioCtx: AudioContext | null;
    playing: boolean;
    paused: boolean;
    currentTime: number;
    duration: number;
    seekValue: number;
    sourceNode: AudioBufferSourceNode | null;
    gainNode: GainNode | null;
    buffer: AudioBuffer | null;
    startTime: number;
    pauseOffset: number;
    animFrame: number | null;
    loaded: boolean;
    volume: number;
  }

  // ── Per-group combined player (like ResultsPanel) ──
  interface GroupPlayer {
    audioCtx: AudioContext | null;
    playing: boolean;
    paused: boolean;
    currentTime: number;
    duration: number;
    seekValue: number;
    sourceNodes: Map<string, AudioBufferSourceNode>;
    gainNodes: Map<string, GainNode>;
    buffers: Map<string, AudioBuffer>;
    analysers: Map<string, AnalyserNode[]>;
    startTime: number;
    pauseOffset: number;
    animFrame: number | null;
    loaded: boolean;
  }

  // ── Pitch subgroup ──
  interface SubgroupStem {
    name: string;
    path: string;
    stemType: string;
  }
  interface Subgroup {
    pitch: number;
    stems: SubgroupStem[];
  }
  interface SubgroupPlayer {
    audioCtx: AudioContext | null;
    playing: boolean;
    paused: boolean;
    currentTime: number;
    duration: number;
    seekValue: number;
    sourceNodes: Map<string, AudioBufferSourceNode>;
    gainNodes: Map<string, GainNode>;
    analysers: Map<string, AnalyserNode[]>;
    buffers: Map<string, AudioBuffer>;
    startTime: number;
    pauseOffset: number;
    animFrame: number | null;
    loaded: boolean;
  }

  // ── State ──

  let uploadPlayers = $state<UploadPlayer[]>([]);
  let dragCounter = $state(0);
  let toast = $state<{ message: string; type: 'success' | 'error' } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Per-stem mute/solo/volume (shared between groups and subgroups)
  let stemStates = $state<Record<string, { muted: boolean; solo: boolean; volume: number }>>({});

  // Per-song group combined players
  let groupPlayers = $state<Record<string, GroupPlayer>>({});

  // Pitch subgroups
  let pitchSubgroups = $state<Record<string, Subgroup[]>>({});
  let subgroupPlayers = $state<Record<string, SubgroupPlayer>>({});
  let loadingSubgroups = $state<Record<string, boolean>>({});
  let abortController = new AbortController();

  let waveformCanvases = $state<Record<string, HTMLCanvasElement>>({});
  let wavePeaksCache = $state<Record<string, number[]>>({});
  const WAVEFORM_H = 80;

  // Drag state for waveform seek
  let dragging = $state<Record<string, boolean>>({});
  let dragPreview = $state<Record<string, number>>({}); // 0-1 fraction during drag

  // Per-stem real-time peak levels (for peak meters)
  let stemLevels = $state<Record<string, { l: number; r: number }>>({});
  // Per-stem peak hold (highest value reached during playback)
  let stemPeaks = $state<Record<string, { l: number; r: number }>>({});

  // Subgroup peak levels and peaks
  let subgroupLevels = $state<Record<string, { l: number; r: number }>>({});
  let subgroupPeaks = $state<Record<string, { l: number; r: number }>>({});

  // Subgroup waveform state
  let subgroupWaveformCanvases = $state<Record<string, HTMLCanvasElement>>({});
  let subgroupWavePeaksCache = $state<Record<string, number[]>>({});
  let subgroupDragging = $state<Record<string, boolean>>({});
  let subgroupDragPreview = $state<Record<string, number>>({});

  function rmsToDb(rms: number): number {
    if (rms < 0.001) return -60;
    return Math.max(-60, 20 * Math.log10(rms));
  }
  function toDbStr(rms: number): string {
    const db = rmsToDb(rms);
    return db <= -60 ? '-∞' : db.toFixed(1);
  }
  function dbToPct(db: number): number {
    return Math.min(100, Math.max(0, ((db + 60) / 60) * 100));
  }

  // ── Props ──
  let {
    results = [] as ResultStem[],
    onResultsChange = () => {},
  } = $props();

  // ── Derived: group results by song ──
  let groupSongs = $derived<string[]>([...new Set(results.map(r => r.song))]);

  function stemsForSong(song: string): ResultStem[] {
    return results.filter(r => r.song === song);
  }

  function stemStateKey(song: string, name: string): string {
    return `${song}/${name}`;
  }
  function getStemState(song: string, name: string) {
    const key = stemStateKey(song, name);
    return stemStates[key] || { muted: false, solo: false, volume: 100 };
  }
  function anySolo(song: string): boolean {
    const stems = stemsForSong(song);
    return stems.some(s => stemStates[stemStateKey(song, s.name)]?.solo);
  }
  function effectiveGain(song: string, name: string): number {
    const state = getStemState(song, name);
    if (state.muted) return 0;
    if (anySolo(song) && !state.solo) return 0;
    return state.volume / 100;
  }

  function toggleMute(song: string, name: string) {
    const key = stemStateKey(song, name);
    stemStates[key] = { ...getStemState(song, name), muted: !(stemStates[key]?.muted ?? false) };
    syncGains(song);
  }
  function toggleSolo(song: string, name: string) {
    const key = stemStateKey(song, name);
    stemStates[key] = { ...getStemState(song, name), solo: !(stemStates[key]?.solo ?? false) };
    syncGains(song);
  }
  function setVolume(song: string, name: string, vol: number) {
    const key = stemStateKey(song, name);
    stemStates[key] = { ...getStemState(song, name), volume: vol };
    syncGains(song);
  }
  function handleVolumeChange(e: Event, song: string, name: string) {
    setVolume(song, name, parseInt((e.target as HTMLInputElement).value));
  }

  function syncGains(song: string) {
    const p = groupPlayers[song];
    if (!p || !p.playing) return;
    const stems = stemsForSong(song);
    for (const stem of stems) {
      const key = stemStateKey(song, stem.name);
      const gain = p.gainNodes.get(key);
      if (gain) gain.gain.value = effectiveGain(song, stem.name);
    }
  }

  // ── Subgroup stem state (same pattern, different key) ──
  function subgroupStemKey(song: string, pitchIdx: number, name: string): string {
    return `subgroup:${song}:${pitchIdx}:${name}`;
  }
  function getSubgroupStemState(song: string, pitchIdx: number, name: string) {
    const key = subgroupStemKey(song, pitchIdx, name);
    return stemStates[key] || { muted: false, solo: false, volume: 100 };
  }
  function anySubgroupSolo(song: string, pitchIdx: number): boolean {
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx]) return false;
    return subs[pitchIdx].stems.some(s => stemStates[subgroupStemKey(song, pitchIdx, s.name)]?.solo);
  }
  function effectiveSubgroupGain(song: string, pitchIdx: number, name: string): number {
    const state = getSubgroupStemState(song, pitchIdx, name);
    if (state.muted) return 0;
    if (anySubgroupSolo(song, pitchIdx) && !state.solo) return 0;
    return state.volume / 100;
  }
  function toggleSubgroupMute(song: string, pitchIdx: number, name: string) {
    const key = subgroupStemKey(song, pitchIdx, name);
    stemStates[key] = { ...getSubgroupStemState(song, pitchIdx, name), muted: !(stemStates[key]?.muted ?? false) };
    syncSubgroupGains(song, pitchIdx);
  }
  function toggleSubgroupSolo(song: string, pitchIdx: number, name: string) {
    const key = subgroupStemKey(song, pitchIdx, name);
    stemStates[key] = { ...getSubgroupStemState(song, pitchIdx, name), solo: !(stemStates[key]?.solo ?? false) };
    syncSubgroupGains(song, pitchIdx);
  }
  function setSubgroupVolume(song: string, pitchIdx: number, name: string, vol: number) {
    const key = subgroupStemKey(song, pitchIdx, name);
    stemStates[key] = { ...getSubgroupStemState(song, pitchIdx, name), volume: vol };
    syncSubgroupGains(song, pitchIdx);
  }
  function handleSubgroupVolumeChange(e: Event, song: string, pitchIdx: number, name: string) {
    setSubgroupVolume(song, pitchIdx, name, parseInt((e.target as HTMLInputElement).value));
  }
  function syncSubgroupGains(song: string, pitchIdx: number) {
    const key = getSubgroupKey(song, pitchIdx);
    const p = subgroupPlayers[key];
    if (!p || !p.playing) return;
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx]) return;
    for (const stem of subs[pitchIdx].stems) {
      const gKey = subgroupStemKey(song, pitchIdx, stem.name);
      const gain = p.gainNodes.get(stem.name);
      if (gain) gain.gain.value = effectiveSubgroupGain(song, pitchIdx, stem.name);
    }
  }

  // ── Group player functions ──

  function getPlayer(song: string): GroupPlayer {
    if (!groupPlayers[song]) {
      groupPlayers[song] = {
        audioCtx: null, playing: false, paused: false,
        currentTime: 0, duration: 0, seekValue: 0,
        sourceNodes: new Map(), gainNodes: new Map(), buffers: new Map(), analysers: new Map(),
        startTime: 0, pauseOffset: 0, animFrame: null, loaded: false,
      };
    }
    return groupPlayers[song];
  }

  function getCtx(song: string): AudioContext {
    const p = getPlayer(song);
    if (!p.audioCtx) p.audioCtx = new AudioContext();
    return p.audioCtx;
  }

  async function loadBuffers(song: string) {
    const p = getPlayer(song);
    if (p.loaded) return;
    const ctx = getCtx(song);
    const stems = stemsForSong(song);
    for (const stem of stems) {
      try {
        const url = downloadUrl(song, stem.name);
        const resp = await fetch(url, { signal: abortController.signal });
        const arrayBuf = await resp.arrayBuffer();
        const audioBuf = await ctx.decodeAudioData(arrayBuf);
        p.buffers.set(stemStateKey(song, stem.name), audioBuf);
      } catch (err) {
        console.error(`Failed to load ${stem.name}:`, err);
      }
    }
    p.loaded = true;
  }

  function stopAllSources(song: string) {
    const p = groupPlayers[song];
    if (!p) return;
    p.sourceNodes.forEach(src => { try { src.stop(); } catch {} });
    p.sourceNodes.clear();
    p.gainNodes.clear();
    p.analysers.clear();
    if (p.animFrame) { cancelAnimationFrame(p.animFrame); p.animFrame = null; }
  }

  async function playGroup(song: string) {
    const p = getPlayer(song);
    if (p.playing && !p.paused) return;
    const ctx = getCtx(song);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadBuffers(song);
    stopAllSources(song);
    const offset = p.paused ? p.pauseOffset : 0;
    const now = ctx.currentTime;
    p.startTime = now - offset;
    let maxDur = 0;
    const stems = stemsForSong(song);
    for (const stem of stems) {
      const buf = p.buffers.get(stemStateKey(song, stem.name));
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;
      const gain = ctx.createGain();
      gain.gain.value = effectiveGain(song, stem.name);

      // Split to stereo for peak meters
      const splitter = ctx.createChannelSplitter(2);
      const analyserL = ctx.createAnalyser();
      analyserL.fftSize = 64;
      const analyserR = ctx.createAnalyser();
      analyserR.fftSize = 64;
      gain.connect(splitter);
      splitter.connect(analyserL, 0);
      splitter.connect(analyserR, 1);
      analyserL.connect(ctx.destination);
      analyserR.connect(ctx.destination);

      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, offset);
      p.sourceNodes.set(stemStateKey(song, stem.name), src);
      p.gainNodes.set(stemStateKey(song, stem.name), gain);
      p.analysers.set(stemStateKey(song, stem.name), [analyserL, analyserR]);
    }
    p.duration = maxDur;
    p.playing = true;
    p.paused = false;
    function tick() {
      const pl = groupPlayers[song];
      if (!pl || !pl.playing || pl.paused) return;
      const elapsed = ctx.currentTime - pl.startTime;
      pl.currentTime = elapsed;
      pl.seekValue = elapsed;
      // Update stem peak levels from analysers
      for (const stem of stems) {
        const key = stemStateKey(song, stem.name);
        const ans = pl.analysers.get(key);
        if (ans) {
          const dataL = new Uint8Array(ans[0].frequencyBinCount);
          const dataR = new Uint8Array(ans[1].frequencyBinCount);
          ans[0].getByteTimeDomainData(dataL);
          ans[1].getByteTimeDomainData(dataR);
          // Compute RMS from time-domain (128 samples each)
          let sumL = 0, sumR = 0;
          for (let j = 0; j < dataL.length; j++) {
            const normL = (dataL[j] - 128) / 128;
            const normR = (dataR[j] - 128) / 128;
            sumL += normL * normL;
            sumR += normR * normR;
          }
          stemLevels = { ...stemLevels, [key]: {
            l: Math.sqrt(sumL / dataL.length),
            r: Math.sqrt(sumR / dataR.length),
          }};
          // Accumulate peak hold
          const curRmsL = Math.sqrt(sumL / dataL.length);
          const curRmsR = Math.sqrt(sumR / dataR.length);
          const prevPeak = stemPeaks[key] || { l: 0, r: 0 };
          stemPeaks = { ...stemPeaks, [key]: {
            l: Math.max(prevPeak.l, curRmsL),
            r: Math.max(prevPeak.r, curRmsR),
          }};
        }
      }
      // Redraw waveform for real-time seek bar
      const cv = waveformCanvases[song];
      if (cv) drawWaveform(cv, song);
      if (elapsed >= pl.duration) { stopGroup(song); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }

  function skipBack(song: string) {
    const p = groupPlayers[song];
    if (!p || !p.loaded || p.duration <= 0) return;
    seekGroup(song, Math.max(0, p.currentTime - 10));
  }

  function skipForward(song: string) {
    const p = groupPlayers[song];
    if (!p || !p.loaded || p.duration <= 0) return;
    seekGroup(song, Math.min(p.duration, p.currentTime + 10));
  }

  function pauseGroup(song: string) {
    const p = groupPlayers[song];
    if (!p || !p.playing || p.paused) return;
    if (!p.audioCtx) return;
    p.pauseOffset = p.audioCtx.currentTime - p.startTime;
    p.audioCtx.suspend();
    p.paused = true;
  }

  function stopGroup(song: string) {
    const p = groupPlayers[song];
    if (!p) return;
    stopAllSources(song);
    p.audioCtx?.suspend();
    p.playing = false; p.paused = false;
    p.currentTime = 0; p.seekValue = 0; p.pauseOffset = 0; p.duration = 0;
    // Reset peak levels
    const stems = stemsForSong(song);
    for (const stem of stems) {
      const key = stemStateKey(song, stem.name);
      stemLevels = { ...stemLevels, [key]: { l: 0, r: 0 } };
      stemPeaks = { ...stemPeaks, [key]: { l: 0, r: 0 } };
    }
  }

  async function seekGroup(song: string, time: number) {
    const p = getPlayer(song);
    const wasPlaying = p.playing;
    stopAllSources(song);
    const ctx = getCtx(song);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadBuffers(song);
    const now = ctx.currentTime;
    p.startTime = now - time;
    p.pauseOffset = time;
    p.currentTime = time;
    p.seekValue = time;
    if (!wasPlaying) return;
    let maxDur = 0;
    const stems = stemsForSong(song);
    for (const stem of stems) {
      const buf = p.buffers.get(stemStateKey(song, stem.name));
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;
      const offset = Math.min(time, buf.duration);
      const gain = ctx.createGain();
      gain.gain.value = effectiveGain(song, stem.name);
      // Analyser for seekGroup peak meters
      const splitter = ctx.createChannelSplitter(2);
      const aL = ctx.createAnalyser(); aL.fftSize = 64;
      const aR = ctx.createAnalyser(); aR.fftSize = 64;
      gain.connect(splitter);
      splitter.connect(aL, 0); splitter.connect(aR, 1);
      aL.connect(ctx.destination); aR.connect(ctx.destination);
      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, offset);
      p.sourceNodes.set(stemStateKey(song, stem.name), src);
      p.gainNodes.set(stemStateKey(song, stem.name), gain);
      p.analysers.set(stemStateKey(song, stem.name), [aL, aR]);
    }
    p.duration = maxDur;
    p.playing = true; p.paused = false;
    function tick() {
      const pl = groupPlayers[song];
      if (!pl || !pl.playing || pl.paused) return;
      const elapsed = ctx.currentTime - pl.startTime;
      pl.currentTime = elapsed; pl.seekValue = elapsed;
      // Update stem peak levels during seek playback
      for (const stem of stems) {
        const key = stemStateKey(song, stem.name);
        const ans = pl.analysers.get(key);
        if (ans) {
          const dL = new Uint8Array(ans[0].frequencyBinCount);
          const dR = new Uint8Array(ans[1].frequencyBinCount);
          ans[0].getByteTimeDomainData(dL);
          ans[1].getByteTimeDomainData(dR);
          let sL = 0, sR = 0;
          for (let j = 0; j < dL.length; j++) {
            sL += ((dL[j] - 128) / 128) ** 2;
            sR += ((dR[j] - 128) / 128) ** 2;
          }
          stemLevels = { ...stemLevels, [key]: { l: Math.sqrt(sL / dL.length), r: Math.sqrt(sR / dR.length) }};
          // Accumulate peak hold
          const curRmsL = Math.sqrt(sL / dL.length);
          const curRmsR = Math.sqrt(sR / dR.length);
          const prevPeak = stemPeaks[key] || { l: 0, r: 0 };
          stemPeaks = { ...stemPeaks, [key]: {
            l: Math.max(prevPeak.l, curRmsL),
            r: Math.max(prevPeak.r, curRmsR),
          }};
        }
      }
      const cv = waveformCanvases[song];
      if (cv) drawWaveform(cv, song);
      if (elapsed >= pl.duration) { stopGroup(song); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }

  function handleSeekInput(e: Event, song: string) {
    const time = parseFloat((e.target as HTMLInputElement).value);
    const p = groupPlayers[song];
    if (p) { p.seekValue = time; p.currentTime = time; p.pauseOffset = time; }
  }
  function handleSeekChange(e: Event, song: string) {
    seekGroup(song, parseFloat((e.target as HTMLInputElement).value));
  }

  // ── Pitch shift ──
  let pitchValues = $state<Record<string, number>>({});
  let pitchProcessing = $state<Record<string, boolean>>({});

  function getPitchValue(song: string): number {
    return pitchValues[song] ?? 0;
  }

  async function handlePitch(song: string) {
    const pitch = getPitchValue(song);
    if (pitch === 0) return;
    pitchProcessing = { ...pitchProcessing, [song]: true };
    try {
      await pitchStems(song, pitch);
      await onResultsChange();
      // Reload pitch subgroups
      await loadPitchSubgroups(song);
      showToast(`Tono cambiado: ${pitch > 0 ? '+' : ''}${pitch} semitonos`, 'success');
    } catch (err: any) {
      showToast(`Error: ${err.message || 'unknown'}`, 'error');
    } finally {
      pitchProcessing = { ...pitchProcessing, [song]: false };
    }
  }

  // ── Pitch subgroups ──
  async function loadPitchSubgroups(song: string) {
    loadingSubgroups = { ...loadingSubgroups, [song]: true };
    try {
      const subs = await getPitchSubgroups(song, abortController.signal);
      const mapped: Subgroup[] = subs.map(s => ({
        pitch: s.pitch,
        stems: s.files.map((f: any) => ({
          name: f.name, path: f.path, stemType: detectStemType(f.name),
        })),
      }));
      pitchSubgroups = { ...pitchSubgroups, [song]: mapped };
    } catch (err) {
      console.error(`Failed to load pitch subgroups for ${song}:`, err);
    } finally {
      loadingSubgroups = { ...loadingSubgroups, [song]: false };
    }
  }

  onMount(async () => {
    // Load subgroups for all existing songs
    for (const song of groupSongs) {
      loadPitchSubgroups(song);
    }
    // Attach global mouseup for drag operations that leave the canvas
    window.addEventListener('mouseup', handleGlobalMouseUp);
    window.addEventListener('mouseup', handleSubgroupGlobalMouseUp);
  });

  // ── Subgroup player functions (same architecture) ──
  function getSubgroupKey(song: string, pitchIdx: number): string {
    return `${song}::${pitchIdx}`;
  }

  function getSubPlayer(key: string): SubgroupPlayer {
    if (!subgroupPlayers[key]) {
      subgroupPlayers[key] = {
        audioCtx: null, playing: false, paused: false,
        currentTime: 0, duration: 0, seekValue: 0,
        sourceNodes: new Map(), gainNodes: new Map(), analysers: new Map(), buffers: new Map(),
        startTime: 0, pauseOffset: 0, animFrame: null, loaded: false,
      };
    }
    return subgroupPlayers[key];
  }

  function getSubCtx(key: string): AudioContext {
    const p = getSubPlayer(key);
    if (!p.audioCtx) p.audioCtx = new AudioContext();
    return p.audioCtx;
  }

  async function loadSubBuffers(song: string, pitchIdx: number) {
    const key = getSubgroupKey(song, pitchIdx);
    const p = getSubPlayer(key);
    if (p.loaded) return;
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx]) return;
    const ctx = getSubCtx(key);
    for (const stem of subs[pitchIdx].stems) {
      try {
        const url = stem.path;
        const resp = await fetch(url, { signal: abortController.signal });
        const arrayBuf = await resp.arrayBuffer();
        const audioBuf = await ctx.decodeAudioData(arrayBuf);
        p.buffers.set(stem.name, audioBuf);
      } catch (err) {
        console.error(`Failed to load subgroup stem ${stem.name}:`, err);
      }
    }
    p.loaded = true;
  }

  function stopAllSubSources(key: string) {
    const p = subgroupPlayers[key];
    if (!p) return;
    p.sourceNodes.forEach(src => { try { src.stop(); } catch {} });
    p.sourceNodes.clear(); p.gainNodes.clear(); p.analysers.clear();
    if (p.animFrame) { cancelAnimationFrame(p.animFrame); p.animFrame = null; }
  }

  async function playSubgroup(song: string, pitchIdx: number) {
    const key = getSubgroupKey(song, pitchIdx);
    const p = getSubPlayer(key);
    if (p.playing && !p.paused) return;
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx]) return;
    const ctx = getSubCtx(key);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadSubBuffers(song, pitchIdx);
    stopAllSubSources(key);
    const offset = p.paused ? p.pauseOffset : 0;
    const now = ctx.currentTime;
    p.startTime = now - offset;
    let maxDur = 0;
    for (const stem of subs[pitchIdx].stems) {
      const buf = p.buffers.get(stem.name);
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;
      const gain = ctx.createGain();
      gain.gain.value = effectiveSubgroupGain(song, pitchIdx, stem.name);
      // Split to stereo for peak meters
      const splitter = ctx.createChannelSplitter(2);
      const analyserL = ctx.createAnalyser();
      analyserL.fftSize = 64;
      const analyserR = ctx.createAnalyser();
      analyserR.fftSize = 64;
      gain.connect(splitter);
      splitter.connect(analyserL, 0);
      splitter.connect(analyserR, 1);
      analyserL.connect(ctx.destination);
      analyserR.connect(ctx.destination);
      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, offset);
      p.sourceNodes.set(stem.name, src);
      p.gainNodes.set(stem.name, gain);
      p.analysers.set(stem.name, [analyserL, analyserR]);
    }
    p.duration = maxDur;
    p.playing = true; p.paused = false;
    function tick() {
      const pl = subgroupPlayers[key];
      if (!pl || !pl.playing || pl.paused) return;
      const elapsed = ctx.currentTime - pl.startTime;
      pl.currentTime = elapsed; pl.seekValue = elapsed;
      // Update stem peak levels from analysers
      for (const stem of (subs[pitchIdx]?.stems || [])) {
        const stKey = subgroupStemKey(song, pitchIdx, stem.name);
        const ans = pl.analysers.get(stem.name);
        if (ans) {
          const dataL = new Uint8Array(ans[0].frequencyBinCount);
          const dataR = new Uint8Array(ans[1].frequencyBinCount);
          ans[0].getByteTimeDomainData(dataL);
          ans[1].getByteTimeDomainData(dataR);
          let sumL = 0, sumR = 0;
          for (let j = 0; j < dataL.length; j++) {
            const normL = (dataL[j] - 128) / 128;
            const normR = (dataR[j] - 128) / 128;
            sumL += normL * normL;
            sumR += normR * normR;
          }
          subgroupLevels = { ...subgroupLevels, [stKey]: {
            l: Math.sqrt(sumL / dataL.length),
            r: Math.sqrt(sumR / dataR.length),
          }};
          const curRmsL = Math.sqrt(sumL / dataL.length);
          const curRmsR = Math.sqrt(sumR / dataR.length);
          const prevPeak = subgroupPeaks[stKey] || { l: 0, r: 0 };
          subgroupPeaks = { ...subgroupPeaks, [stKey]: {
            l: Math.max(prevPeak.l, curRmsL),
            r: Math.max(prevPeak.r, curRmsR),
          }};
        }
      }
      // Redraw subgroup waveform
      const cv = subgroupWaveformCanvases[key];
      if (cv) drawSubgroupWaveform(cv, key);
      if (elapsed >= pl.duration) { stopSubgroup(key); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }

  function pauseSubgroup(key: string) {
    const p = subgroupPlayers[key];
    if (!p || !p.playing || p.paused) return;
    if (!p.audioCtx) return;
    p.pauseOffset = p.audioCtx.currentTime - p.startTime;
    p.audioCtx.suspend();
    p.paused = true;
  }

  function stopSubgroup(key: string) {
    const p = subgroupPlayers[key];
    if (!p) return;
    stopAllSubSources(key);
    p.audioCtx?.suspend();
    p.playing = false; p.paused = false;
    p.currentTime = 0; p.seekValue = 0; p.pauseOffset = 0; p.duration = 0;
    // Reset peak levels for subgroup stems
    const parts = key.split('::');
    const song = parts[0];
    const pitchIdx = parseInt(parts[1]);
    const subs = pitchSubgroups[song];
    if (subs && subs[pitchIdx]) {
      for (const stem of subs[pitchIdx].stems) {
        const stKey = subgroupStemKey(song, pitchIdx, stem.name);
        subgroupLevels = { ...subgroupLevels, [stKey]: { l: 0, r: 0 } };
        subgroupPeaks = { ...subgroupPeaks, [stKey]: { l: 0, r: 0 } };
      }
    }
  }

  async function seekSubgroup(key: string, time: number) {
    const p = getSubPlayer(key);
    const wasPlaying = p.playing;
    stopAllSubSources(key);
    const ctx = getSubCtx(key);
    if (ctx.state === 'suspended') await ctx.resume();
    // derive song + pitchIdx from key
    const parts = key.split('::');
    const song = parts[0];
    const pitchIdx = parseInt(parts[1]);
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx]) return;
    // We need to reload buffers
    const p2 = getSubPlayer(key);
    const wasLoaded = p2.loaded;
    if (!wasLoaded) await loadSubBuffers(song, pitchIdx);
    const now = ctx.currentTime;
    p2.startTime = now - time;
    p2.pauseOffset = time;
    p2.currentTime = time;
    p2.seekValue = time;
    if (!wasPlaying) return;
    let maxDur = 0;
    for (const stem of subs[pitchIdx].stems) {
      const buf = p2.buffers.get(stem.name);
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;
      const offset = Math.min(time, buf.duration);
      const gain = ctx.createGain();
      gain.gain.value = effectiveSubgroupGain(song, pitchIdx, stem.name);
      const splitter = ctx.createChannelSplitter(2);
      const aL = ctx.createAnalyser(); aL.fftSize = 64;
      const aR = ctx.createAnalyser(); aR.fftSize = 64;
      gain.connect(splitter);
      splitter.connect(aL, 0); splitter.connect(aR, 1);
      aL.connect(ctx.destination); aR.connect(ctx.destination);
      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, offset);
      p2.sourceNodes.set(stem.name, src);
      p2.gainNodes.set(stem.name, gain);
      p2.analysers.set(stem.name, [aL, aR]);
    }
    p2.duration = maxDur;
    p2.playing = true; p2.paused = false;
    function tick() {
      const pl = subgroupPlayers[key];
      if (!pl || !pl.playing || pl.paused) return;
      const elapsed = ctx.currentTime - pl.startTime;
      pl.currentTime = elapsed; pl.seekValue = elapsed;
      // Update subgroup peak levels
      for (const stem of (subs[pitchIdx]?.stems || [])) {
        const stKey = subgroupStemKey(song, pitchIdx, stem.name);
        const ans = pl.analysers.get(stem.name);
        if (ans) {
          const dL = new Uint8Array(ans[0].frequencyBinCount);
          const dR = new Uint8Array(ans[1].frequencyBinCount);
          ans[0].getByteTimeDomainData(dL);
          ans[1].getByteTimeDomainData(dR);
          let sL = 0, sR = 0;
          for (let j = 0; j < dL.length; j++) {
            sL += ((dL[j] - 128) / 128) ** 2;
            sR += ((dR[j] - 128) / 128) ** 2;
          }
          subgroupLevels = { ...subgroupLevels, [stKey]: { l: Math.sqrt(sL / dL.length), r: Math.sqrt(sR / dR.length) }};
          const curRmsL = Math.sqrt(sL / dL.length);
          const curRmsR = Math.sqrt(sR / dR.length);
          const prevPeak = subgroupPeaks[stKey] || { l: 0, r: 0 };
          subgroupPeaks = { ...subgroupPeaks, [stKey]: { l: Math.max(prevPeak.l, curRmsL), r: Math.max(prevPeak.r, curRmsR) }};
        }
      }
      const cv = subgroupWaveformCanvases[key];
      if (cv) drawSubgroupWaveform(cv, key);
      if (elapsed >= pl.duration) { stopSubgroup(key); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p2.animFrame = requestAnimationFrame(tick);
  }

  function handleSubSeekInput(e: Event, key: string) {
    const time = parseFloat((e.target as HTMLInputElement).value);
    const p = subgroupPlayers[key];
    if (p) { p.seekValue = time; p.currentTime = time; p.pauseOffset = time; }
  }
  function handleSubSeekChange(e: Event, key: string) {
    seekSubgroup(key, parseFloat((e.target as HTMLInputElement).value));
  }

  function subgroupSkipBack(key: string) {
    const p = subgroupPlayers[key];
    if (!p || !p.loaded || p.duration <= 0) return;
    seekSubgroup(key, Math.max(0, p.currentTime - 10));
  }
  function subgroupSkipForward(key: string) {
    const p = subgroupPlayers[key];
    if (!p || !p.loaded || p.duration <= 0) return;
    seekSubgroup(key, Math.min(p.duration, p.currentTime + 10));
  }

  async function handleDeleteSubgroup(song: string, pitchIdx: number) {
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx]) return;
    const pitch = subs[pitchIdx].pitch;
    if (!confirm(`Eliminar grupo de tono ${pitch > 0 ? '+' : ''}${pitch} semitonos?`)) return;
    try {
      await deletePitchSubgroup(song, pitch);
      // Remove from local state
      const remaining = [...subs];
      remaining.splice(pitchIdx, 1);
      if (remaining.length === 0) {
        const { [song]: _, ...rest } = pitchSubgroups;
        pitchSubgroups = rest;
      } else {
        pitchSubgroups = { ...pitchSubgroups, [song]: remaining };
      }
      showToast('Grupo eliminado', 'success');
    } catch (err: any) {
      showToast(`Error: ${err.message || 'unknown'}`, 'error');
    }
  }

  // ── Export / Delete group ──
  function handleExportGroup(song: string) {
    const stems = stemsForSong(song);
    for (const stem of stems) {
      const url = downloadUrl(song, stem.name);
      const a = document.createElement('a');
      a.href = url; a.download = stem.name; a.click();
    }
  }

  async function handleDeleteGroup(song: string) {
    if (!confirm(`Eliminar todo el grupo "${song}"?`)) return;
    try {
      await deleteSong(song);
      showToast('Grupo eliminado', 'success');
      onResultsChange();
    } catch (err: any) {
      showToast(`Error: ${err.message || 'unknown'}`, 'error');
    }
  }

  async function deleteStem(song: string, name: string) {
    if (!confirm(`Eliminar "${name}"?`)) return;
    try {
      await deleteStemApi(song, name);
      showToast(`"${name}" eliminado`, 'success');
      onResultsChange();
    } catch (err: any) {
      showToast(`Error: ${err.message || 'unknown'}`, 'error');
    }
  }

  // ── Upload player functions (standalone) ──
  function newUploadPlayer(id: string, name: string): UploadPlayer {
    return {
      id, name, status: 'uploading',
      audioCtx: null, playing: false, paused: false,
      currentTime: 0, duration: 0, seekValue: 0,
      sourceNode: null, gainNode: null, buffer: null,
      startTime: 0, pauseOffset: 0, animFrame: null,
      loaded: false, volume: 100,
    };
  }

  function handleDropZoneFile(f: File) {
    const id = crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
    const p = newUploadPlayer(id, f.name);
    uploadPlayers = [...uploadPlayers, p];
    uploadPitchAudio(f).then(() => {
      uploadPlayers = uploadPlayers.map(up => up.id === id ? { ...up, status: 'ready' as const } : up);
    }).catch((err) => {
      uploadPlayers = uploadPlayers.map(up => up.id === id ? { ...up, status: 'error' as const, errorMsg: err.message } : up);
    });
  }
  function handleDrop() { dragCounter = 0; }
  function handleDragOver(e: DragEvent) { e.preventDefault(); }
  function handleDropEvent(e: DragEvent) {
    e.preventDefault(); dragCounter = 0;
    const files = e.dataTransfer?.files;
    if (files) for (let i = 0; i < files.length; i++) handleDropZoneFile(files[i]);
  }
  function handleClick() {
    (document.getElementById('pitch-dropzone-input') as HTMLInputElement)?.click();
  }
  function handleInput(e: Event) {
    const input = e.target as HTMLInputElement;
    if (input.files) {
      for (let i = 0; i < input.files.length; i++) handleDropZoneFile(input.files[i]);
      input.value = '';
    }
  }

  function getUploadCtx(p: UploadPlayer): AudioContext {
    if (!p.audioCtx) p.audioCtx = new AudioContext();
    return p.audioCtx;
  }
  async function loadUploadBuffer(p: UploadPlayer) {
    if (p.loaded && p.buffer) return;
    const ctx = getUploadCtx(p);
    const resp = await fetch(pitchInputDownloadUrl(p.name));
    const buf = await resp.arrayBuffer();
    const audioBuf = await ctx.decodeAudioData(buf);
    p.buffer = audioBuf; p.duration = audioBuf.duration; p.loaded = true;
  }
  function stopUploadSource(p: UploadPlayer) {
    if (p.sourceNode) { try { p.sourceNode.stop(); } catch {} p.sourceNode.disconnect(); p.sourceNode = null; }
    if (p.gainNode) { p.gainNode.disconnect(); p.gainNode = null; }
    if (p.animFrame) { cancelAnimationFrame(p.animFrame); p.animFrame = null; }
  }
  function cleanupUploadPlayer(p: UploadPlayer) {
    stopUploadSource(p); p.audioCtx?.close(); p.audioCtx = null;
  }

  function getUploadPlayer(id: string): UploadPlayer | undefined {
    return uploadPlayers.find(p => p.id === id);
  }

  async function toggleUploadPlay(id: string) {
    const p = getUploadPlayer(id);
    if (!p) return;
    if (p.playing && !p.paused) { pauseUpload(id); return; }
    if (p.paused) { resumeUpload(id); return; }
    const ctx = getUploadCtx(p);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadUploadBuffer(p);
    stopUploadSource(p);
    const now = ctx.currentTime;
    p.startTime = now;
    const gain = ctx.createGain(); gain.gain.value = p.volume / 100; gain.connect(ctx.destination);
    const src = ctx.createBufferSource(); src.buffer = p.buffer; src.connect(gain); src.start(0);
    p.sourceNode = src; p.gainNode = gain; p.playing = true; p.paused = false;
    function tick() {
      const pl = getUploadPlayer(id);
      if (!pl || !pl.playing || pl.paused) return;
      pl.currentTime = pl.audioCtx!.currentTime - pl.startTime;
      pl.seekValue = pl.currentTime;
      if (pl.currentTime >= pl.duration) { stopUpload(id); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }
  function pauseUpload(id: string) {
    const p = getUploadPlayer(id);
    if (!p || !p.playing || p.paused) return;
    p.pauseOffset = p.audioCtx!.currentTime - p.startTime;
    p.audioCtx!.suspend(); p.paused = true;
  }
  function resumeUpload(id: string) {
    const p = getUploadPlayer(id);
    if (!p || !p.playing || !p.paused) return;
    p.audioCtx!.resume(); p.paused = false;
    function tick() {
      const pl = getUploadPlayer(id);
      if (!pl || !pl.playing || pl.paused) return;
      pl.currentTime = pl.audioCtx!.currentTime - pl.startTime;
      pl.seekValue = pl.currentTime;
      if (pl.currentTime >= pl.duration) { stopUpload(id); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }
  function stopUpload(id: string) {
    const p = getUploadPlayer(id);
    if (!p) return;
    stopUploadSource(p); p.audioCtx?.suspend();
    p.playing = false; p.paused = false; p.currentTime = 0; p.seekValue = 0; p.pauseOffset = 0;
  }
  function handleUploadSeekInput(e: Event, id: string) {
    const p = getUploadPlayer(id);
    if (p) p.seekValue = parseFloat((e.target as HTMLInputElement).value);
  }
  async function handleUploadSeekChange(e: Event, id: string) {
    const p = getUploadPlayer(id);
    if (!p || !p.buffer) return;
    const seekTo = parseFloat((e.target as HTMLInputElement).value);
    const wasPlaying = p.playing && !p.paused;
    stopUploadSource(p);
    const ctx = getUploadCtx(p);
    if (ctx.state === 'suspended') await ctx.resume();
    await loadUploadBuffer(p);
    const now = ctx.currentTime;
    p.startTime = now - seekTo; p.pauseOffset = seekTo; p.currentTime = seekTo; p.seekValue = seekTo;
    if (!wasPlaying) return;
    const gain = ctx.createGain(); gain.gain.value = p.volume / 100; gain.connect(ctx.destination);
    const src = ctx.createBufferSource(); src.buffer = p.buffer; src.connect(gain); src.start(0, seekTo);
    p.sourceNode = src; p.gainNode = gain; p.playing = true; p.paused = false;
    function tick() {
      const pl = getUploadPlayer(id);
      if (!pl || !pl.playing || pl.paused) return;
      pl.currentTime = pl.audioCtx!.currentTime - pl.startTime;
      pl.seekValue = pl.currentTime;
      if (pl.currentTime >= pl.duration) { stopUpload(id); return; }
      pl.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }
  function handleUploadVolume(e: Event, id: string) {
    const p = getUploadPlayer(id);
    if (!p) return;
    p.volume = parseInt((e.target as HTMLInputElement).value);
    if (p.gainNode) p.gainNode.gain.value = p.volume / 100;
  }

  async function handleDeleteUpload(id: string) {
    const p = getUploadPlayer(id);
    if (!p) return;
    if (!confirm(`Eliminar "${p.name}"?`)) return;
    try { await deletePitchUpload(p.name); } catch {}
    cleanupUploadPlayer(p);
    uploadPlayers = uploadPlayers.filter(pl => pl.id !== id);
  }

  // ── Waveform ──
  function darkenColor(hex: string, amount: number): string {
    if (!hex || hex === '') return '#333';
    const num = parseInt(hex.replace('#', ''), 16);
    if (isNaN(num)) return '#333';
    const r = Math.max(0, (num >> 16) - amount);
    const g = Math.max(0, ((num >> 8) & 0xff) - amount);
    const b = Math.max(0, (num & 0xff) - amount);
    return `rgb(${r}, ${g}, ${b})`;
  }

  async function computeWavePeaks(song: string): Promise<number[]> {
    // Return cached peaks if already computed
    if (wavePeaksCache[song]) return wavePeaksCache[song];

    const stems = stemsForSong(song);
    if (stems.length === 0) return [];

    try {
      const url = downloadUrl(song, stems[0].name);
      const resp = await fetch(url);
      const arrayBuf = await resp.arrayBuffer();
      const audioCtx = new OfflineAudioContext(1, 1, 44100);
      const audioBuf = await audioCtx.decodeAudioData(arrayBuf);
      const channel = audioBuf.getChannelData(0);

      // Compute 2000 peak values for high-res waveform
      const PEAK_RES = 2000;
      const steps = Math.max(1, Math.floor(channel.length / PEAK_RES));
      const data: number[] = [];
      for (let i = 0; i < PEAK_RES; i++) {
        let max = 0;
        const start = i * steps;
        const end = Math.min(start + steps, channel.length);
        for (let j = start; j < end; j++) max = Math.max(max, Math.abs(channel[j]));
        data.push(max);
      }
      audioCtx.close();
      wavePeaksCache = { ...wavePeaksCache, [song]: data };
      return data;
    } catch {
      // Fallback synthetic peaks
      const data: number[] = [];
      for (let i = 0; i < 2000; i++) {
        data.push(((Math.abs((song.length + i * 31)) % 80) / 100) * 0.8 + 0.1);
      }
      wavePeaksCache = { ...wavePeaksCache, [song]: data };
      return data;
    }
  }

  async function drawWaveform(canvas: HTMLCanvasElement, song: string) {
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
    const w = canvas.clientWidth * dpr;
    const h = canvas.clientHeight * dpr;
    if (w <= 0 || h <= 0) return;
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, w, h);

    // Colors: accent for played, darkened accent for unplayed
    const accentCol = accent();
    const dimAccentCol = darkenColor(accentCol, 60);

    // Line color: white on dark, black on light
    const isLight = typeof document !== 'undefined' && document.body.classList.contains('light-theme');
    const lineCol = isLight ? '#000' : '#fff';

    // Get playback progress (or drag preview when scrubbing)
    const p = groupPlayers[song];
    const isDragging = dragging[song];
    const rawProgress = p && p.loaded && p.duration > 0 ? (p.currentTime / p.duration) : 0;
    const progress = isDragging ? (dragPreview[song] ?? rawProgress) : rawProgress;

    // Get cached peaks or compute them (first call only)
    const peaks = wavePeaksCache[song];
    if (!peaks || peaks.length === 0) {
      // Schedule async computation, draw placeholder
      ctx.fillStyle = dimAccentCol;
      ctx.fillRect(0, 0, w, h);
      computeWavePeaks(song);
      return;
    }

    // Interpolate peaks to current canvas width — use barW to avoid gaps
    const splitX = Math.round(progress * w);
    const barW = Math.max(1, Math.floor(w / peaks.length));

    // Draw unplayed portion (right) — darkened accent
    ctx.fillStyle = dimAccentCol;
    for (let i = splitX; i < w; i += barW) {
      const peakIdx = Math.floor((i / w) * peaks.length);
      const peak = peaks[Math.min(peakIdx, peaks.length - 1)];
      const barH = Math.max(1, peak * h);
      ctx.fillRect(i, (h - barH) / 2, barW, barH);
    }

    // Draw played portion (left) — accent color
    ctx.fillStyle = accentCol;
    for (let i = 0; i < splitX; i += barW) {
      const peakIdx = Math.floor((i / w) * peaks.length);
      const peak = peaks[Math.min(peakIdx, peaks.length - 1)];
      const barH = Math.max(1, peak * h);
      ctx.fillRect(i, (h - barH) / 2, barW, barH);
    }

    // Draw vertical playback line (always visible during drag, even at edges)
    const showLine = isDragging || (progress > 0 && progress < 1);
    if (showLine) {
      ctx.strokeStyle = lineCol;
      ctx.lineWidth = Math.max(1, 2 * dpr);
      ctx.beginPath();
      ctx.moveTo(splitX, 0);
      ctx.lineTo(splitX, h);
      ctx.stroke();
    }
  }

  function waveformAction(node: HTMLCanvasElement, song: string) {
    waveformCanvases[song] = node;
    drawWaveform(node, song);
    // Start computing peaks asynchronously if not already cached
    if (!wavePeaksCache[song]) {
      computeWavePeaks(song).then(() => drawWaveform(node, song));
    }
  }

  // ── Waveform drag-to-seek ──
  function getWaveformFrac(e: MouseEvent, song: string): number {
    const canvas = waveformCanvases[song];
    if (!canvas) return 0;
    const rect = canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    return Math.max(0, Math.min(1, x / rect.width));
  }

  function handleWaveformMouseDown(e: MouseEvent, song: string) {
    e.preventDefault();
    const frac = getWaveformFrac(e, song);
    dragging = { ...dragging, [song]: true };
    dragPreview = { ...dragPreview, [song]: frac };
    const cv = waveformCanvases[song];
    if (cv) drawWaveform(cv, song);
  }

  function handleWaveformMouseMove(e: MouseEvent, song: string) {
    if (!dragging[song]) return;
    e.preventDefault();
    const frac = getWaveformFrac(e, song);
    dragPreview = { ...dragPreview, [song]: frac };
    const cv = waveformCanvases[song];
    if (cv) drawWaveform(cv, song);
  }

  function handleWaveformMouseUp(e: MouseEvent, song: string) {
    if (!dragging[song]) return;
    dragging = { ...dragging, [song]: false };
    const frac = dragPreview[song] ?? 0;
    const p = groupPlayers[song];
    if (p && p.loaded && p.duration > 0) {
      seekGroup(song, frac * p.duration);
    }
  }

  function handleWaveformMouseLeave(e: MouseEvent, song: string) {
    if (dragging[song]) {
      handleWaveformMouseUp(e, song);
    }
  }

  // Global mouseup to catch drags that escape the canvas
  function handleGlobalMouseUp() {
    for (const song of Object.keys(dragging)) {
      if (dragging[song]) {
        dragging = { ...dragging, [song]: false };
        const frac = dragPreview[song] ?? 0;
        const p = groupPlayers[song];
        if (p && p.loaded && p.duration > 0) {
          seekGroup(song, frac * p.duration);
        }
      }
    }
  }

  // Attach global mouseup on mount, detach on destroy

  // ── Subgroup waveform (same as main group) ──
  async function computeSubgroupWavePeaks(key: string): Promise<number[]> {
    if (subgroupWavePeaksCache[key]) return subgroupWavePeaksCache[key];
    const parts = key.split('::');
    const song = parts[0];
    const pitchIdx = parseInt(parts[1]);
    const subs = pitchSubgroups[song];
    if (!subs || !subs[pitchIdx] || subs[pitchIdx].stems.length === 0) return [];
    try {
      const url = subs[pitchIdx].stems[0].path;
      const resp = await fetch(url);
      const arrayBuf = await resp.arrayBuffer();
      const audioCtx = new OfflineAudioContext(1, 1, 44100);
      const audioBuf = await audioCtx.decodeAudioData(arrayBuf);
      const channel = audioBuf.getChannelData(0);
      const PEAK_RES = 2000;
      const steps = Math.max(1, Math.floor(channel.length / PEAK_RES));
      const data: number[] = [];
      for (let i = 0; i < PEAK_RES; i++) {
        let max = 0;
        const start = i * steps;
        const end = Math.min(start + steps, channel.length);
        for (let j = start; j < end; j++) max = Math.max(max, Math.abs(channel[j]));
        data.push(max);
      }
      audioCtx.close();
      subgroupWavePeaksCache = { ...subgroupWavePeaksCache, [key]: data };
      return data;
    } catch {
      const data: number[] = [];
      for (let i = 0; i < 2000; i++) {
        data.push(((Math.abs((key.length + i * 31)) % 80) / 100) * 0.8 + 0.1);
      }
      subgroupWavePeaksCache = { ...subgroupWavePeaksCache, [key]: data };
      return data;
    }
  }

  async function drawSubgroupWaveform(canvas: HTMLCanvasElement, key: string) {
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
    const w = canvas.clientWidth * dpr;
    const h = canvas.clientHeight * dpr;
    if (w <= 0 || h <= 0) return;
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, w, h);
    const accentCol = accent();
    const dimAccentCol = darkenColor(accentCol, 60);
    const isLight = typeof document !== 'undefined' && document.body.classList.contains('light-theme');
    const lineCol = isLight ? '#000' : '#fff';
    const p = subgroupPlayers[key];
    const isDragging = subgroupDragging[key];
    const rawProgress = p && p.loaded && p.duration > 0 ? (p.currentTime / p.duration) : 0;
    const progress = isDragging ? (subgroupDragPreview[key] ?? rawProgress) : rawProgress;
    const peaks = subgroupWavePeaksCache[key];
    if (!peaks || peaks.length === 0) {
      ctx.fillStyle = dimAccentCol;
      ctx.fillRect(0, 0, w, h);
      computeSubgroupWavePeaks(key);
      return;
    }
    const splitX = Math.round(progress * w);
    const barW = Math.max(1, Math.floor(w / peaks.length));
    ctx.fillStyle = dimAccentCol;
    for (let i = splitX; i < w; i += barW) {
      const peakIdx = Math.floor((i / w) * peaks.length);
      const peak = peaks[Math.min(peakIdx, peaks.length - 1)];
      const barH = Math.max(1, peak * h);
      ctx.fillRect(i, (h - barH) / 2, barW, barH);
    }
    ctx.fillStyle = accentCol;
    for (let i = 0; i < splitX; i += barW) {
      const peakIdx = Math.floor((i / w) * peaks.length);
      const peak = peaks[Math.min(peakIdx, peaks.length - 1)];
      const barH = Math.max(1, peak * h);
      ctx.fillRect(i, (h - barH) / 2, barW, barH);
    }
    const showLine = isDragging || (progress > 0 && progress < 1);
    if (showLine) {
      ctx.strokeStyle = lineCol;
      ctx.lineWidth = Math.max(1, 2 * dpr);
      ctx.beginPath();
      ctx.moveTo(splitX, 0);
      ctx.lineTo(splitX, h);
      ctx.stroke();
    }
  }

  function subgroupWaveformAction(node: HTMLCanvasElement, key: string) {
    subgroupWaveformCanvases[key] = node;
    drawSubgroupWaveform(node, key);
    if (!subgroupWavePeaksCache[key]) {
      computeSubgroupWavePeaks(key).then(() => drawSubgroupWaveform(node, key));
    }
  }

  function getSubgroupWaveformFrac(e: MouseEvent, key: string): number {
    const canvas = subgroupWaveformCanvases[key];
    if (!canvas) return 0;
    const rect = canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    return Math.max(0, Math.min(1, x / rect.width));
  }

  function handleSubgroupWaveformMouseDown(e: MouseEvent, key: string) {
    e.preventDefault();
    const frac = getSubgroupWaveformFrac(e, key);
    subgroupDragging = { ...subgroupDragging, [key]: true };
    subgroupDragPreview = { ...subgroupDragPreview, [key]: frac };
    const cv = subgroupWaveformCanvases[key];
    if (cv) drawSubgroupWaveform(cv, key);
  }

  function handleSubgroupWaveformMouseMove(e: MouseEvent, key: string) {
    if (!subgroupDragging[key]) return;
    e.preventDefault();
    const frac = getSubgroupWaveformFrac(e, key);
    subgroupDragPreview = { ...subgroupDragPreview, [key]: frac };
    const cv = subgroupWaveformCanvases[key];
    if (cv) drawSubgroupWaveform(cv, key);
  }

  function handleSubgroupWaveformMouseUp(e: MouseEvent, key: string) {
    if (!subgroupDragging[key]) return;
    subgroupDragging = { ...subgroupDragging, [key]: false };
    const frac = subgroupDragPreview[key] ?? 0;
    const p = subgroupPlayers[key];
    if (p && p.loaded && p.duration > 0) {
      seekSubgroup(key, frac * p.duration);
    }
  }

  function handleSubgroupWaveformMouseLeave(e: MouseEvent, key: string) {
    if (subgroupDragging[key]) {
      handleSubgroupWaveformMouseUp(e, key);
    }
  }

  function handleSubgroupGlobalMouseUp() {
    for (const key of Object.keys(subgroupDragging)) {
      if (subgroupDragging[key]) {
        subgroupDragging = { ...subgroupDragging, [key]: false };
        const frac = subgroupDragPreview[key] ?? 0;
        const p = subgroupPlayers[key];
        if (p && p.loaded && p.duration > 0) {
          seekSubgroup(key, frac * p.duration);
        }
      }
    }
  }

  // ── Format / Helpers ──
  function fmtTime(sec: number | undefined): string {
    if (sec == null || !isFinite(sec)) return '0:00';
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  function accent(): string {
    if (typeof document === 'undefined') return '#6c5ce7';
    return getComputedStyle(document.body).getPropertyValue('--accent').trim() || '#6c5ce7';
  }

  function showToast(message: string, type: 'success' | 'error') {
    toast = { message, type };
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = null; }, 3000);
  }

  function outputDownloadUrl(stem: ResultStem): string {
    return downloadUrl(stem.song, stem.name);
  }

  // ── Cleanup ──
  onDestroy(() => {
    if (toastTimer) clearTimeout(toastTimer);
    window.removeEventListener('mouseup', handleGlobalMouseUp);
    window.removeEventListener('mouseup', handleSubgroupGlobalMouseUp);
    // Do NOT abort abortController — keeps background fetch alive
    // Do NOT close AudioContexts for groups/subgroups — keeps playback alive
    for (const p of uploadPlayers) cleanupUploadPlayer(p);
  });
</script>

<div class="pitch-page">
  <!-- ═══════ Output groups (from separator results) ═══════ -->
  {#if results.length > 0}
    <section class="output-groups-section">
      <h3 class="section-title">Grupos de salida</h3>
      <p class="section-desc">Archivos generados por el separador — cambio de tono disponible</p>
      <div class="output-groups-list">
        {#each groupSongs as song}
          {@const stems = stemsForSong(song)}
          {#if stems.length > 0}
            <div class="output-group-card">
              <!-- ── Group header with combined player ── -->
              <div class="output-group-header">
                <span class="output-song-name">📁 {song}</span>
                <span class="output-stem-count">{stems.length} pistas</span>
              </div>

              <!-- ── Combined player controls ── -->
              <div class="group-player-bar">
                <div class="playback-controls">
                  <button class="ctrl-btn skip-btn" onclick={() => skipBack(song)}
                    disabled={!groupPlayers[song]?.loaded} title="-10 segundos">{@html IconSkipBack}</button>
                  <button class="ctrl-btn play-btn" onclick={() => playGroup(song)}
                    disabled={groupPlayers[song]?.playing && !groupPlayers[song]?.paused}
                    title={groupPlayers[song]?.playing && !groupPlayers[song]?.paused ? 'Reproduciendo' : 'Reproducir todo'}>▶</button>
                  <button class="ctrl-btn pause-btn" onclick={() => pauseGroup(song)}
                    disabled={!groupPlayers[song]?.playing || groupPlayers[song]?.paused} title="Pausa">⏸</button>
                  <button class="ctrl-btn stop-btn" onclick={() => stopGroup(song)}
                    disabled={!groupPlayers[song]?.playing && !groupPlayers[song]?.paused} title="Parar">⏹</button>
                  <button class="ctrl-btn skip-btn" onclick={() => skipForward(song)}
                    disabled={!groupPlayers[song]?.loaded} title="+10 segundos">{@html IconSkipForward}</button>
                </div>
                <div class="seek-area">
                  <span class="time-display">{fmtTime(groupPlayers[song]?.currentTime)}/{fmtTime(groupPlayers[song]?.duration)}</span>
                </div>
                <div class="vol-slider-wrap">
                  <label class="vol-label-small">Vol:</label>
                  <input type="range" min="0" max="100"
                    value={100}
                    oninput={(e) => {
                      for (const stem of stems) setVolume(song, stem.name, parseInt((e.target as HTMLInputElement).value));
                    }}
                    class="vol-slider" title="Volumen general" />
                </div>
                <div class="group-actions">
                  <button class="song-btn export-btn" onclick={() => handleExportGroup(song)} title="Descargar todo">⬇</button>
                  <button class="song-btn delete-btn" onclick={() => handleDeleteGroup(song)} title="Eliminar grupo">🗑</button>
                </div>
              </div>

              <!-- ── Waveform as seek bar ── -->
              <div class="waveform-seek-row">
                <canvas class="waveform-seek" width="200" height={WAVEFORM_H}
                  use:waveformAction={song}
                  onmousedown={(e) => handleWaveformMouseDown(e, song)}
                  onmousemove={(e) => handleWaveformMouseMove(e, song)}
                  onmouseup={(e) => handleWaveformMouseUp(e, song)}
                  onmouseleave={(e) => handleWaveformMouseLeave(e, song)}
                  role="slider" tabindex="0"
                  aria-label="Barra de reproducción" />
              </div>

              <!-- ── Individual stem rows (mute/solo/volume) ── -->
              <div class="output-stems">
                {#each stems as stem}
                  {@const state = getStemState(song, stem.name)}
                  {@const sLevel = stemLevels[stemStateKey(song, stem.name)] || { l: 0, r: 0 }}
                  {@const pLevel = stemPeaks[stemStateKey(song, stem.name)] || { l: 0, r: 0 }}
                  <div class="stem-row" class:muted={state.muted}>
                    <span class="stem-emoji">{stemEmoji(stem.stemType)}</span>
                    <div class="stem-left-controls">
                      <button class="stem-btn mute-btn" class:active={state.muted}
                        onclick={() => toggleMute(song, stem.name)} title="Silenciar">M</button>
                      <button class="stem-btn solo-btn" class:active={state.solo}
                        onclick={() => toggleSolo(song, stem.name)} title="Solo">S</button>
                      <input type="range" min="0" max="100" value={state.volume}
                        oninput={(e) => handleVolumeChange(e, song, stem.name)}
                        class="stem-vol-slider" title="Volumen" />
                    </div>
                    <span class="stem-name" title={stem.name}>{stem.stemType || stem.name}</span>
                    <div class="peak-meter">
                      <div class="peak-db-top">L: {toDbStr(pLevel.l)} dB &nbsp; R: {toDbStr(pLevel.r)} dB</div>
                      <div class="peak-bar-container"><div class="peak-bar peak-l" style="width:{dbToPct(rmsToDb(sLevel.l))}%"></div><div class="peak-marker" style="left:{dbToPct(rmsToDb(pLevel.l))}%"></div></div>
                      <div class="peak-bar-container"><div class="peak-bar peak-r" style="width:{dbToPct(rmsToDb(sLevel.r))}%"></div><div class="peak-marker" style="left:{dbToPct(rmsToDb(pLevel.r))}%"></div></div>
                      <div class="peak-db-bottom">
                        <span>-60</span><span>-40</span><span>-20</span><span>-12</span><span>0 dB</span>
                      </div>
                    </div>
                    <div class="stem-actions">
                      <a class="song-btn export-btn" href={outputDownloadUrl(stem)} download={stem.name} title="Descargar">⬇</a>
                      <button class="song-btn delete-btn" onclick={() => deleteStem(song, stem.name)} title="Eliminar">🗑</button>
                    </div>
                  </div>
                {/each}
              </div>

              <!-- ── Pitch shift controls ── -->
              <div class="pitch-control-row">
                <label class="pitch-label">Tono:</label>
                <input type="range" min="-12" max="12" step="1"
                  value={getPitchValue(song)}
                  oninput={(e) => { pitchValues = { ...pitchValues, [song]: parseFloat((e.target as HTMLInputElement).value) }; }}
                  class="pitch-slider" />
                <span class="pitch-value">{getPitchValue(song) > 0 ? '+' : ''}{getPitchValue(song)}</span>
                <button class="pitch-btn" onclick={() => handlePitch(song)}
                  disabled={pitchProcessing[song] || getPitchValue(song) === 0}>
                  {pitchProcessing[song] ? '⏳' : '🎵 Cambiar tono'}
                </button>
              </div>

              <!-- ── Pitch subgroups ── -->
              {#if pitchSubgroups[song] && pitchSubgroups[song].length > 0}
                <div class="pitch-subgroups-section">
                  <h4 class="subgroups-title">Subgrupos de tono</h4>
                  {#each pitchSubgroups[song] as subs, idx}
                    {@const subKey = getSubgroupKey(song, idx)}
                    {@const subStems = subs.stems}
                    <div class="pitch-subgroup-card">
                      <div class="subgroup-header">
                        <span class="subgroup-pitch-label">Tono: {subs.pitch > 0 ? '+' : ''}{subs.pitch}</span>
                        <span class="output-stem-count">{subStems.length} pistas</span>
                      </div>

                      <!-- ── Subgroup player bar (same as main group) ── -->
                      <div class="group-player-bar">
                        <div class="playback-controls">
                          <button class="ctrl-btn skip-btn" onclick={() => subgroupSkipBack(subKey)}
                            disabled={!subgroupPlayers[subKey]?.loaded} title="-10 segundos">{@html IconSkipBack}</button>
                          <button class="ctrl-btn play-btn" onclick={() => playSubgroup(song, idx)}
                            disabled={subgroupPlayers[subKey]?.playing && !subgroupPlayers[subKey]?.paused}
                            title={subgroupPlayers[subKey]?.playing && !subgroupPlayers[subKey]?.paused ? 'Reproduciendo' : 'Reproducir'}>▶</button>
                          <button class="ctrl-btn pause-btn" onclick={() => pauseSubgroup(subKey)}
                            disabled={!subgroupPlayers[subKey]?.playing || subgroupPlayers[subKey]?.paused} title="Pausa">⏸</button>
                          <button class="ctrl-btn stop-btn" onclick={() => stopSubgroup(subKey)}
                            disabled={!subgroupPlayers[subKey]?.playing && !subgroupPlayers[subKey]?.paused} title="Parar">⏹</button>
                          <button class="ctrl-btn skip-btn" onclick={() => subgroupSkipForward(subKey)}
                            disabled={!subgroupPlayers[subKey]?.loaded} title="+10 segundos">{@html IconSkipForward}</button>
                        </div>
                        <div class="seek-area">
                          <span class="time-display">{fmtTime(subgroupPlayers[subKey]?.currentTime)}/{fmtTime(subgroupPlayers[subKey]?.duration)}</span>
                        </div>
                        <div class="vol-slider-wrap">
                          <label class="vol-label-small">Vol:</label>
                          <input type="range" min="0" max="100" value={100}
                            oninput={(e) => {
                              for (const sstem of subStems) setSubgroupVolume(song, idx, sstem.name, parseInt((e.target as HTMLInputElement).value));
                            }}
                            class="vol-slider" title="Volumen general" />
                        </div>
                      </div>

                      <!-- ── Subgroup waveform seek ── -->
                      <div class="waveform-seek-row">
                        <canvas class="waveform-seek" width="200" height={WAVEFORM_H}
                          use:subgroupWaveformAction={subKey}
                          onmousedown={(e) => handleSubgroupWaveformMouseDown(e, subKey)}
                          onmousemove={(e) => handleSubgroupWaveformMouseMove(e, subKey)}
                          onmouseup={(e) => handleSubgroupWaveformMouseUp(e, subKey)}
                          onmouseleave={(e) => handleSubgroupWaveformMouseLeave(e, subKey)}
                          role="slider" tabindex="0"
                          aria-label="Barra de reproducción" />
                      </div>

                      <!-- ── Subgroup stems (full controls like main group) ── -->
                      <div class="output-stems">
                        {#each subStems as sstem}
                          {@const sgState = getSubgroupStemState(song, idx, sstem.name)}
                          {@const sLevel = subgroupLevels[subgroupStemKey(song, idx, sstem.name)] || { l: 0, r: 0 }}
                          {@const pLevel = subgroupPeaks[subgroupStemKey(song, idx, sstem.name)] || { l: 0, r: 0 }}
                          <div class="stem-row" class:muted={sgState.muted}>
                            <span class="stem-emoji">{stemEmoji(sstem.stemType)}</span>
                            <div class="stem-left-controls">
                              <button class="stem-btn mute-btn" class:active={sgState.muted}
                                onclick={() => toggleSubgroupMute(song, idx, sstem.name)} title="Silenciar">M</button>
                              <button class="stem-btn solo-btn" class:active={sgState.solo}
                                onclick={() => toggleSubgroupSolo(song, idx, sstem.name)} title="Solo">S</button>
                              <input type="range" min="0" max="100" value={sgState.volume}
                                oninput={(e) => handleSubgroupVolumeChange(e, song, idx, sstem.name)}
                                class="stem-vol-slider" title="Volumen" />
                            </div>
                            <span class="stem-name" title={sstem.name}>{sstem.stemType || sstem.name}</span>
                            <div class="peak-meter">
                              <div class="peak-db-top">L: {toDbStr(pLevel.l)} dB &nbsp; R: {toDbStr(pLevel.r)} dB</div>
                              <div class="peak-bar-container"><div class="peak-bar peak-l" style="width:{dbToPct(rmsToDb(sLevel.l))}%"></div><div class="peak-marker" style="left:{dbToPct(rmsToDb(pLevel.l))}%"></div></div>
                              <div class="peak-bar-container"><div class="peak-bar peak-r" style="width:{dbToPct(rmsToDb(sLevel.r))}%"></div><div class="peak-marker" style="left:{dbToPct(rmsToDb(pLevel.r))}%"></div></div>
                              <div class="peak-db-bottom">
                                <span>-60</span><span>-40</span><span>-20</span><span>-12</span><span>0 dB</span>
                              </div>
                            </div>
                            <div class="stem-actions">
                              <a class="song-btn export-btn" href={sstem.path} download={sstem.name} title="Descargar">⬇</a>
                            </div>
                          </div>
                        {/each}
                      </div>
                      <div class="subgroup-actions">
                        <button class="song-btn delete-btn" onclick={() => handleDeleteSubgroup(song, idx)}
                          title="Eliminar este subgrupo">🗑 Eliminar grupo</button>
                      </div>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        {/each}
      </div>
    </section>
  {/if}

  <!-- ═══════ Dropzone ═══════ -->
  <section class="pitch-dropzone-section">
    <h3 class="section-title">Subir audio para cambio de tono</h3>
    <p class="section-desc">Los archivos se guardan en la carpeta input_rubberband</p>
    <div class="pitch-dropzone"
      ondragover={handleDragOver}
      ondrop={handleDropEvent}
      onclick={handleClick}
      role="button" tabindex="0">
      <span class="pitch-dropzone-icon">{@html IconUpload}</span>
      <span class="pitch-dropzone-text">Arrastra archivos aquí o haz clic</span>
      <span class="pitch-dropzone-hint">WAV, MP3, FLAC, OGG, M4A</span>
    </div>
    <input id="pitch-dropzone-input" type="file" hidden accept="audio/*" multiple onchange={handleInput} />
  </section>

  <!-- ═══════ Uploaded files with players ═══════ -->
  {#if uploadPlayers.length > 0}
    <section class="pitch-players-section">
      <h3 class="section-title">Archivos subidos ({uploadPlayers.length})</h3>
      <div class="pitch-players-list">
        {#each uploadPlayers as p (p.id)}
          <div class="song-group" class:loading={p.status === 'uploading'}>
            {#if p.status === 'uploading'}
              <div class="song-header">
                <h3 class="song-name">📤 {p.name}</h3>
                <span class="upload-status">Subiendo…</span>
              </div>
            {:else if p.status === 'error'}
              <div class="song-header">
                <h3 class="song-name error">⚠️ {p.name}</h3>
                <span class="upload-status error">Error: {p.errorMsg}</span>
                <button class="song-btn delete-btn" onclick={() => handleDeleteUpload(p.id)} title="Eliminar">🗑</button>
              </div>
            {:else}
              <div class="song-header">
                <h3 class="song-name">🎵 {p.name}</h3>
                <div class="playback-controls">
                  <button class="ctrl-btn play-btn" onclick={() => toggleUploadPlay(p.id)}
                    disabled={p.playing && !p.paused}
                    title={p.playing && !p.paused ? 'Reproduciendo' : 'Reproducir'}>▶</button>
                  <button class="ctrl-btn pause-btn" onclick={() => pauseUpload(p.id)}
                    disabled={!p.playing || p.paused} title="Pausa">⏸</button>
                  <button class="ctrl-btn stop-btn" onclick={() => stopUpload(p.id)}
                    disabled={!p.playing && !p.paused} title="Parar">⏹</button>
                </div>
                <div class="seek-area">
                  <input type="range" min="0" max={p.duration || 100} step="0.1"
                    value={p.seekValue || 0}
                    disabled={!p.loaded}
                    oninput={(e) => handleUploadSeekInput(e, p.id)}
                    onchange={(e) => handleUploadSeekChange(e, p.id)}
                    class="seek-slider" title="Buscar" />
                  <span class="time-display">{fmtTime(p.currentTime)}/{fmtTime(p.duration)}</span>
                </div>
                <div class="song-actions">
                  <a class="song-btn export-btn" href={pitchInputDownloadUrl(p.name)} download={p.name} title="Descargar">⬇</a>
                  <button class="song-btn delete-btn" onclick={() => handleDeleteUpload(p.id)} title="Eliminar">🗑</button>
                </div>
              </div>
              <div class="stem-controls">
                <div class="vol-slider-wrap">
                  <label class="vol-label-small">Vol:</label>
                  <input type="range" min="0" max="100" value={p.volume}
                    oninput={(e) => handleUploadVolume(e, p.id)} class="vol-slider" title="Volumen" />
                  <span class="vol-label">{p.volume}</span>
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </section>
  {/if}
</div>

{#if toast}
  <div class="toast {toast.type}">{toast.message}</div>
{/if}

<style>
  .pitch-page {
    width: 100%;
    max-width: 900px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
    padding: 1rem;
  }

  .section-title {
    margin: 0 0 0.5rem 0;
    font-size: 1rem;
    font-weight: 700;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .section-desc {
    margin: 0 0 1rem 0;
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  /* ── Output groups ── */
  .output-groups-section { width: 100%; }
  .output-groups-list { display: flex; flex-direction: column; gap: 0.75rem; }
  .output-group-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 0.75rem 1rem;
  }
  .output-group-header {
    display: flex; align-items: center; gap: 0.5rem;
    padding-bottom: 0.4rem; border-bottom: 1px solid var(--border);
    margin-bottom: 0.5rem;
  }
  .output-song-name {
    font-size: 0.9rem; font-weight: 600; color: var(--accent-light);
    flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .output-stem-count { font-size: 0.7rem; color: var(--text-muted); flex-shrink: 0; }

  /* ── Group player bar ── */
  .group-player-bar {
    display: flex; align-items: center; gap: 0.5rem;
    margin-bottom: 0.5rem; flex-wrap: wrap;
  }
  .group-actions { display: flex; gap: 0.3rem; flex-shrink: 0; margin-left: auto; padding-left: 0.5rem; border-left: 1px solid var(--border-light); }

  /* ── Stem rows (like ResultsPanel) ── */
  .output-stems { display: flex; flex-direction: column; gap: 0.2rem; margin-bottom: 0.5rem; }
  .stem-row {
    display: flex; align-items: center; gap: 0.3rem;
    padding: 0.25rem 0.5rem;
    border-radius: 6px;
    font-size: 0.8rem;
    transition: background 0.15s;
  }
  .stem-row:hover { background: var(--bg-hover); }
  .stem-row.muted { opacity: 0.5; }
  .stem-emoji { font-size: 0.9rem; width: 1.5em; text-align: center; flex-shrink: 0; }
  .stem-name { width: 100px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; margin: 0 0.3rem; }
  .stem-left-controls {
    display: flex; align-items: center; gap: 0.2rem;
    flex-shrink: 0;
  }
  .stem-actions {
    display: flex; align-items: center; gap: 0.25rem;
    flex-shrink: 0;
    margin-left: 0.3rem;
    padding-left: 0.5rem;
    border-left: 1px solid var(--border-light);
  }

  /* ── Peak meters per stem (gordos, flex-grow, con dB) ── */
  .peak-meter {
    display: flex; flex-direction: column; gap: 1px;
    flex: 1;
    min-width: 120px;
    margin: 0 0.5rem;
  }
  .peak-db-top {
    font-size: 0.6rem;
    color: var(--text-secondary);
    font-family: 'Courier New', monospace;
    font-weight: 600;
    white-space: nowrap;
    margin-bottom: 1px;
    letter-spacing: -0.3px;
  }
  .peak-bar-container {
    position: relative;
    width: 100%; height: 14px;
    background: var(--bg-hover);
    border-radius: 3px;
    overflow: hidden;
    border: 1px solid var(--border-light);
  }
  .peak-bar {
    height: 100%;
    border-radius: 2px;
    transition: width 0.05s ease;
  }
  .peak-l { background: linear-gradient(90deg, #4caf50, #8bc34a); }
  .peak-r { background: linear-gradient(90deg, #2196f3, #64b5f6); }
  .peak-marker {
    position: absolute;
    top: 0; bottom: 0;
    width: 2px;
    background: #fff;
    transform: translateX(-1px);
    pointer-events: none;
    z-index: 1;
    transition: left 0.05s ease;
  }
  .peak-db-bottom {
    display: flex; justify-content: space-between;
    font-size: 0.55rem;
    color: var(--text-muted);
    font-family: 'Courier New', monospace;
    margin-top: 1px;
    padding: 0 2px;
  }
  .stem-controls {
    display: flex; align-items: center; gap: 0.3rem;
    flex-shrink: 0;
  }
  .stem-btn {
    width: 22px; height: 22px; border-radius: 3px;
    border: 1px solid var(--border-light);
    background: transparent; color: var(--text-secondary);
    font-size: 0.6rem; font-weight: 700;
    cursor: pointer; padding: 0;
    display: flex; align-items: center; justify-content: center;
  }
  .stem-btn.active {
    background: var(--accent); color: #fff; border-color: var(--accent);
  }
  .mute-btn.active { background: #f44336; border-color: #f44336; }
  .solo-btn.active { background: #ff9800; border-color: #ff9800; }
  .stem-vol-slider {
    -webkit-appearance: none; appearance: none;
    width: 80px; height: 4px;
    border-radius: 3px; background: var(--bg-hover);
    outline: none; cursor: pointer;
  }
  .stem-vol-slider::-webkit-slider-thumb {
    -webkit-appearance: none; appearance: none;
    width: 12px; height: 12px; border-radius: 50%;
    background: var(--accent); cursor: pointer; border: 2px solid #0a0a14;
  }
  .stem-vol-label { font-size: 0.65rem; color: var(--text-muted); min-width: 2em; text-align: right; }

  /* ── Waveform ── */
  .waveform-row { display: none; }
  .waveform-mini { display: none; }

  .waveform-seek-row {
    width: 100%;
    margin-bottom: 0.4rem;
    cursor: pointer;
    border-radius: 4px;
    overflow: hidden;
  }
  .waveform-seek-row:hover {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .waveform-seek {
    display: block;
    width: 100%;
    height: 80px;
    background: #0a0a14;
    border-radius: 4px;
    cursor: grab;
  }
  .waveform-seek:active {
    cursor: grabbing;
  }
  .light-theme .waveform-seek {
    background: #e8e8f0;
  }

  .playback-controls { display: flex; gap: 0.3rem; flex-shrink: 0; }
  .ctrl-btn {
    width: 36px; height: 36px;
    border-radius: 8px;
    border: none; font-size: 0.9rem;
    cursor: pointer; display: flex; align-items: center; justify-content: center;
    transition: background 0.2s, transform 0.1s, opacity 0.2s;
    flex-shrink: 0; padding: 0;
    font-family: 'Segoe UI', system-ui, sans-serif;
  }
  .ctrl-btn:active:not(:disabled) { transform: scale(0.93); }
  .ctrl-btn:disabled { opacity: 0.35; cursor: not-allowed; }
  .play-btn { background: var(--accent); color: #fff; }
  .play-btn:not(:disabled):hover { background: var(--accent-light); }
  .pause-btn { background: #ff9800; color: var(--text-primary); }
  .pause-btn:not(:disabled):hover { background: #e68900; }
  .stop-btn { background: #f44336; color: #fff; }
  .stop-btn:not(:disabled):hover { background: #d32f2f; }
  .skip-btn { background: #3a3a5a; color: var(--text-primary); font-size: 0.75rem; }
  .skip-btn:not(:disabled):hover { background: #4a4a6a; }

  /* ── Seek ── */
  .seek-area {
    flex: 1; display: flex; flex-direction: column; gap: 0.1rem;
    min-width: 60px; max-width: 180px;
  }
  .seek-slider {
    display: none;
  }
  .seek-slider:disabled { opacity: 0.4; cursor: not-allowed; }
  .time-display {
    font-size: 1rem; color: var(--text-secondary);
    font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap;
    font-family: 'Courier New', monospace;
    font-weight: 600;
  }

  /* ── Vol slider ── */
  .vol-slider-wrap { display: flex; align-items: center; gap: 0.3rem; }
  .vol-label-small { font-size: 0.7rem; color: var(--text-secondary); font-weight: 600; }
  .vol-slider {
    -webkit-appearance: none; appearance: none;
    width: 120px; height: 5px; border-radius: 3px;
    background: var(--bg-hover); outline: none; cursor: pointer;
  }
  .vol-slider::-webkit-slider-thumb {
    -webkit-appearance: none; appearance: none;
    width: 16px; height: 16px; border-radius: 50%;
    background: var(--accent); cursor: pointer; border: 2px solid #0a0a14;
  }
  .vol-label { font-size: 0.7rem; color: var(--text-muted); min-width: 2em; text-align: right; }

  /* ── Song actions ── */
  .song-actions { display: flex; gap: 0.4rem; flex-shrink: 0; }
  .song-btn {
    padding: 0.3rem 0.6rem; border-radius: 5px;
    border: 1px solid var(--border-light); background: var(--bg-hover);
    color: var(--text-secondary); font-size: 0.75rem;
    cursor: pointer; transition: background 0.2s, border-color 0.2s;
    white-space: nowrap; text-decoration: none; display: inline-flex; align-items: center;
  }
  .song-btn:hover { background: #333355; border-color: var(--text-muted); }
  .export-btn:hover { color: var(--accent); border-color: var(--accent); }
  .delete-btn:hover { color: #f44336; border-color: #f44336; }

  /* ── Pitch controls ── */
  .pitch-control-row {
    display: flex; align-items: center; gap: 0.5rem;
    padding-top: 0.4rem; border-top: 1px solid var(--border);
  }
  .pitch-label { font-size: 0.8rem; color: var(--text-secondary); font-weight: 600; }
  .pitch-slider {
    -webkit-appearance: none; appearance: none;
    flex: 1; max-width: 200px; height: 4px;
    border-radius: 2px; background: var(--bg-hover); outline: none; cursor: pointer;
  }
  .pitch-slider::-webkit-slider-thumb {
    -webkit-appearance: none; appearance: none;
    width: 14px; height: 14px; border-radius: 50%;
    background: var(--accent); cursor: pointer; border: 2px solid #0a0a14;
  }
  .pitch-value {
    font-size: 0.85rem; font-weight: 700; color: var(--accent-light);
    min-width: 2.5em; text-align: center;
  }
  .pitch-btn {
    padding: 0.35rem 0.8rem; border-radius: 6px;
    border: 1px solid var(--accent); background: var(--accent-bg);
    color: var(--accent-light); font-size: 0.75rem; font-weight: 600;
    cursor: pointer; transition: background 0.2s;
  }
  .pitch-btn:hover:not(:disabled) { background: var(--accent-subtle); }
  .pitch-btn:disabled { opacity: 0.5; cursor: not-allowed; }

  /* ── Pitch subgroups ── */
  .pitch-subgroups-section {
    margin-top: 0.5rem; padding-top: 0.5rem; border-top: 1px dashed var(--border);
  }
  .subgroups-title {
    margin: 0 0 0.5rem 0; font-size: 0.85rem; font-weight: 600;
    color: var(--accent); text-transform: uppercase; letter-spacing: 0.3px;
  }
  .pitch-subgroup-card {
    background: var(--bg-primary); border: 1px solid var(--border-light);
    border-radius: 8px; padding: 0.5rem 0.75rem; margin-bottom: 0.5rem;
  }
  .subgroup-header {
    display: flex; align-items: center; gap: 0.5rem;
    margin-bottom: 0.4rem;
  }
  .subgroup-pitch-label {
    font-size: 0.8rem; font-weight: 700; color: var(--accent-light);
    flex: 1;
  }
  .subgroup-stems { display: flex; flex-direction: column; gap: 0.15rem; margin: 0.3rem 0; }
  .subgroup-actions { display: flex; justify-content: flex-end; padding-top: 0.3rem; }

  /* ── Dropzone ── */
  .pitch-dropzone-section {
    width: 100%; box-sizing: border-box;
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: 12px; padding: 20px;
  }
  .pitch-dropzone {
    width: 100%; box-sizing: border-box;
    border: 2px dashed var(--border); border-radius: 12px;
    padding: 2rem 1rem; display: flex; flex-direction: column; align-items: center;
    gap: 0.5rem; cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
    background: var(--bg-primary);
  }
  .pitch-dropzone:hover { border-color: var(--accent); background: var(--bg-hover); }
  .pitch-dropzone-icon { font-size: 2rem; }
  .pitch-dropzone-text { font-size: 0.95rem; font-weight: 600; color: var(--text-primary); }
  .pitch-dropzone-hint { font-size: 0.75rem; color: var(--text-muted); }

  /* ── Upload players ── */
  .pitch-players-section { width: 100%; }
  .pitch-players-list { display: flex; flex-direction: column; gap: 0.75rem; }
  .song-group {
    background: var(--bg-surface); border-radius: 8px;
    padding: 0.75rem 1rem; animation: fadeIn 0.3s ease;
    border: 1px solid var(--border);
  }
  .song-group.loading { opacity: 0.7; }
  .song-header {
    display: flex; align-items: center; gap: 0.5rem;
    padding-bottom: 0.5rem; border-bottom: 1px solid var(--border);
    margin-bottom: 0.5rem; flex-wrap: wrap;
  }
  .song-name {
    margin: 0; font-size: 0.95rem; font-weight: 600;
    color: var(--accent-light); word-break: break-word;
    flex-shrink: 0; max-width: 180px;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .song-name.error { color: #e57373; }
  .upload-status { font-size: 0.75rem; color: var(--text-muted); font-style: italic; }
  .upload-status.error { color: #e57373; font-style: normal; }

  /* ── Toast ── */
  .toast {
    position: fixed; bottom: 60px; left: 50%;
    transform: translateX(-50%);
    padding: 12px 24px; border-radius: 8px; color: white;
    font-weight: 600; z-index: 1000;
    animation: toastIn 0.3s ease, toastOut 0.3s ease 2.7s forwards;
  }
  .toast.success { background: #4caf50; }
  .toast.error { background: #f44336; }
  @keyframes toastIn { from { opacity: 0; transform: translateX(-50%) translateY(20px); } }
  @keyframes toastOut { to { opacity: 0; transform: translateX(-50%) translateY(-20px); } }
  @keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

  /* ── Responsive ── */
  @media (max-width: 600px) {
    .pitch-page { padding: 0.5rem; }
    .group-player-bar { flex-direction: column; align-items: stretch; }
    .seek-area { max-width: none; }
    .stem-controls { flex-wrap: wrap; }
  }
</style>
