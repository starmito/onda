import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { mount } from 'svelte';
import DAWPage from './DAWPage.svelte';
import WaveSurfer from 'wavesurfer.js';

function createMockWaveSurfer() {
  const handlers: Record<string, (() => void)[]> = {};
  let loadedUrl: string | null = null;
  const registeredPlugins: unknown[] = [];
  return {
    on(event: string, handler: () => void) {
      if (!handlers[event]) handlers[event] = [];
      handlers[event].push(handler);
      return () => {
        handlers[event] = handlers[event]?.filter((h) => h !== handler) ?? [];
      };
    },
    un(_event: string, _handler: () => void) {},
    registerPlugin(plugin: unknown) {
      registeredPlugins.push(plugin);
      return plugin;
    },
    getRegisteredPlugins() {
      return registeredPlugins;
    },
    load(url: string) {
      loadedUrl = url;
      // Only signal readiness when the component actually provided a real URL.
      // This makes the test fail if the old code calls load('') or skips load().
      if (url) {
        setTimeout(() => {
          handlers['ready']?.forEach((h) => h());
        }, 0);
      }
    },
    empty() {},
    destroy() {},
    setVolume() {},
    getVolume() {
      return 1;
    },
    zoom() {},
    getDuration() {
      return 10;
    },
    getCurrentTime() {
      return 0;
    },
    setTime() {},
    play() {},
    pause() {},
    stop() {},
    playPause() {},
    getWrapper() {
      return null;
    },
    getLoadedUrl() {
      return loadedUrl;
    },
    _emit(event: string) {
      handlers[event]?.forEach((h) => h());
    },
  };
}

describe('DAWPage waveform initialization', () => {
  let createSpy: ReturnType<typeof vi.spyOn>;
  const mockedInstances: ReturnType<typeof createMockWaveSurfer>[] = [];

  beforeEach(() => {
    mockedInstances.length = 0;
    createSpy = vi.spyOn(WaveSurfer, 'create').mockImplementation((options: any) => {
      const instance = createMockWaveSurfer();
      mockedInstances.push(instance);
      return instance as unknown as WaveSurfer;
    });

    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/inputs?include=all')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve([
              {
                name: 'test.wav',
                path: 'input/test.wav',
                source: 'input',
                processed: false,
              },
            ]),
        } as Response);
      }
      if (url.includes('/api/daw/stems')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ output: {}, pitch: [] }),
        } as Response);
      }
      if (url.includes('/api/daw/import')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              file: 'test.wav',
              path: 'daw-data/test/import_test.wav',
              url: 'daw-data/test/import_test.wav',
              name: 'test',
              size: 1234,
            }),
        } as Response);
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({}),
      } as Response);
    });
  });

  afterEach(() => {
    createSpy.mockRestore();
    vi.restoreAllMocks();
    mockedInstances.length = 0;
  });

  it('loads the imported audio URL into WaveSurfer and enables play', async () => {
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(DAWPage, { target });

    // Open the import panel.
    const importBtn = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Importar',
    );
    expect(importBtn).toBeTruthy();
    importBtn!.click();

    // Wait for the input list and stems to load.
    await new Promise((r) => setTimeout(r, 50));

    // Click the import button inside the Subidas list.
    const importRowBtn = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Importar' && b !== importBtn,
    );
    expect(importRowBtn).toBeTruthy();
    importRowBtn!.click();

    // Give Svelte time to bind the container and run the effect.
    await new Promise((r) => setTimeout(r, 50));

    expect(createSpy).toHaveBeenCalled();
    const options = createSpy.mock.calls[0][0];
    expect(options.container).toBeInstanceOf(HTMLDivElement);
    expect(options.container.classList.contains('track-waveform')).toBe(true);

    // WaveSurfer must have been asked to load the real URL returned by the API.
    expect(mockedInstances.length).toBeGreaterThan(0);
    const loadedUrl = mockedInstances[0].getLoadedUrl();
    expect(loadedUrl).toBe('daw-data/test/import_test.wav');

    // Wait for the mocked ready event so the play button enables.
    await new Promise((r) => setTimeout(r, 20));

    const trackPlayBtn = target.querySelector('.track-play-btn') as HTMLButtonElement | null;
    expect(trackPlayBtn).toBeTruthy();
    expect(trackPlayBtn!.disabled).toBe(false);

    document.body.removeChild(target);
  });
});
