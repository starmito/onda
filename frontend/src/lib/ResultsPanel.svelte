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

  // Pitch shift state
  let pitchSliderValue = $state<Record<string, number>>({});
  let pitchProcessing = $state<Record<string, boolean>>({});

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
      buffers: Map<string, AudioBuffer>;
      startTime: number;
      pauseOffset: number;
      animFrame: number | null;
      loaded: boolean;
    } | null;
  }
  let pitchSubgroups = $state<Record<string, PitchedSubgroup[]>>({});

  async function loadPitchSubgroups(song: string) {
    try {
      const subs = await getPitchSubgroups(song);
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
    } catch {
      // ignore
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
        const resp = await fetch(url);
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
        seekValue: 0, sourceNodes: new Map(), gainNodes: new Map(), buffers: new Map(),
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
          source.connect(gain);
          gain.connect(player.audioCtx.destination);
          source.start(0, player.pauseOffset);
          player.sourceNodes.set(name, source);
          player.gainNodes.set(name, gain);
        }
        startSubgroupTimer(song, pitch);
        return;
      }

      // Start fresh: load and play all stems
      player.loaded = false;
      const loadPromises = sg.stems.map(async (stem) => {
        if (player.buffers.has(stem.name)) return;
        const url = pitchDownloadUrl(song, pitch, stem.name);
        const resp = await fetch(url);
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
        source.connect(gain);
        gain.connect(player.audioCtx.destination);
        source.start(0);
        player.sourceNodes.set(name, source);
        player.gainNodes.set(name, gain);
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
    player.duration = 0;
    player.loaded = false;
    player.buffers.clear();
    player.gainNodes.clear();
    pitchSubgroups[song] = [...subs];
    pitchSubgroups = { ...pitchSubgroups };
  }

  function startSubgroupTimer(song: string, pitch: number) {
    const subs = pitchSubgroups[song] || [];
    const sg = subs.find(s => s.pitch === pitch);
    const player = sg?.player;
    if (!player) return;

    function tick() {
      if (!player.playing || player.paused) return;
      player.currentTime = player.audioCtx!.currentTime - player.startTime;
      player.seekValue = player.currentTime;
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
        source.connect(gain);
        gain.connect(player.audioCtx!.destination);
        source.start(0, seekTo);
        player.sourceNodes.set(name, source);
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
    if (!player || !player.playing) return;
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
        pitchSubgroups[song] = [...subs];
        pitchSubgroups = { ...pitchSubgroups };
      }
      showToast(`Stem "${stemName}" eliminado`, 'success');
    } catch (e) {
      showToast(`Error: ${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  }

  // ---- $effect to load pitch subgroups when songGroups changes ----

  $effect(() => {
    const songs = songGroups.map(g => g.song);
    for (const song of songs) {
      loadPitchSubgroups(song);
    }
  });

  let waveformDrawn = $state<Set<string>>(new Set());

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

    let audioCtx: AudioContext | undefined;
    try {
      const url = downloadUrl(song, name);
      const resp = await fetch(url);
      const arrayBuf = await resp.arrayBuffer();
      audioCtx = new AudioContext();
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
        ctx.fillStyle = '#00d4ff';
        ctx.fillRect(i, (h - barH) / 2, 1, barH);
      }
    } catch {
      // Fallback: draw from stem name hash (deterministic)
      let hash = 0;
      for (let i = 0; i < key.length; i++) {
        hash = ((hash << 5) - hash) + key.charCodeAt(i);
        hash |= 0;
      }
      ctx.fillStyle = '#00d4ff';
      const barCount = 40;
      const barWidth = w / barCount;
      for (let i = 0; i < barCount; i++) {
        const hVal = ((Math.abs(hash + i * 31) % 80) / 100) * h * 0.8 + h * 0.1;
        const x = i * barWidth + 1;
        const y = (h - hVal) / 2;
        ctx.fillRect(x, y, barWidth - 2, hVal);
      }
    } finally {
      audioCtx?.close();
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
      const resp = await fetch(url);
      const arrayBuf = await resp.arrayBuffer();
      const audioCtx = new AudioContext();
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
        ctx.fillStyle = '#00d4ff';
        ctx.fillRect(i, (h - barH) / 2, 1, barH);
      }
      audioCtx.close();
    } catch {
      let hash = 0;
      for (let i = 0; i < url.length; i++) {
        hash = ((hash << 5) - hash) + url.charCodeAt(i);
        hash |= 0;
      }
      ctx.fillStyle = '#00d4ff';
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

  // Cleanup on component destroy
  onDestroy(() => {
    if (toastTimer) clearTimeout(toastTimer);
    for (const [key, player] of Object.entries(groupPlayers)) {
      player.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
      player.audioCtx?.close();
      if (player.animFrame) cancelAnimationFrame(player.animFrame);
    }
    // Cleanup subgroup players
    for (const subs of Object.values(pitchSubgroups)) {
      for (const sg of subs) {
        const p = sg.player;
        if (p) {
          p.sourceNodes.forEach(s => { try { s.stop(); } catch(e) {} });
          p.audioCtx?.close();
          if (p.animFrame) cancelAnimationFrame(p.animFrame);
        }
      }
    }
  });
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
            <div class="pitched-group">
              <!-- Subgroup header -->
              <div class="pitched-header">
                <h4 class="pitched-title">
                  {group.song} ({sg.pitch > 0 ? '+' : ''}{sg.pitch})
                </h4>
                <button class="pitched-delete-btn" onclick={() => handleDeletePitchSubgroup(group.song, sg.pitch)} title="Eliminar subgrupo">🗑</button>
              </div>

              <!-- Subgroup playback controls -->
              <div class="song-header pitched-playback">
                <button class="ctrl-btn play-btn" onclick={() => playSubgroup(group.song, sg.pitch)} disabled={subPlayer?.playing && !subPlayer?.paused}>▶</button>
                <button class="ctrl-btn pause-btn" onclick={() => pauseSubgroup(group.song, sg.pitch)} disabled={!subPlayer?.playing || subPlayer?.paused}>⏸</button>
                <button class="ctrl-btn stop-btn" onclick={() => stopSubgroup(group.song, sg.pitch)} disabled={!subPlayer?.playing && !subPlayer?.paused}>⏹</button>
                <div class="seek-area">
                  <input type="range" min="0" max={subPlayer?.duration || 100} step="0.1" value={subPlayer?.seekValue || 0}
                    disabled={!subPlayer?.playing && !subPlayer?.paused}
                    oninput={(e) => handleSubgroupSeekInput(e, group.song, sg.pitch)}
                    onchange={(e) => handleSubgroupSeekChange(e, group.song, sg.pitch)}
                    class="seek-slider" />
                  <span class="time-display">{fmtTime(subPlayer?.currentTime)} / {fmtTime(subPlayer?.duration)}</span>
                </div>
              </div>

              <!-- Subgroup stems -->
              <div class="stems-list">
                {#each sg.stems as stem}
                  {@const stemId = `pitch:${group.song}:${sg.pitch}:${stem.name}`}
                  {@const subState = stemStates[stemId] ?? { muted: false, solo: false, volume: 100 }}
                  <div class="stem-row pitched-stem" class:muted={subState.muted}>
                    <canvas class="waveform-mini" width="120" height="28"
                      data-url={pitchDownloadUrl(group.song, sg.pitch, stem.name)}
                      use:waveformUrl></canvas>
                    <span class="stem-emoji">{stemEmoji(stem.stemType)}</span>
                    <span class="stem-name" title={stem.name}>{stem.name}</span>
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
    align-items: center;
    gap: 0.5rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid #2a2a3e;
    margin-bottom: 0.5rem;
    flex-wrap: wrap;
  }

  .song-name {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: #00d4ff;
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
    background: #00d4ff;
    color: #0a0a14;
  }
  .play-btn:not(:disabled):hover {
    background: #00b8e0;
  }
  .pause-btn {
    background: #ff9800;
    color: #0a0a14;
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
    background: #2a2a3e;
    outline: none;
    cursor: pointer;
  }
  .seek-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 12px;
    height: 12px;
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
    font-size: 0.7rem;
    color: #888;
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
    width: 26px;
    height: 26px;
    min-width: 24px;
    min-height: 24px;
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
    border-top: 1px solid #2a2a3e;
    margin-top: 0.5rem;
  }
  .pitch-label {
    font-size: 0.8rem;
    color: #c0c0d0;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .pitch-slider {
    flex: 1;
    max-width: 150px;
    accent-color: #b388ff;
    cursor: pointer;
  }
  .pitch-apply-btn {
    padding: 0.3rem 0.8rem;
    background: #b388ff;
    border: none;
    border-radius: 6px;
    color: #0a0a14;
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .pitch-apply-btn:hover:not(:disabled) { opacity: 0.9; }
  .pitch-apply-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .pitched-group {
    margin-left: 1.5rem;
    border-left: 2px solid #b388ff44;
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
    color: #b388ff;
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
