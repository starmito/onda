import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount } from 'svelte';
import ResultsPanel from './ResultsPanel.svelte';
import { playerState } from './playerStore.svelte';
import type { ResultStem } from './types';

describe('ResultsPanel subgroup seek keeps gain nodes', () => {
  beforeEach(() => {
    // Reset shared state between tests
    playerState.resultsStemStates = {};
    playerState.resultsPitchSubgroups = {};
    playerState.resultsGroupPlayers = {};

    let gainId = 0;
    globalThis.AudioContext = vi.fn(function () {
      return {
        state: 'running',
        currentTime: 10,
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
        createGain: vi.fn(() => {
          gainId += 1;
          return { id: `gain-${gainId}`, gain: { value: 1 }, connect: vi.fn() };
        }),
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

    globalThis.fetch = vi.fn((url: string | Request | URL) => {
      const urlStr = String(url);
      if (urlStr.includes('/api/pitch/')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve([
              {
                pitch: 2,
                files: [{ name: 'vocals_pitch+2.wav', path: '/tmp/onda-test/song1_pitch+2/vocals_pitch+2.wav' }],
              },
            ]),
        } as Response);
      }
      return Promise.resolve({
        ok: true,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(0)),
      } as Response);
    });
  });

  it('registers new gain nodes after seeking a playing subgroup', async () => {
    const files: ResultStem[] = [
      { song: 'song1', name: 'vocals.wav', path: '/tmp/onda-test/song1/vocals.wav', stemType: 'vocals' },
    ];

    const target = document.createElement('div');
    target.style.width = '800px';
    document.body.appendChild(target);
    mount(ResultsPanel, { target, props: { files } });

    // Wait for the $effect that loads pitch subgroups
    await new Promise((r) => setTimeout(r, 300));

    // Manually set up a playing subgroup player with a buffer but no gain nodes
    const fakeBuffer = { duration: 1 } as AudioBuffer;
    const fakeCtx = new AudioContext();
    playerState.resultsPitchSubgroups['song1'][0].player = {
      audioCtx: fakeCtx,
      playing: true,
      paused: false,
      currentTime: 0,
      duration: 1,
      seekValue: 0,
      sourceNodes: new Map(),
      gainNodes: new Map(),
      analysers: new Map(),
      buffers: new Map([['vocals_pitch+2.wav', fakeBuffer]]),
      startTime: 10,
      pauseOffset: 0,
      animFrame: null,
      loaded: true,
    };

    // Force re-render so the subgroup UI appears with the player
    await new Promise((r) => setTimeout(r, 50));

    const canvas = target.querySelector('canvas.waveform-seek') as HTMLCanvasElement;
    expect(canvas).toBeTruthy();

    // Give the canvas a size so waveform drawing does not bail out early
    Object.defineProperty(canvas, 'clientWidth', { value: 400, writable: true });
    Object.defineProperty(canvas, 'clientHeight', { value: 80, writable: true });

    const rect = { left: 0, top: 0, width: 400, height: 80 };
    canvas.getBoundingClientRect = () => rect as DOMRect;

    // Mouse down + up on the seek canvas triggers pitchedSeek
    canvas.dispatchEvent(new MouseEvent('mousedown', { clientX: 200, clientY: 40, bubbles: true }));
    canvas.dispatchEvent(new MouseEvent('mouseup', { clientX: 200, clientY: 40, bubbles: true }));

    await new Promise((r) => setTimeout(r, 50));

    const player = playerState.resultsPitchSubgroups['song1'][0].player!;
    // After the fix, the gain node created during seek is registered so that
    // later mute/solo updates can reach the active source.
    expect(player.gainNodes.size).toBe(1);
    expect(player.gainNodes.has('vocals_pitch+2.wav')).toBe(true);
  });
});
