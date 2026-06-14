<script lang="ts">
  import { onDestroy } from 'svelte';
  import { downloadUrl, pitchDownloadUrl, deleteSong, deleteStem as deleteStemApi, pitchStems, getPitchSubgroups, deletePitchSubgroup, deletePitchStem } from './api';
  import type { ResultStem, ResultGroup } from './types';
  import type { PitchResponse, PitchSubgroup } from './api';
  import { stemEmoji, detectStemType } from './types';


  export type { ResultStem };

  export interface SongGroup {
    song: string;
    stems: {
      name: string;
      path: string;
      stemType: string;
      muted: boolean;
      solo: boolean;
      volume: number;
      id: string;
    }[];
  }

  let {
    files = [],
    onstemdeleted = (_song: string, _name: string, _path: string) => {},
    ongroupdeleted = (_song: string) => {},
  }: {
    files?: ResultStem[];
    onstemdeleted?: (song: string, name: string, path: string) => void;
    ongroupdeleted?: (song: string) => void;
  } = $props();

  // Toast notification state
  let toast = $state<{ message: string; type: 'success' | 'error' } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  function showToast(message: string, type: 'success' | 'error') {
    toast = { message, type };
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toast = null;
    }, 3000);
  }

  // Group files by song
  let songGroups = $derived(groupFiles(files));

  // State for mute/solo/volume per stem
  let stemStates = $state<Record<string, { muted: boolean; solo: boolean; volume: number }>>({});

  // Per-group Web Audio player state
  let groupPlayers = $state<Record<string, {
    audioCtx: AudioContext | null;
    playing: boolean;
    paused: boolean;
    currentTime: number;
    duration: number;
    seekValue: number;
    sourceNodes: Map<string, AudioBufferSourceNode>;
    gainNodes: Map<string, GainNode>;
    buffers: Map<string, AudioBuffer>;
    startTime: number;
    pauseOffset: number;
    animFrame: number | null;
    loaded: boolean;
  }>>({});

  // Canvas refs for waveform drawing
  let waveformCanvases = $state<Record<string, HTMLCanvasElement>>({});
  
  // Read accent color from CSS for canvas fills
  function accent(): string {
    if (typeof document === 'undefined') return '#6c5ce7';
    const c = getComputedStyle(document.body).getPropertyValue('--accent').trim();
    return c || '#6c5ce7';
  }

  // Pitch shift state
  let pitchSliderValue = $state<Record<string, number>>({});
  let pitchProcessing = $state<Record<string, boolean>>({});

  // AbortController for cancelling in-flight fetch requests on destroy
  let abortController = new AbortController();

  // Pitched subgroup state with independent player
  interface PitchedSubgroupStem {
    name: string;
    path: string;
    stemType: string;
  }

  interface PitchedSubgroup {
    pitch: number;
    stems: PitchedSubgroupStem[];
    player: {
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
    } | null;
  }
  let pitchSubgroups = $state<Record<string, PitchedSubgroup[]>>({});

  // Peak levels for pitched subgroup stems
  let pitchedLevels = $state<Record<string, { l: number; r: number }>>({});
  let pitchedPeaks = $state<Record<string, { l: number; r: number }>>({});

  // Waveform state for pitched subgroup seek
  let pitchedWaveformCanvases = $state<Record<string, HTMLCanvasElement>>({});
  let pitchedWavePeaksCache = $state<Record<string, number[]>>({});
  let pitchedDragging = $state<Record<string, boolean>>({});
  let pitchedDragPreview = $state<Record<string, number>>({});

  async function loadPitchSubgroups(song: string) {
    try {
      const subs = await getPitchSubgroups(song, abortController.signal);
      const mapped: PitchedSubgroup[] = subs.map(s => ({
        pitch: s.pitch,
        stems: s.files.map(f => ({
          name: f.name,
          path: f.path,
          stemType: detectStemType(f.name),
        })),
        player: null,
      }));
      pitchSubgroups[song] = mapped;
      pitchSubgroups = { ...pitchSubgroups };
    } catch (err) {
      console.error(`Failed to load pitch subgroups for ${song}:`, err);
      showToast(`Error loading pitch groups: ${err instanceof Error ? err.message : String(err)}`, 'error');
    }
  }

  // ---- Grouping ----

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
        const key = stemKey(s.song || song, s.name);
        // Read-only: never mutate $state inside $derived
        const state = stemStates[key] || { muted: false, solo: false, volume: 100 };
        return { ...s, stemType: s.stemType || 'other', ...state, id: key };
      }),
    }));
  }

  function extractSong(name: string, path: string): string {
    if (path) {
      const parts = path.split('/');
      if (parts.length >= 2) return parts[parts.length - 2];
    }
    return name.replace(/_(vocals|drums|bass|other|instrumental)\.\w+$/i, '');
  }

  function stemKey(song: string, name: string): string {
    return `${song}/${name}`;
  }

  // ---- Stem mute/solo/volume ----

  function toggleMute(song: string, name: string) {
    const key = stemKey(song, name);
    stemStates[key] = {
      ...stemStates[key],
      muted: !(stemStates[key]?.muted ?? false),
      volume: stemStates[key]?.volume ?? 100,
    };
    syncGains(song);
  }

  function toggleSolo(song: string, name: string) {
    const key = stemKey(song, name);
    stemStates[key] = {
      ...stemStates[key],
      solo: !(stemStates[key]?.solo ?? false),
      volume: stemStates[key]?.volume ?? 100,
    };
    syncGains(song);
  }

  function setVolume(song: string, name: string, vol: number) {
    const key = stemKey(song, name);
    stemStates[key] = {
      ...stemStates[key],
      volume: vol,
    };
    syncGains(song);
  }

  function handleVolumeChange(e: Event, song: string, name: string) {
    setVolume(song, name, parseInt((e.target as HTMLInputElement).value));
  }

  // ---- Per-group player management ----

  function getPlayer(song: string) {
    if (!groupPlayers[song]) {
      groupPlayers[song] = {
        audioCtx: null,
        playing: false,
        paused: false,
        currentTime: 0,
        duration: 0,
        seekValue: 0,
        sourceNodes: new Map(),
        gainNodes: new Map(),
        buffers: new Map(),
        startTime: 0,
        pauseOffset: 0,
        animFrame: null,
        loaded: false,
      };
    }
    return groupPlayers[song];
  }

  function getCtx(song: string): AudioContext {
    let p = getPlayer(song);
    if (!p.audioCtx) {
      p.audioCtx = new AudioContext();
    }
    return p.audioCtx;
  }

  function getGroup(groupSong: string): SongGroup | undefined {
    return songGroups.find((g: SongGroup) => g.song === groupSong);
  }

  function anySolo(song: string): boolean {
    const group = getGroup(song);
    if (!group) return false;
    return group.stems.some((s) => stemStates[stemKey(song, s.name)]?.solo);
  }

  function effectiveGain(song: string, name: string): number {
    const key = stemKey(song, name);
    const state = stemStates[key] || { muted: false, solo: false, volume: 100 };
    if (state.muted) return 0;
    const hasAnySolo = anySolo(song);
    if (hasAnySolo && !state.solo) return 0;
    return (state.volume ?? 100) / 100;
  }

  function syncGains(song: string) {
    const p = groupPlayers[song];
    if (!p || !p.playing) return;
    const group = getGroup(song);
    if (!group) return;
    for (const stem of group.stems) {
      const key = stemKey(song, stem.name);
      const gain = p.gainNodes.get(key);
      if (gain) {
        gain.gain.value = effectiveGain(song, stem.name);
      }
    }
  }

  async function loadBuffers(song: string) {
    const p = getPlayer(song);
    if (p.loaded) return;
    const ctx = getCtx(song);
    const group = getGroup(song);
    if (!group) return;

    for (const stem of group.stems) {
      try {
        const url = downloadUrl(song, stem.name);
        const resp = await fetch(url, { signal: abortController.signal });
        const arrayBuf = await resp.arrayBuffer();
        const audioBuf = await ctx.decodeAudioData(arrayBuf);
        p.buffers.set(stemKey(song, stem.name), audioBuf);
      } catch (err) {
        console.error(`Failed to load ${stem.name}:`, err);
      }
    }
    p.loaded = true;
  }

  function stopAllSources(song: string) {
    const p = groupPlayers[song];
    if (!p) return;
    p.sourceNodes.forEach((src: AudioBufferSourceNode) => {
      try { src.stop(); } catch { /* already stopped */ }
    });
    p.sourceNodes.clear();
    p.gainNodes.clear();
    if (p.animFrame) {
      cancelAnimationFrame(p.animFrame);
      p.animFrame = null;
    }
  }

  async function playGroup(song: string) {
    const p = getPlayer(song);
    if (p.playing && !p.paused) return;

    const ctx = getCtx(song);
    if (ctx.state === 'suspended') {
      await ctx.resume();
    }

    await loadBuffers(song);

    stopAllSources(song);

    const offset = p.paused ? p.pauseOffset : 0;
    const now = ctx.currentTime;
    p.startTime = now - offset;

    let maxDur = 0;
    const group = getGroup(song);
    if (!group) return;

    for (const stem of group.stems) {
      const buf = p.buffers.get(stemKey(song, stem.name));
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;

      const gain = ctx.createGain();
      gain.gain.value = effectiveGain(song, stem.name);
      gain.connect(ctx.destination);

      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, offset);

      p.sourceNodes.set(stemKey(song, stem.name), src);
      p.gainNodes.set(stemKey(song, stem.name), gain);
    }

    p.duration = maxDur;
    p.playing = true;
    p.paused = false;

    function tick() {
      const player = groupPlayers[song];
      if (!player || !player.playing || player.paused) return;
      const elapsed = ctx.currentTime - player.startTime;
      player.currentTime = elapsed;
      player.seekValue = elapsed;

      if (elapsed >= player.duration) {
        stopGroup(song);
        return;
      }
      player.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }

  function pauseGroup(song: string) {
    const p = groupPlayers[song];
    if (!p || !p.playing || p.paused) return;
    const ctx = p.audioCtx;
    if (!ctx) return;
    p.pauseOffset = ctx.currentTime - p.startTime;
    ctx.suspend();
    p.paused = true;
  }

  function stopGroup(song: string) {
    const p = groupPlayers[song];
    if (!p) return;
    stopAllSources(song);
    p.audioCtx?.suspend();
    p.playing = false;
    p.paused = false;
    p.currentTime = 0;
    p.seekValue = 0;
    p.pauseOffset = 0;
    p.duration = 0;
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
    const group = getGroup(song);
    if (!group) return;

    for (const stem of group.stems) {
      const buf = p.buffers.get(stemKey(song, stem.name));
      if (!buf) continue;
      if (buf.duration > maxDur) maxDur = buf.duration;
      const effectiveOffset = Math.min(time, buf.duration);

      const gain = ctx.createGain();
      gain.gain.value = effectiveGain(song, stem.name);
      gain.connect(ctx.destination);

      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(0, effectiveOffset);

      p.sourceNodes.set(stemKey(song, stem.name), src);
      p.gainNodes.set(stemKey(song, stem.name), gain);
    }

    p.duration = maxDur;
    p.playing = true;
    p.paused = false;

    function tick() {
      const player = groupPlayers[song];
      if (!player || !player.playing || player.paused) return;
      const elapsed = ctx.currentTime - player.startTime;
      player.currentTime = elapsed;
      player.seekValue = elapsed;

      if (elapsed >= player.duration) {
        stopGroup(song);
        return;
      }
      player.animFrame = requestAnimationFrame(tick);
    }
    p.animFrame = requestAnimationFrame(tick);
  }

  function handleSeekInput(e: Event, song: string) {
    const target = e.target as HTMLInputElement;
    const time = parseFloat(target.value);
    const p = groupPlayers[song];
    if (p) {
      p.seekValue = time;
      p.currentTime = time;
      p.pauseOffset = time;
    }
  }

  function handleSeekChange(e: Event, song: string) {
    const target = e.target as HTMLInputElement;
    seekGroup(song, parseFloat(target.value));
  }

  function fmtTime(sec: number | undefined): string {
    if (sec == null || !isFinite(sec)) return '0:00';
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  // ---- Export ----

  function handleExport(song: string) {
    const group = getGroup(song);
    if (!group) return;
    for (const stem of group.stems) {
      const url = downloadUrl(song, stem.name);
      const a = document.createElement('a');
      a.href = url;
      a.download = stem.name;
      a.click();
    }
  }

  // ---- Delete ----

  async function handleDeleteSong(song: string) {
    if (!confirm(`Delete all files for "${song}"?`)) return;
    try {
      await deleteSong(song);
      showToast('Grupo borrado correctamente', 'success');
      ongroupdeleted(song);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('Error al borrar: ' + msg, 'error');
    }
  }

  async function deleteStem(song: string, name: string, path: string) {
    if (!confirm(`Delete "${name}"?`)) return;
    try {
      await deleteStemApi(song, name);
      showToast(`Stem "${name}" eliminado`, 'success');
      onstemdeleted(song, name, path);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('Delete fallido: ' + msg, 'error');
    }
  }

  async function handleApplyPitch(song: string) {
    const value = pitchSliderValue[song] || 0;
    if (value === 0) return;

    // Check if already exists
    const existing = pitchSubgroups[song] || [];
    if (existing.some(s => s.pitch === value)) return;

    pitchProcessing[song] = true;
    pitchProcessing = { ...pitchProcessing };

    try {
      const result = await pitchStems(song, value);
      const stems = result.files.map(f => ({
        name: f.name,
        path: f.path,
        stemType: detectStemType(f.name),
      }));

      pitchSubgroups[song] = [...existing, { pitch: value, stems, player: null }];
      pitchSubgroups = { ...pitchSubgroups };
    } catch (e) {
      showToast(`Error al cambiar tono: ${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      pitchProcessing[song] = false;
      pitchProcessing = { ...pitchProcessing };
    }
  }

  async function handleDeletePitchSubgroup(song: string, pitch: number) {
    if (!confirm(`¿Eliminar subgrupo ${song} (${pitch > 0 ? '+' : ''}${pitch})?`)) return;
    try {
      await deletePitchSubgroup(song, pitch);
      pitchSubgroups[song] = (pitchSubgroups[song] || []).filter(s => s.pitch !== pitch);
      pitchSubgroups = { ...pitchSubgroups };
      showToast(`Subgrupo ${pitch > 0 ? '+' : ''}${pitch} eliminado`, 'success');
    } catch (e) {
      showToast(`Error: ${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  }

  // ---- Subgroup player functions ----

  function subgroupSongKey(song: string, pitch: number): string {
    return song + (pitch > 0 ? '_pitch+' : '_pitch') + pitch;
  }

  function getOrCreateSubgroupPlayer(song: string, pitch: number): NonNullable<PitchedSubgroup['player']> {
    const subs = pitchSubgroups[song] || [];
    const idx = subs.findIndex(s => s.pitch === pitch);
    if (idx === -1) throw new Error('Subgroup not found');
    const sg = subs[idx];
    if (!sg.player) {
      sg.player = {
        audioCtx: null, playing: false, paused: false, currentTime: 0, duration: 0,
        seekValue: 0, sourceNodes: new Map(), gainNodes: new Map(), analysers: new Map(), buffers: new Map(),
        startTime: 0, pauseOffset: 0, animFrame: null, loaded: false,
      };
      pitchSubgroups[song] = [...subs];
      pitchSubgroups = { ...pitchSubgroups };
    }
    return sg.player!;
  }

  async function playSubgroup(song: string, pitch: number) {
    const player = getOrCreateSubgroupPlayer(song, pitch);
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch)!;

    // Stop all other subgroup players for this song
    for (const s of subs) {
      if (s.pitch !== pitch && s.player?.playing) {
        stopSubgroup(song, s.pitch);
      }
    }

    try {
      if (!player.audioCtx) {
        player.audioCtx = new AudioContext();
      }

      if (player.paused && player.buffers.size > 0) {
        // Resume
        player.playing = true;
        player.paused = false;
        player.startTime = player.audioCtx.currentTime - player.pauseOffset;
        for (const [name, buffer] of player.buffers) {
          const source = player.audioCtx.createBufferSource();
          source.buffer = buffer;
          const gain = player.gainNodes.get(name) || player.audioCtx.createGain();
          const stemState = stemStates[`pitch:${song}:${pitch}:${name}`] || { muted: false, solo: false, volume: 100 };
          gain.gain.value = stemState.muted ? 0 : stemState.volume / 100;
          const splitter = player.audioCtx.createChannelSplitter(2);
          const aL = player.audioCtx.createAnalyser(); aL.fftSize = 64;
          const aR = player.audioCtx.createAnalyser(); aR.fftSize = 64;
          gain.connect(splitter);
          splitter.connect(aL, 0); splitter.connect(aR, 1);
          aL.connect(player.audioCtx.destination); aR.connect(player.audioCtx.destination);
          source.connect(gain);
          source.start(0, player.pauseOffset);
          player.sourceNodes.set(name, source);
          player.gainNodes.set(name, gain);
          player.analysers.set(name, [aL, aR]);
        }
        startSubgroupTimer(song, pitch);
        return;
      }

      // Start fresh: load and play all stems
      player.loaded = false;
      const loadPromises = sg.stems.map(async (stem) => {
        if (player.buffers.has(stem.name)) return;
        const url = pitchDownloadUrl(song, pitch, stem.name);
        const resp = await fetch(url, { signal: abortController.signal });
        const buf = await resp.arrayBuffer();
        const audioBuf = await player.audioCtx!.decodeAudioData(buf);
        player.buffers.set(stem.name, audioBuf);
        if (!player.duration || player.duration < audioBuf.duration) {
          player.duration = audioBuf.duration;
        }
      });

      await Promise.all(loadPromises);
      player.loaded = true;
      player.playing = true;
      player.paused = false;
      player.pauseOffset = 0;
      player.startTime = player.audioCtx.currentTime;

      for (const [name, buffer] of player.buffers) {
        const source = player.audioCtx.createBufferSource();
        source.buffer = buffer;
        const gain = player.audioCtx.createGain();
        const stemState = stemStates[`pitch:${song}:${pitch}:${name}`] || { muted: false, solo: false, volume: 100 };
        gain.gain.value = stemState.muted ? 0 : stemState.volume / 100;
        const splitter = player.audioCtx.createChannelSplitter(2);
        const aL = player.audioCtx.createAnalyser(); aL.fftSize = 64;
        const aR = player.audioCtx.createAnalyser(); aR.fftSize = 64;
        gain.connect(splitter);
        splitter.connect(aL, 0); splitter.connect(aR, 1);
        aL.connect(player.audioCtx.destination); aR.connect(player.audioCtx.destination);
        source.connect(gain);
        source.start(0);
        player.sourceNodes.set(name, source);
        player.gainNodes.set(name, gain);
        player.analysers.set(name, [aL, aR]);
      }

      startSubgroupTimer(song, pitch);
    } catch (e) {
      showToast(`Error playing subgroup: ${e}`, 'error');
    }
  }

  function pauseSubgroup(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player || !player.playing || player.paused) return;

    player.paused = true;
    player.pauseOffset = player.audioCtx!.currentTime - player.startTime;
    if (player.animFrame) cancelAnimationFrame(player.animFrame);
    player.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
    player.sourceNodes.clear();
  }

  function stopSubgroup(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return;

    player.playing = false;
    player.paused = false;
    player.currentTime = 0;
    player.seekValue = 0;
    player.pauseOffset = 0;
    if (player.animFrame) cancelAnimationFrame(player.animFrame);
    player.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
    player.sourceNodes.clear();
    player.analysers.clear();
    player.duration = 0;
    player.loaded = false;
    player.buffers.clear();
    player.gainNodes.clear();
    // Reset peak levels
    for (const stem of (sg?.stems || [])) {
      const stKey = `pitch:${song}:${pitch}:${stem.name}`;
      pitchedLevels = { ...pitchedLevels, [stKey]: { l: 0, r: 0 } };
      pitchedPeaks = { ...pitchedPeaks, [stKey]: { l: 0, r: 0 } };
    }
    pitchSubgroups[song] = [...subs];
    pitchSubgroups = { ...pitchSubgroups };
  }

  function startSubgroupTimer(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return;

    const pitchedKey = `${song}:${pitch}`;

    function tick() {
      if (!player.playing || player.paused) return;
      player.currentTime = player.audioCtx!.currentTime - player.startTime;
      player.seekValue = player.currentTime;
      // Update peak levels from analysers
      for (const stem of (sg?.stems || [])) {
        const stKey = `pitch:${song}:${pitch}:${stem.name}`;
        const ans = player.analysers.get(stem.name);
        if (ans) {
          const dL = new Uint8Array(ans[0].frequencyBinCount);
          const dR = new Uint8Array(ans[1].frequencyBinCount);
          ans[0].getByteTimeDomainData(dL);
          ans[1].getByteTimeDomainData(dR);
          let sumL = 0, sumR = 0;
          for (let j = 0; j < dL.length; j++) {
            const nL = (dL[j] - 128) / 128;
            const nR = (dR[j] - 128) / 128;
            sumL += nL * nL; sumR += nR * nR;
          }
          pitchedLevels = { ...pitchedLevels, [stKey]: { l: Math.sqrt(sumL / dL.length), r: Math.sqrt(sumR / dR.length) }};
          const curL = Math.sqrt(sumL / dL.length);
          const curR = Math.sqrt(sumR / dR.length);
          const prevPk = pitchedPeaks[stKey] || { l: 0, r: 0 };
          pitchedPeaks = { ...pitchedPeaks, [stKey]: { l: Math.max(prevPk.l, curL), r: Math.max(prevPk.r, curR) }};
        }
      }
      // Redraw waveform
      const cv = pitchedWaveformCanvases[pitchedKey];
      if (cv) drawPitchedWaveform(cv, song, pitch);
      if (player.currentTime >= player.duration) {
        stopSubgroup(song, pitch);
        return;
      }
      pitchSubgroups[song] = [...subs];
      pitchSubgroups = { ...pitchSubgroups };
      player.animFrame = requestAnimationFrame(tick);
    }
    player.animFrame = requestAnimationFrame(tick);
  }

  function handleSubgroupSeekInput(e: Event, song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return;
    player.seekValue = parseFloat((e.target as HTMLInputElement).value);
  }

  async function handleSubgroupSeekChange(e: Event, song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return;
    const seekTo = parseFloat((e.target as HTMLInputElement).value);
    const wasPlaying = player.playing && !player.paused;
    // Cancel previous animation frame
    if (player.animFrame) cancelAnimationFrame(player.animFrame);
    if (wasPlaying) {
      player.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
      player.sourceNodes.clear();
      player.analysers.clear();
      player.pauseOffset = seekTo;
      player.currentTime = seekTo;
      player.startTime = player.audioCtx!.currentTime - seekTo;
      for (const [name, buffer] of player.buffers) {
        if (player.sourceNodes.has(name)) continue;
        const source = player.audioCtx!.createBufferSource();
        source.buffer = buffer;
        const gain = player.gainNodes.get(name) || player.audioCtx!.createGain();
        const stemState = stemStates[`pitch:${song}:${pitch}:${name}`] || { muted: false, solo: false, volume: 100 };
        gain.gain.value = stemState.muted ? 0 : stemState.volume / 100;
        const splitter = player.audioCtx!.createChannelSplitter(2);
        const aL = player.audioCtx!.createAnalyser(); aL.fftSize = 64;
        const aR = player.audioCtx!.createAnalyser(); aR.fftSize = 64;
        gain.connect(splitter);
        splitter.connect(aL, 0); splitter.connect(aR, 1);
        aL.connect(player.audioCtx!.destination); aR.connect(player.audioCtx!.destination);
        source.connect(gain);
        source.start(0, seekTo);
        player.sourceNodes.set(name, source);
        player.analysers.set(name, [aL, aR]);
      }
      startSubgroupTimer(song, pitch);
    }
  }

  // Subgroup stem controls
  function toggleSubgroupMute(song: string, pitch: number, stemName: string) {
    const key = `pitch:${song}:${pitch}:${stemName}`;
    const current = stemStates[key] || { muted: false, solo: false, volume: 100 };
    stemStates[key] = { ...current, muted: !current.muted };
    stemStates = { ...stemStates };
    syncSubgroupGains(song, pitch);
  }

  function toggleSubgroupSolo(song: string, pitch: number, stemName: string) {
    const key = `pitch:${song}:${pitch}:${stemName}`;
    const current = stemStates[key] || { muted: false, solo: false, volume: 100 };
    stemStates[key] = { ...current, solo: !current.solo };
    stemStates = { ...stemStates };
    syncSubgroupGains(song, pitch);
  }

  function handleSubgroupVolume(e: Event, song: string, pitch: number, stemName: string) {
    const key = `pitch:${song}:${pitch}:${stemName}`;
    const val = parseInt((e.target as HTMLInputElement).value);
    const current = stemStates[key] || { muted: false, solo: false, volume: 100 };
    stemStates[key] = { ...current, volume: val };
    stemStates = { ...stemStates };
    syncSubgroupGains(song, pitch);
  }

  function syncSubgroupGains(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return; // solo retornar si no hay player, aunque no esté playing
    const hasSolo = sg.stems.some(s => stemStates[`pitch:${song}:${pitch}:${s.name}`]?.solo);
    for (const stem of sg.stems) {
      const key = `pitch:${song}:${pitch}:${stem.name}`;
      const gain = player.gainNodes.get(stem.name);
      if (gain) {
        const state = stemStates[key] || { muted: false, solo: false, volume: 100 };
        if (state.muted) gain.gain.value = 0;
        else if (hasSolo && !state.solo) gain.gain.value = 0;
        else gain.gain.value = (state.volume ?? 100) / 100;
      }
    }
  }

  async function handleDeleteSubgroupStem(song: string, pitch: number, stemName: string) {
    if (!confirm(`Delete "${stemName}"?`)) return;
    try {
      await deletePitchStem(song, pitch, stemName);
      const subs = pitchSubgroups[song] || [];
      const sg = subs.find(s => s.pitch === pitch);
      if (sg) {
        sg.stems = sg.stems.filter(s => s.name !== stemName);
        if (sg.stems.length === 0) {
          // Eliminar subgrupo entero
          pitchSubgroups[song] = (pitchSubgroups[song] || []).filter(s => s.pitch !== pitch);
        } else {
          pitchSubgroups[song] = [...subs];
        }
        pitchSubgroups = { ...pitchSubgroups };
      }
      showToast(`Stem "${stemName}" eliminado`, 'success');
    } catch (e) {
      showToast(`Error: ${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  }

  let pitchLoadVersion = 0;

  // ---- $effect to load pitch subgroups when songGroups changes ----

  $effect(() => {
    const songs = songGroups.map(g => g.song);
    const currentVersion = ++pitchLoadVersion;
    for (const song of songs) {
      // Usar then() en vez de await para paralelismo controlado
      getPitchSubgroups(song, abortController.signal).then(subs => {
        if (currentVersion !== pitchLoadVersion) return; // stale response
            subs = subs.filter(s => s.files != null);  // skip damaged subgroups
        const mapped = subs.map(s => ({
          pitch: s.pitch,
          stems: s.files.map(f => ({
            name: f.name,
            path: f.path,
            stemType: detectStemType(f.name),
          })),
          player: null,
        }));
        pitchSubgroups[song] = mapped;
        pitchSubgroups = { ...pitchSubgroups };
      }).catch((err) => {
        console.error(`Failed to load pitch subgroups for ${song}:`, err);
        showToast(`Error loading pitch groups: ${err instanceof Error ? err.message : String(err)}`, 'error');
      });
    }
  });

  let waveformDrawn = $state<Set<string>>(new Set());

  // Shared OfflineAudioContext for waveform decoding (avoid creating one per canvas)
  let sharedAudioCtx: OfflineAudioContext | null = null;
  function getAudioCtx(): OfflineAudioContext {
    if (!sharedAudioCtx || sharedAudioCtx.state === 'closed') {
      sharedAudioCtx = new OfflineAudioContext(1, 1, 44100);
    }
    return sharedAudioCtx;
  }

  async function drawRealWaveform(canvas: HTMLCanvasElement, song: string, name: string) {
    const key = stemKey(song, name);
    if (waveformDrawn.has(key)) return;
    waveformDrawn.add(key);

    // Scale canvas for retina displays (CSS sizes via class, intrinsic resolution via attr)
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
    canvas.width = canvas.clientWidth * dpr;
    canvas.height = canvas.clientHeight * dpr;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);

    let audioCtx: OfflineAudioContext | undefined;
    try {
      const url = downloadUrl(song, name);
      const resp = await fetch(url, { signal: abortController.signal });
      const arrayBuf = await resp.arrayBuffer();
      audioCtx = getAudioCtx();
      const audioBuf = await audioCtx.decodeAudioData(arrayBuf);
      const channel = audioBuf.getChannelData(0);

      const step = Math.max(1, Math.floor(channel.length / w));
      for (let i = 0; i < w; i++) {
        let max = 0;
        const start = i * step;
        const end = Math.min(start + step, channel.length);
        for (let j = start; j < end; j++) {
          max = Math.max(max, Math.abs(channel[j]));
        }
        const barH = Math.max(1, max * h);
        ctx.fillStyle = accent();
        ctx.fillRect(i, (h - barH) / 2, 1, barH);
      }
    } catch {
      // Fallback: draw from stem name hash (deterministic)
      let hash = 0;
      for (let i = 0; i < key.length; i++) {
        hash = ((hash << 5) - hash) + key.charCodeAt(i);
        hash |= 0;
      }
      ctx.fillStyle = accent();
      const barCount = 40;
      const barWidth = w / barCount;
      for (let i = 0; i < barCount; i++) {
        const hVal = ((Math.abs(hash + i * 31) % 80) / 100) * h * 0.8 + h * 0.1;
        const x = i * barWidth + 1;
        const y = (h - hVal) / 2;
        ctx.fillRect(x, y, barWidth - 2, hVal);
      }
    } finally {
      // Don't close shared audio context
    }
  }

  function waveformAction(node: HTMLCanvasElement, params: { song: string; name: string }) {
    waveformCanvases[stemKey(params.song, params.name)] = node;
    drawRealWaveform(node, params.song, params.name);
  }

  function waveformUrl(node: HTMLCanvasElement) {
    const url = node.dataset.url;
    if (!url) return;
    waveformCanvases[url] = node;
    drawWaveformFromUrl(node, url);
  }

  async function drawWaveformFromUrl(canvas: HTMLCanvasElement, url: string) {
    if (waveformDrawn.has(url)) return;
    waveformDrawn.add(url);
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
    canvas.width = canvas.clientWidth * dpr;
    canvas.height = canvas.clientHeight * dpr;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);
    try {
      const resp = await fetch(url, { signal: abortController.signal });
      const arrayBuf = await resp.arrayBuffer();
      const audioCtx = getAudioCtx();
      const audioBuf = await audioCtx.decodeAudioData(arrayBuf);
      const channel = audioBuf.getChannelData(0);
      const step = Math.max(1, Math.floor(channel.length / w));
      for (let i = 0; i < w; i++) {
        let max = 0;
        const start = i * step;
        const end = Math.min(start + step, channel.length);
        for (let j = start; j < end; j++) {
          max = Math.max(max, Math.abs(channel[j]));
        }
        const barH = Math.max(1, max * h);
        ctx.fillStyle = accent();
        ctx.fillRect(i, (h - barH) / 2, 1, barH);
      }
      // Don't close shared audio context
    } catch {
      let hash = 0;
      for (let i = 0; i < url.length; i++) {
        hash = ((hash << 5) - hash) + url.charCodeAt(i);
        hash |= 0;
      }
      ctx.fillStyle = accent();
      const barCount = 40;
      const barWidth = w / barCount;
      for (let i = 0; i < barCount; i++) {
        const hVal = ((Math.abs(hash + i * 31) % 80) / 100) * h * 0.8 + h * 0.1;
        const x = i * barWidth + 1;
        const y = (h - hVal) / 2;
        ctx.fillRect(x, y, barWidth - 2, hVal);
      }
    }
  }

  // ── Pitched subgroup skip ──
  function pitchedSkipBack(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player || !player.loaded || player.duration <= 0) return;
    const newTime = Math.max(0, player.currentTime - 10);
    pitchedSeek(song, pitch, newTime);
  }
  function pitchedSkipForward(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player || !player.loaded || player.duration <= 0) return;
    const newTime = Math.min(player.duration, player.currentTime + 10);
    pitchedSeek(song, pitch, newTime);
  }
  async function pitchedSeek(song: string, pitch: number, time: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return;
    const wasPlaying = player.playing && !player.paused;
    if (player.animFrame) cancelAnimationFrame(player.animFrame);
    if (wasPlaying) {
      player.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
      player.sourceNodes.clear();
      player.analysers.clear();
      player.pauseOffset = time;
      player.currentTime = time;
      player.seekValue = time;
      player.startTime = player.audioCtx!.currentTime - time;
      for (const [name, buffer] of player.buffers) {
        const source = player.audioCtx!.createBufferSource();
        source.buffer = buffer;
        const gain = player.gainNodes.get(name) || player.audioCtx!.createGain();
        const stemState = stemStates[`pitch:${song}:${pitch}:${name}`] || { muted: false, solo: false, volume: 100 };
        gain.gain.value = stemState.muted ? 0 : stemState.volume / 100;
        const splitter = player.audioCtx!.createChannelSplitter(2);
        const aL = player.audioCtx!.createAnalyser(); aL.fftSize = 64;
        const aR = player.audioCtx!.createAnalyser(); aR.fftSize = 64;
        gain.connect(splitter);
        splitter.connect(aL, 0); splitter.connect(aR, 1);
        aL.connect(player.audioCtx!.destination); aR.connect(player.audioCtx!.destination);
        source.connect(gain);
        source.start(0, Math.min(time, buffer.duration));
        player.sourceNodes.set(name, source);
        player.analysers.set(name, [aL, aR]);
      }
      startSubgroupTimer(song, pitch);
    }
  }

  // ── Pitched subgroup waveform (seek canvas, same as main group) ──
  async function computePitchedWavePeaks(song: string, pitch: number): Promise<number[]> {
    const pitchedKey = `${song}:${pitch}`;
    if (pitchedWavePeaksCache[pitchedKey]) return pitchedWavePeaksCache[pitchedKey];
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    if (!sg || sg.stems.length === 0) return [];
    try {
      const url = pitchDownloadUrl(song, pitch, sg.stems[0].name);
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
      pitchedWavePeaksCache = { ...pitchedWavePeaksCache, [pitchedKey]: data };
      return data;
    } catch {
      const data: number[] = [];
      for (let i = 0; i < 2000; i++) {
        data.push(((Math.abs((song.length + i * 31)) % 80) / 100) * 0.8 + 0.1);
      }
      pitchedWavePeaksCache = { ...pitchedWavePeaksCache, [pitchedKey]: data };
      return data;
    }
  }

  async function drawPitchedWaveform(canvas: HTMLCanvasElement, song: string, pitch: number) {
    const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
    const w = canvas.clientWidth * dpr;
    const h = canvas.clientHeight * dpr;
    if (w <= 0 || h <= 0) return;
    canvas.width = w; canvas.height = h;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, w, h);
    const accentCol = accent();
    const dimAccentCol = darkenColor(accentCol, 60);
    const isLight = typeof document !== 'undefined' && document.body.classList.contains('light-theme');
    const lineCol = isLight ? '#000' : '#fff';
    const pitchedKey = `${song}:${pitch}`;
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    const isDragging = pitchedDragging[pitchedKey];
    const rawProgress = player && player.loaded && player.duration > 0 ? (player.currentTime / player.duration) : 0;
    const progress = isDragging ? (pitchedDragPreview[pitchedKey] ?? rawProgress) : rawProgress;
    const peaks = pitchedWavePeaksCache[pitchedKey];
    if (!peaks || peaks.length === 0) {
      ctx.fillStyle = dimAccentCol;
      ctx.fillRect(0, 0, w, h);
      computePitchedWavePeaks(song, pitch);
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

  function darkenColor(hex: string, amount: number): string {
    if (!hex || hex === '') return '#333';
    const num = parseInt(hex.replace('#', ''), 16);
    if (isNaN(num)) return '#333';
    const r = Math.max(0, (num >> 16) - amount);
    const g = Math.max(0, ((num >> 8) & 0xff) - amount);
    const b = Math.max(0, (num & 0xff) - amount);
    return `rgb(${r}, ${g}, ${b})`;
  }

  function pitchedWaveformAction(node: HTMLCanvasElement, params: { song: string; pitch: number }) {
    const pitchedKey = `${params.song}:${params.pitch}`;
    pitchedWaveformCanvases[pitchedKey] = node;
    drawPitchedWaveform(node, params.song, params.pitch);
    if (!pitchedWavePeaksCache[pitchedKey]) {
      computePitchedWavePeaks(params.song, params.pitch).then(() => drawPitchedWaveform(node, params.song, params.pitch));
    }
  }

  function getPitchedWaveformFrac(e: MouseEvent, song: string, pitch: number): number {
    const pitchedKey = `${song}:${pitch}`;
    const canvas = pitchedWaveformCanvases[pitchedKey];
    if (!canvas) return 0;
    const rect = canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    return Math.max(0, Math.min(1, x / rect.width));
  }

  function handlePitchedWaveformMouseDown(e: MouseEvent, song: string, pitch: number) {
    e.preventDefault();
    const pitchedKey = `${song}:${pitch}`;
    const frac = getPitchedWaveformFrac(e, song, pitch);
    pitchedDragging = { ...pitchedDragging, [pitchedKey]: true };
    pitchedDragPreview = { ...pitchedDragPreview, [pitchedKey]: frac };
    const cv = pitchedWaveformCanvases[pitchedKey];
    if (cv) drawPitchedWaveform(cv, song, pitch);
  }
  function handlePitchedWaveformMouseMove(e: MouseEvent, song: string, pitch: number) {
    const pitchedKey = `${song}:${pitch}`;
    if (!pitchedDragging[pitchedKey]) return;
    e.preventDefault();
    const frac = getPitchedWaveformFrac(e, song, pitch);
    pitchedDragPreview = { ...pitchedDragPreview, [pitchedKey]: frac };
    const cv = pitchedWaveformCanvases[pitchedKey];
    if (cv) drawPitchedWaveform(cv, song, pitch);
  }
  function handlePitchedWaveformMouseUp(e: MouseEvent, song: string, pitch: number) {
    const pitchedKey = `${song}:${pitch}`;
    if (!pitchedDragging[pitchedKey]) return;
    pitchedDragging = { ...pitchedDragging, [pitchedKey]: false };
    const frac = pitchedDragPreview[pitchedKey] ?? 0;
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (player && player.loaded && player.duration > 0) {
      pitchedSeek(song, pitch, frac * player.duration);
    }
  }
  function handlePitchedWaveformMouseLeave(e: MouseEvent, song: string, pitch: number) {
    const pitchedKey = `${song}:${pitch}`;
    if (pitchedDragging[pitchedKey]) {
      handlePitchedWaveformMouseUp(e, song, pitch);
    }
  }
  function handlePitchedGlobalMouseUp() {
    for (const pitchedKey of Object.keys(pitchedDragging)) {
      if (pitchedDragging[pitchedKey]) {
        pitchedDragging = { ...pitchedDragging, [pitchedKey]: false };
        const frac = pitchedDragPreview[pitchedKey] ?? 0;
        const parts = pitchedKey.split(':');
        const song = parts[0];
        const pitch = parseInt(parts[1]);
        const subs = pitchSubgroups[song] || [];
        const sg = subs.find(s => s.pitch === pitch);
        const player = sg?.player;
        if (player && player.loaded && player.duration > 0) {
          pitchedSeek(song, pitch, frac * player.duration);
        }
      }
    }
  }

  // Cleanup on component destroy
  onDestroy(() => {
    if (toastTimer) clearTimeout(toastTimer);
    // Do NOT abort abortController — keeps background fetch alive
    // Do NOT close AudioContexts for groups/subgroups — keeps playback alive
    for (const [key, player] of Object.entries(groupPlayers)) {
      player.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
      if (player.animFrame) cancelAnimationFrame(player.animFrame);
    }
    // Cleanup subgroup players (stop sources, keep AudioContexts alive)
    for (const subs of Object.values(pitchSubgroups)) {
      for (const sg of subs) {
        const p = sg.player;
        if (p) {
          p.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
          if (p.animFrame) cancelAnimationFrame(p.animFrame);
        }
      }
    }
    waveformDrawn.clear();
  });

  function formatPitchStemName(name: string): string {
    const match = name.match(/^(.+?)_pitch([+-]\d+)(?:\..+)?$/);
    if (match) {
      return `${match[1]} (${match[2]})`;
    }
    return name;
  }
</script>

{#if songGroups.length > 0}
  <div class="results-panel">
    <h2 class="results-title">📀 Results</h2>

    {#each songGroups as group (group.song)}
      {@const player = groupPlayers[group.song]}
      <div class="song-group">
        <!-- Song header with transport controls -->
        <div class="song-header">
          <h3 class="song-name">🎵 {group.song}</h3>

          <div class="playback-controls">
            <button
              class="ctrl-btn play-btn"
              onclick={() => playGroup(group.song)}
              disabled={player?.playing && !player?.paused}
              title={player?.playing && !player?.paused ? 'Playing' : 'Play'}
            >
              ▶
            </button>
            <button
              class="ctrl-btn pause-btn"
              onclick={() => pauseGroup(group.song)}
              disabled={!player?.playing || player?.paused}
              title="Pause"
            >
              ⏸
            </button>
            <button
              class="ctrl-btn stop-btn"
              onclick={() => stopGroup(group.song)}
              disabled={!player?.playing && !player?.paused}
              title="Stop"
            >
              ⏹
            </button>
          </div>

          <div class="seek-area">
            <input
              type="range"
              min="0"
              max={player?.duration || 100}
              step="0.1"
              value={player?.seekValue || 0}
              disabled={!player?.playing && !player?.paused}
              oninput={(e) => handleSeekInput(e, group.song)}
              onchange={(e) => handleSeekChange(e, group.song)}
              class="seek-slider"
              title="Seek"
            />
            <span class="time-display">
              {fmtTime(player?.currentTime)} / {fmtTime(player?.duration)}
            </span>
          </div>

          <div class="song-actions">
            <button
              class="song-btn export-btn"
              onclick={() => handleExport(group.song)}
              title="Download all stems"
            >
              ⬇ Export
            </button>
            <button
              class="song-btn delete-btn"
              onclick={() => handleDeleteSong(group.song)}
              title="Delete song"
            >
              🗑
            </button>
          </div>
        </div>

        <!-- Pitch controls -->
        <div class="pitch-section">
          <label class="pitch-label">
            Tono: <strong>{pitchSliderValue[group.song] || 0}</strong>
          </label>
          <input
            type="range"
            class="pitch-slider"
            min="-12"
            max="12"
            step="1"
            value={pitchSliderValue[group.song] || 0}
            oninput={(e) => { pitchSliderValue[group.song] = parseInt((e.target as HTMLInputElement).value); pitchSliderValue = { ...pitchSliderValue }; }}
          />
          <button
            class="pitch-apply-btn"
            onclick={() => handleApplyPitch(group.song)}
            disabled={pitchProcessing[group.song] || !(pitchSliderValue[group.song] || 0)}
          >
            {pitchProcessing[group.song] ? '⏳' : '🎵 Cambiar tono'}
          </button>
        </div>

        <!-- Stem rows -->
        <div class="stems-list">
          {#each group.stems as stem (stem.id)}
            {@const key = stemKey(group.song, stem.name)}
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
              <span class="stem-emoji">{stemEmoji(stem.stemType)}</span>
              <span class="stem-name" title={stem.name}>{formatPitchStemName(stem.name)}</span>

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
                <button
                  class="stem-btn delete-stem-btn"
                  onclick={() => deleteStem(group.song, stem.name, stem.path)}
                  title="Delete stem"
                >
                  ✕
                </button>
              </div>
            </div>
          {/each}
        </div>

        <!-- Pitch subgroups -->
        {#if pitchSubgroups[group.song]?.length}
          {#each pitchSubgroups[group.song] as sg (sg.pitch)}
            {@const subPlayer = sg.player}
            {@const subsStems = sg.stems}
            <div class="pitched-group">
              <!-- Subgroup header -->
              <div class="pitched-header">
                <h4 class="pitched-title">
                  {group.song} ({sg.pitch > 0 ? '+' : ''}{sg.pitch})
                </h4>
                <button class="pitched-delete-btn" onclick={() => handleDeletePitchSubgroup(group.song, sg.pitch)} title="Eliminar subgrupo">🗑</button>
              </div>

              <!-- Subgroup player bar (same as main group) -->
              <div class="song-header pitched-playback" style="flex-wrap:wrap">
                <div class="playback-controls">
                  <button class="ctrl-btn skip-btn" onclick={() => pitchedSkipBack(group.song, sg.pitch)}
                    disabled={!subPlayer?.loaded} title="-10s">⏪</button>
                  <button class="ctrl-btn play-btn" onclick={() => playSubgroup(group.song, sg.pitch)}
                    disabled={subPlayer?.playing && !subPlayer?.paused}
                    title={subPlayer?.playing && !subPlayer?.paused ? 'Playing' : 'Play'}>▶</button>
                  <button class="ctrl-btn pause-btn" onclick={() => pauseSubgroup(group.song, sg.pitch)}
                    disabled={!subPlayer?.playing || subPlayer?.paused} title="Pause">⏸</button>
                  <button class="ctrl-btn stop-btn" onclick={() => stopSubgroup(group.song, sg.pitch)}
                    disabled={!subPlayer?.playing && !subPlayer?.paused} title="Stop">⏹</button>
                  <button class="ctrl-btn skip-btn" onclick={() => pitchedSkipForward(group.song, sg.pitch)}
                    disabled={!subPlayer?.loaded} title="+10s">⏩</button>
                </div>
                <div class="seek-area" style="max-width:160px">
                  <span class="time-display">{fmtTime(subPlayer?.currentTime)} / {fmtTime(subPlayer?.duration)}</span>
                </div>
                <div class="vol-slider-wrap" style="flex-shrink:0">
                  <input type="range" min="0" max="100" value={100}
                    oninput={(e) => {
                      const vol = parseInt((e.target as HTMLInputElement).value);
                      for (const st of subsStems) {
                        const key = `pitch:${group.song}:${sg.pitch}:${st.name}`;
                        const cur = stemStates[key] || { muted: false, solo: false, volume: 100 };
                        stemStates[key] = { ...cur, volume: vol };
                      }
                      stemStates = { ...stemStates };
                      syncSubgroupGains(group.song, sg.pitch);
                    }}
                    class="vol-slider" style="width:80px" title="Master volume" />
                </div>
              </div>

              <!-- Subgroup waveform seek canvas -->
              <div style="width:100%; margin-bottom:0.4rem; cursor:pointer; border-radius:4px; overflow:hidden"
                onmouseover={() => {}}
                class:waveform-hover={true}>
                <canvas class="waveform-seek" width="200" height="80"
                  use:pitchedWaveformAction={{ song: group.song, pitch: sg.pitch }}
                  onmousedown={(e) => handlePitchedWaveformMouseDown(e, group.song, sg.pitch)}
                  onmousemove={(e) => handlePitchedWaveformMouseMove(e, group.song, sg.pitch)}
                  onmouseup={(e) => handlePitchedWaveformMouseUp(e, group.song, sg.pitch)}
                  onmouseleave={(e) => handlePitchedWaveformMouseLeave(e, group.song, sg.pitch)}
                  role="slider" tabindex="0"
                  aria-label="Waveform seek" />
              </div>

              <!-- Subgroup stems with full controls + peak meters -->
              <div class="stems-list">
                {#each sg.stems as stem}
                  {@const stemId = `pitch:${group.song}:${sg.pitch}:${stem.name}`}
                  {@const subState = stemStates[stemId] ?? { muted: false, solo: false, volume: 100 }}
                  {@const sLevel = pitchedLevels[stemId] || { l: 0, r: 0 }}
                  {@const pLevel = pitchedPeaks[stemId] || { l: 0, r: 0 }}
                  <div class="stem-row pitched-stem" class:muted={subState.muted}>
                    <span class="stem-emoji">{stemEmoji(stem.stemType)}</span>
                    <span class="stem-name" title={stem.name}>{formatPitchStemName(stem.name)}</span>
                    <div class="stem-controls">
                      <button class="stem-btn mute-btn" class:active={subState.muted}
                        onclick={() => toggleSubgroupMute(group.song, sg.pitch, stem.name)}>M</button>
                      <button class="stem-btn solo-btn" class:active={subState.solo}
                        onclick={() => toggleSubgroupSolo(group.song, sg.pitch, stem.name)}>S</button>
                      <div class="vol-slider-wrap">
                        <input type="range" min="0" max="100" value={subState.volume}
                          oninput={(e) => handleSubgroupVolume(e, group.song, sg.pitch, stem.name)} class="vol-slider" />
                        <span class="vol-label">{subState.volume}</span>
                      </div>
                      <!-- Peak meters -->
                      <div class="peak-meter" style="min-width:100px; margin:0 0.3rem">
                        <div class="peak-db-top">L:{toDbStr(pLevel.l)}dB R:{toDbStr(pLevel.r)}dB</div>
                        <div class="peak-bar-container"><div class="peak-bar peak-l" style="width:{dbToPct(rmsToDb(sLevel.l))}%"></div><div class="peak-marker" style="left:{dbToPct(rmsToDb(pLevel.l))}%"></div></div>
                        <div class="peak-bar-container"><div class="peak-bar peak-r" style="width:{dbToPct(rmsToDb(sLevel.r))}%"></div><div class="peak-marker" style="left:{dbToPct(rmsToDb(pLevel.r))}%"></div></div>
                      </div>
                      <a class="stem-btn dl-btn" href={pitchDownloadUrl(group.song, sg.pitch, stem.name)} download={stem.name}>⬇</a>
                      <button class="stem-btn delete-stem-btn"
                        onclick={() => handleDeleteSubgroupStem(group.song, sg.pitch, stem.name)}>✕</button>
                    </div>
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        {/if}
      </div>
    {/each}
  </div>
{/if}

{#if toast}
  <div class="toast {toast.type}">{toast.message}</div>
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
    color: var(--text-primary);
  }

  .song-group {
    background: var(--bg-surface);
    border-radius: 8px;
    padding: 0.75rem 1rem;
    animation: fadeIn 0.3s ease;
  }

  .song-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid var(--border);
    margin-bottom: 0.5rem;
    flex-wrap: wrap;
  }

  .song-name {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: var(--accent-light);
    word-break: break-word;
    flex-shrink: 0;
  }

  /* ---- Transport buttons ---- */
  .playback-controls {
    display: flex;
    gap: 0.25rem;
    flex-shrink: 0;
  }

  .ctrl-btn {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: none;
    font-size: 0.85rem;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.2s, transform 0.1s, opacity 0.2s;
    flex-shrink: 0;
    padding: 0;
  }
  .ctrl-btn:active:not(:disabled) {
    transform: scale(0.95);
  }
  .ctrl-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .play-btn {
    background: var(--accent);
    color: #fff;
  }
  .play-btn:not(:disabled):hover {
    background: var(--accent-light);
  }
  .pause-btn {
    background: #ff9800;
    color: var(--text-primary);
  }
  .pause-btn:not(:disabled):hover {
    background: #e68900;
  }
  .stop-btn {
    background: #f44336;
    color: #fff;
  }
  .stop-btn:not(:disabled):hover {
    background: #d32f2f;
  }

  /* ---- Seek ---- */
  .seek-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 80px;
    max-width: 200px;
  }

  .seek-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 4px;
    border-radius: 2px;
    background: var(--bg-hover);
    outline: none;
    cursor: pointer;
  }
  .seek-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: 2px solid #0a0a14;
  }
  .seek-slider:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .time-display {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
    text-align: right;
    white-space: nowrap;
  }

  /* ---- Song actions ---- */
  .song-actions {
    display: flex;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .song-btn {
    padding: 0.3rem 0.6rem;
    border-radius: 5px;
    border: 1px solid var(--border-light);
    background: var(--bg-hover);
    color: var(--text-secondary);
    font-size: 0.75rem;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
    white-space: nowrap;
  }
  .song-btn:hover {
    background: #333355;
    border-color: var(--text-muted);
  }
  .export-btn:hover { color: var(--accent); border-color: var(--accent); }
  .delete-btn:hover { color: #f44336; border-color: #f44336; }

  /* ---- Stem rows ---- */
  .stems-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .stem-row {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.3rem 0.4rem;
    border-radius: 6px;
    background: #111;
    transition: background 0.2s, opacity 0.2s;
    flex-wrap: wrap;
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
    width: 120px;
    max-width: 25%;
    height: 28px;
  }

  .stem-emoji {
    font-size: 1rem;
    flex-shrink: 0;
  }

  .stem-name {
    flex: 1;
    font-size: 0.85rem;
    color: var(--text-primary);
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
    width: 26px;
    height: 26px;
    min-width: 24px;
    min-height: 24px;
    border-radius: 4px;
    border: 1px solid var(--border-light);
    background: var(--bg-hover);
    color: var(--text-secondary);
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
    border-color: var(--text-muted);
    color: var(--text-secondary);
  }
  .stem-btn.active {
    background: var(--accent);
    color: #fff;
    border-color: var(--accent);
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
    color: var(--accent-light);
    font-size: 0.8rem;
  }
  .dl-btn:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
  .delete-stem-btn {
    color: #f44336;
    font-size: 0.75rem;
  }
  .delete-stem-btn:hover {
    background: #f44336;
    color: #fff;
    border-color: #f44336;
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
    background: var(--bg-hover);
    outline: none;
    cursor: pointer;
  }
  .vol-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: 2px solid #0a0a14;
  }

  .vol-label {
    font-size: 0.7rem;
    color: var(--text-secondary);
    width: 24px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  @media (max-width: 600px) {
    .song-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 0.4rem;
    }
    .seek-area {
      width: 100%;
    }
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
  .toast.success { background: #4caf50; }
  .toast.error { background: #f44336; }
  @keyframes toastIn { from { opacity: 0; transform: translateX(-50%) translateY(20px); } }
  @keyframes toastOut { to { opacity: 0; transform: translateX(-50%) translateY(-20px); } }

  .pitch-section {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0;
    border-top: 1px solid var(--border);
    margin-top: 0.5rem;
  }
  .pitch-label {
    font-size: 0.8rem;
    color: var(--text-primary);
    white-space: nowrap;
    flex-shrink: 0;
  }
  .pitch-slider {
    flex: 1;
    max-width: 150px;
    accent-color: var(--accent-light);
    cursor: pointer;
  }
  .pitch-apply-btn {
    padding: 0.3rem 0.8rem;
    background: var(--accent);
    border: none;
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .pitch-apply-btn:hover:not(:disabled) { opacity: 0.9; }
  .pitch-apply-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .pitched-group {
    margin-left: 1.5rem;
    border-left: 2px solid var(--accent-border);
    padding-left: 0.75rem;
    margin-top: 0.75rem;
    padding-bottom: 0.5rem;
  }
  .pitched-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.3rem;
  }
  .pitched-title {
    margin: 0;
    font-size: 0.85rem;
    color: var(--accent-light);
    font-weight: 600;
  }
  .pitched-delete-btn {
    background: none;
    border: 1px solid #4a2a2a;
    border-radius: 4px;
    color: #e57373;
    font-size: 0.8rem;
    cursor: pointer;
    padding: 0.15rem 0.4rem;
  }
  .pitched-delete-btn:hover { background: #3a1a1a; }
  .pitched-playback {
    padding-bottom: 0.3rem;
    margin-bottom: 0.3rem;
  }
  .pitched-stem {
    opacity: 0.85;
  }
</style>
