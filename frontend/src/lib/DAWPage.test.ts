import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount } from 'svelte';
import DAWPage from './DAWPage.svelte';

vi.mock('wavesurfer.js', () => ({
  default: class WaveSurfer {
    volume = 1;
    static create() {
      return new WaveSurfer();
    }
    on(_event: string, handler: () => void) {
      if (_event === 'ready') {
        // Simulate async ready so applyTrackAudioState runs
        setTimeout(handler, 0);
      }
    }
    load() {}
    empty() {}
    destroy() {}
    setVolume(v: number) {
      this.volume = v;
    }
    getVolume() {
      return this.volume;
    }
    zoom() {}
    getDuration() {
      return 10;
    }
    getCurrentTime() {
      return 0;
    }
    setTime() {}
    play() {}
    pause() {}
    stop() {}
    playPause() {}
  },
}));

vi.mock('wavesurfer.js/dist/plugins/regions.js', () => ({
  default: {
    create: () => ({
      getRegions: () => [],
      clearRegions: () => {},
      addRegion: () => {},
    }),
  },
}));

vi.mock('wavesurfer.js/dist/plugins/timeline.js', () => ({
  default: {
    create: () => ({}),
  },
}));

describe('DAWPage mute/solo controls', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(0)),
        json: () => Promise.resolve({}),
      } as Response)
    );
  });

  it('renders the DAW transport and track area', async () => {
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(DAWPage, { target });
    await new Promise((r) => setTimeout(r, 50));
    expect(target.textContent).toContain('DAW');
    expect(target.querySelectorAll('button').length).toBeGreaterThan(0);
  });
});
