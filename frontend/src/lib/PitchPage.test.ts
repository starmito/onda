import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount } from 'svelte';
import PitchPage from './PitchPage.svelte';
import { playerState } from './playerStore.svelte';
import type { ResultStem } from './types';

describe('PitchPage mute/solo', () => {
  beforeEach(() => {
    playerState.stemStates = {};
    playerState.groupPlayers = {};
    playerState.subgroupPlayers = {};
    playerState.pitchSubgroups = {};

    globalThis.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(0)),
        json: () => Promise.resolve([]),
      } as Response)
    );
    globalThis.AudioContext = vi.fn(function () {
      return {
        state: 'running',
        currentTime: 0,
        destination: {},
        resume: vi.fn(),
        suspend: vi.fn(),
        decodeAudioData: vi.fn(() =>
          Promise.resolve({
            duration: 1,
            getChannelData: () => new Float32Array(10),
            sampleRate: 44100,
            numberOfChannels: 1,
            length: 10,
          } as unknown as AudioBuffer)
        ),
        createGain: vi.fn(() => ({ gain: { value: 1 }, connect: vi.fn() })),
        createBufferSource: vi.fn(() => ({
          buffer: null,
          connect: vi.fn(),
          start: vi.fn(),
          stop: vi.fn(),
        })),
        createChannelSplitter: vi.fn(() => ({ connect: vi.fn() })),
        createAnalyser: vi.fn(() => ({
          fftSize: 64,
          getByteTimeDomainData: vi.fn(),
          connect: vi.fn(),
        })),
      };
    }) as unknown as typeof AudioContext;
  });

  it('renders mute and solo buttons for each output stem', async () => {
    const results: ResultStem[] = [
      { song: 'song1', name: 'vocals.wav', path: '/tmp/onda-test/song1/vocals.wav', stemType: 'vocals' },
      { song: 'song1', name: 'drums.wav', path: '/tmp/onda-test/song1/drums.wav', stemType: 'drums' },
    ];
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(PitchPage, { target, props: { results } });
    await new Promise((r) => setTimeout(r, 50));
    const buttons = target.querySelectorAll('button');
    const muteButtons = Array.from(buttons).filter((b) => b.textContent?.trim() === 'M');
    const soloButtons = Array.from(buttons).filter((b) => b.textContent?.trim() === 'S');
    expect(muteButtons.length).toBeGreaterThanOrEqual(2);
    expect(soloButtons.length).toBeGreaterThanOrEqual(2);
  });

  it('updates shared state when a stem mute button is clicked', async () => {
    const results: ResultStem[] = [
      { song: 'song1', name: 'vocals.wav', path: '/tmp/onda-test/song1/vocals.wav', stemType: 'vocals' },
      { song: 'song1', name: 'drums.wav', path: '/tmp/onda-test/song1/drums.wav', stemType: 'drums' },
    ];
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(PitchPage, { target, props: { results } });
    await new Promise((r) => setTimeout(r, 50));

    const buttons = target.querySelectorAll('button');
    const muteButtons = Array.from(buttons).filter((b) => b.textContent?.trim() === 'M');
    expect(muteButtons.length).toBeGreaterThanOrEqual(2);

    muteButtons[0].click();
    await new Promise((r) => setTimeout(r, 20));

    const activeKey = Object.keys(playerState.stemStates).find((k) =>
      playerState.stemStates[k].muted
    );
    expect(activeKey).toBeTruthy();
  });
});
