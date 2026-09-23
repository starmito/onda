import { describe, it, expect } from 'vitest';
import {
  effectiveStemGain,
  computeGroupGains,
  toggleStemMute,
  toggleStemSolo,
  setStemVolume,
  effectiveTrackVolume,
  defaultStemMixState,
  defaultTrackMixState,
} from './playerStore.svelte';

describe('effectiveStemGain', () => {
  it('returns 0 when the stem is muted', () => {
    expect(effectiveStemGain({ ...defaultStemMixState, muted: true, volume: 100 }, false)).toBe(0);
  });

  it('returns 0 when another stem is soloed and this one is not', () => {
    expect(effectiveStemGain({ ...defaultStemMixState, solo: false, volume: 100 }, true)).toBe(0);
  });

  it('returns the stem volume when no solo is active', () => {
    expect(effectiveStemGain({ ...defaultStemMixState, volume: 75 }, false)).toBe(0.75);
  });

  it('returns the stem volume when it is the soloed stem', () => {
    expect(effectiveStemGain({ ...defaultStemMixState, solo: true, volume: 50 }, true)).toBe(0.5);
  });

  it('treats undefined state as default (unmuted, no solo, full volume)', () => {
    expect(effectiveStemGain(undefined, false)).toBe(1);
  });
});

describe('computeGroupGains', () => {
  it('silences a muted stem and keeps others unchanged', () => {
    const states = {
      a: { ...defaultStemMixState, muted: false, volume: 100 },
      b: { ...defaultStemMixState, muted: true, volume: 100 },
    };
    const gains = computeGroupGains(states, ['a', 'b']);
    expect(gains).toEqual({ a: 1, b: 0 });
  });

  it('silences non-solo stems when any stem is soloed', () => {
    const states = {
      a: { ...defaultStemMixState, solo: true, volume: 100 },
      b: { ...defaultStemMixState, solo: false, volume: 100 },
      c: { ...defaultStemMixState, solo: false, volume: 100 },
    };
    const gains = computeGroupGains(states, ['a', 'b', 'c']);
    expect(gains).toEqual({ a: 1, b: 0, c: 0 });
  });

  it('returns all stems at full volume when no state is present', () => {
    const gains = computeGroupGains({}, ['a', 'b']);
    expect(gains).toEqual({ a: 1, b: 1 });
  });

  it('applies per-stem volume even when a solo is active', () => {
    const states = {
      a: { ...defaultStemMixState, solo: true, volume: 80 },
      b: { ...defaultStemMixState, solo: false, volume: 100 },
    };
    const gains = computeGroupGains(states, ['a', 'b']);
    expect(gains).toEqual({ a: 0.8, b: 0 });
  });
});

describe('stem state toggles', () => {
  it('toggleStemMute flips muted and preserves volume/solo', () => {
    const next = toggleStemMute({ ...defaultStemMixState, muted: false, solo: true, volume: 42 });
    expect(next).toEqual({ muted: true, solo: true, volume: 42 });
  });

  it('toggleStemSolo flips solo and preserves volume/muted', () => {
    const next = toggleStemSolo({ ...defaultStemMixState, muted: true, solo: false, volume: 60 });
    expect(next).toEqual({ muted: true, solo: true, volume: 60 });
  });

  it('setStemVolume updates volume and preserves mute/solo', () => {
    const next = setStemVolume({ ...defaultStemMixState, muted: true, solo: true, volume: 50 }, 33);
    expect(next).toEqual({ muted: true, solo: true, volume: 33 });
  });
});

describe('effectiveTrackVolume', () => {
  it('returns 0 when the track is muted', () => {
    expect(effectiveTrackVolume({ ...defaultTrackMixState, muted: true, volume: 1 }, false)).toBe(0);
  });

  it('returns 0 when another track is soloed and this one is not', () => {
    expect(effectiveTrackVolume({ ...defaultTrackMixState, solo: false, volume: 1 }, true)).toBe(0);
  });

  it('returns the track volume when no solo is active', () => {
    expect(effectiveTrackVolume({ ...defaultTrackMixState, volume: 0.75 }, false)).toBe(0.75);
  });

  it('returns the track volume when it is the soloed track', () => {
    expect(effectiveTrackVolume({ ...defaultTrackMixState, solo: true, volume: 0.5 }, true)).toBe(0.5);
  });
});


