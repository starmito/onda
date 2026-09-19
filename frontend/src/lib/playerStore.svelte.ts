// ── Each uploaded file player (standalone, simple) ──
export interface UploadPlayer {
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
export interface GroupPlayer {
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
export interface SubgroupStem {
  name: string;
  path: string;
  stemType: string;
}
export interface Subgroup {
  pitch: number;
  stems: SubgroupStem[];
}
export interface SubgroupPlayer {
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

// ResultsPanel nested pitched subgroup (player lives inside the subgroup object)
export interface PitchedSubgroup {
  pitch: number;
  stems: SubgroupStem[];
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

// Module-level reactive state: survives component unmount/remount.
// AudioContexts/sources are kept alive intentionally so playback continues
// when the user navigates away from the Pitch tab.
export const playerState = $state({
  // Identifier used to verify the store is included in the served bundle.
  __storeId: 'onda-pitch-player-store-v1',
  uploadPlayers: [] as UploadPlayer[],

  // Per-stem mute/solo/volume (shared between groups and subgroups)
  stemStates: {} as Record<string, { muted: boolean; solo: boolean; volume: number }>,

  // Per-song group combined players
  groupPlayers: {} as Record<string, GroupPlayer>,

  // Pitch subgroups
  pitchSubgroups: {} as Record<string, Subgroup[]>,
  subgroupPlayers: {} as Record<string, SubgroupPlayer>,
  loadingSubgroups: {} as Record<string, boolean>,

  // Waveform peak caches (avoid re-decoding on remount)
  wavePeaksCache: {} as Record<string, number[]>,
  subgroupWavePeaksCache: {} as Record<string, number[]>,

  // Per-stem real-time peak levels (for peak meters)
  stemLevels: {} as Record<string, { l: number; r: number }>,
  // Per-stem peak hold (highest value reached during playback)
  stemPeaks: {} as Record<string, { l: number; r: number }>,

  // Subgroup peak levels and peaks
  subgroupLevels: {} as Record<string, { l: number; r: number }>,
  subgroupPeaks: {} as Record<string, { l: number; r: number }>,

  // Pitch shift UI state
  pitchValues: {} as Record<string, number>,
  pitchProcessing: {} as Record<string, boolean>,

  // Pitch shift for uploaded files
  uploadPitchValues: {} as Record<string, number>,
  uploadPitchProcessing: {} as Record<string, boolean>,
  uploadSubgroups: {} as Record<string, Subgroup[]>,

  // ── ResultsPanel persistent player state (separate namespace from PitchPage) ──
  resultsStemStates: {} as Record<string, { muted: boolean; solo: boolean; volume: number }>,
  resultsGroupPlayers: {} as Record<string, GroupPlayer>,
  resultsPitchSubgroups: {} as Record<string, PitchedSubgroup[]>,
  resultsPitchedLevels: {} as Record<string, { l: number; r: number }>,
  resultsPitchedPeaks: {} as Record<string, { l: number; r: number }>,
});

// Helper to re-hydrate Map fields after a hot reload or when a plain object
// accidentally replaces the reactive state. Svelte's $state proxies plain
// objects, but our code expects Map instances for sourceNodes/gainNodes/etc.
function ensureMap<T>(value: unknown): Map<string, T> {
  if (value instanceof Map) return value as Map<string, T>;
  return new Map<string, T>();
}

// Ensure a GroupPlayer has the correct Map instances.
export function repairGroupPlayer(player: GroupPlayer): GroupPlayer {
  return {
    ...player,
    sourceNodes: ensureMap<AudioBufferSourceNode>(player.sourceNodes),
    gainNodes: ensureMap<GainNode>(player.gainNodes),
    buffers: ensureMap<AudioBuffer>(player.buffers),
    analysers: ensureMap<AnalyserNode[]>(player.analysers),
  };
}

// Ensure a SubgroupPlayer has the correct Map instances.
export function repairSubgroupPlayer(player: SubgroupPlayer): SubgroupPlayer {
  return {
    ...player,
    sourceNodes: ensureMap<AudioBufferSourceNode>(player.sourceNodes),
    gainNodes: ensureMap<GainNode>(player.gainNodes),
    analysers: ensureMap<AnalyserNode[]>(player.analysers),
    buffers: ensureMap<AudioBuffer>(player.buffers),
  };
}

// Convenience helpers used by both PitchPage and ResultsPanel.
export function stemStateKey(song: string, name: string): string {
  return `${song}/${name}`;
}

export function getStemState(song: string, name: string) {
  const key = stemStateKey(song, name);
  return playerState.stemStates[key] || { muted: false, solo: false, volume: 100 };
}

export function subgroupStemKey(song: string, pitchIdx: number, name: string): string {
  return `subgroup:${song}:${pitchIdx}:${name}`;
}

export function getSubgroupStemState(song: string, pitchIdx: number, name: string) {
  const key = subgroupStemKey(song, pitchIdx, name);
  return playerState.stemStates[key] || { muted: false, solo: false, volume: 100 };
}
