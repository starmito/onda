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

// ── Shared mute/solo/volume logic for stems and tracks ──

export interface StemMixState {
  muted: boolean;
  solo: boolean;
  volume: number; // 0-100
}

export const defaultStemMixState: StemMixState = { muted: false, solo: false, volume: 100 };

/** Effective gain for a stem given its own state and whether any stem in the group is soloed. */
export function effectiveStemGain(state: StemMixState | undefined, groupHasSolo: boolean): number {
  const s = state || defaultStemMixState;
  if (s.muted) return 0;
  if (groupHasSolo && !s.solo) return 0;
  return (s.volume ?? 100) / 100;
}

/** Whether any stem in the provided states is soloed.
 *  If stemKeys is provided, only those keys are considered; otherwise all states are scanned.
 */
export function anyStemSolo(states: Record<string, StemMixState>, stemKeys?: string[]): boolean {
  if (stemKeys) {
    return stemKeys.some((k) => states[k]?.solo);
  }
  return Object.values(states).some((s) => s?.solo);
}

/** Compute effective gain for each stem key in a group. */
export function computeGroupGains(
  states: Record<string, StemMixState>,
  stemKeys: string[],
): Record<string, number> {
  const hasSolo = anyStemSolo(states, stemKeys);
  const gains: Record<string, number> = {};
  for (const key of stemKeys) {
    gains[key] = effectiveStemGain(states[key], hasSolo);
  }
  return gains;
}

/** Return a new state with mute toggled. */
export function toggleStemMute(state: StemMixState | undefined): StemMixState {
  return { ...(state || defaultStemMixState), muted: !(state?.muted ?? false) };
}

/** Return a new state with solo toggled. */
export function toggleStemSolo(state: StemMixState | undefined): StemMixState {
  return { ...(state || defaultStemMixState), solo: !(state?.solo ?? false) };
}

/** Return a new state with the volume updated. */
export function setStemVolume(state: StemMixState | undefined, volume: number): StemMixState {
  return { ...(state || defaultStemMixState), volume };
}

/** Ensure a stem state exists in the record and return it. */
export function ensureStemState(
  states: Record<string, StemMixState>,
  key: string,
): StemMixState {
  if (!states[key]) {
    states[key] = { ...defaultStemMixState };
  }
  return states[key];
}

// ── Shared mute/solo logic for DAW tracks (0-1 volume range) ──

export interface TrackMixState {
  muted: boolean;
  solo: boolean;
  volume: number; // 0-1
}

export const defaultTrackMixState: TrackMixState = { muted: false, solo: false, volume: 1 };

/** Effective volume for a track given its own state and whether any track in the group is soloed. */
export function effectiveTrackVolume(state: TrackMixState | undefined, groupHasSolo: boolean): number {
  const s = state || defaultTrackMixState;
  if (s.muted) return 0;
  if (groupHasSolo && !s.solo) return 0;
  return s.volume ?? 1;
}

/** Whether any track in the array is soloed. */
export function anyTrackSolo(tracks: TrackMixState[]): boolean {
  return tracks.some((t) => t.solo);
}

// Backwards-compatible helpers used by older subgroup code in ResultsPanel.
export function effectiveGainFromState(
  state: StemMixState | undefined,
  hasSolo: boolean,
): number {
  return effectiveStemGain(state, hasSolo);
}
