import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { mount } from 'svelte';
import DAWPage from './DAWPage.svelte';
import WaveSurfer from 'wavesurfer.js';
import RegionsPlugin from 'wavesurfer.js/dist/plugins/regions.js';
import TimelinePlugin from 'wavesurfer.js/dist/plugins/timeline.js';

// This test exercises the v8 plugin registration path.
// It must fail when plugins are passed to WaveSurfer.create() instead of
// being registered via ws.registerPlugin().

function createMockRegion(params: { start: number; end: number; color?: string }) {
  return {
    id: `region_${Math.random().toString(36).slice(2)}`,
    start: params.start,
    end: params.end,
    color: params.color ?? 'rgba(0,0,0,0)',
    element: document.createElement('div'),
    remove: vi.fn(),
    play: vi.fn(),
    setOptions: vi.fn(),
  };
}

function createMockRegionsPlugin() {
  const handlers: Record<string, ((...args: unknown[]) => void)[]> = {};
  const regions: ReturnType<typeof createMockRegion>[] = [];
  return {
    on(event: string, handler: (...args: unknown[]) => void) {
      if (!handlers[event]) handlers[event] = [];
      handlers[event].push(handler);
      return () => {
        handlers[event] = handlers[event]?.filter((h) => h !== handler) ?? [];
      };
    },
    addRegion(params: { start: number; end: number; color?: string }) {
      const region = createMockRegion(params);
      regions.push(region);
      handlers['region-created']?.forEach((h) => h(region));
      return region;
    },
    getRegions() {
      return regions;
    },
    clearRegions() {
      regions.length = 0;
    },
    _emit(event: string, ...args: unknown[]) {
      handlers[event]?.forEach((h) => h(...args));
    },
  };
}

function createMockTimelinePlugin() {
  return {
    on: () => () => {},
  };
}

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

describe('DAWPage v8 plugin registration', () => {
  let createSpy: ReturnType<typeof vi.spyOn>;
  let regionsCreateSpy: ReturnType<typeof vi.spyOn>;
  let timelineCreateSpy: ReturnType<typeof vi.spyOn>;
  const mockedInstances: ReturnType<typeof createMockWaveSurfer>[] = [];

  beforeEach(() => {
    mockedInstances.length = 0;
    createSpy = vi.spyOn(WaveSurfer, 'create').mockImplementation((options: any) => {
      if (options.plugins) {
        throw new Error(
          'WaveSurfer v8: plugins must be registered with registerPlugin(), not passed to create()',
        );
      }
      const instance = createMockWaveSurfer();
      mockedInstances.push(instance);
      return instance as unknown as WaveSurfer;
    });

    regionsCreateSpy = vi.spyOn(RegionsPlugin, 'create').mockImplementation(() => {
      return createMockRegionsPlugin() as unknown as ReturnType<typeof RegionsPlugin.create>;
    });

    timelineCreateSpy = vi.spyOn(TimelinePlugin, 'create').mockImplementation(() => {
      return createMockTimelinePlugin() as unknown as ReturnType<typeof TimelinePlugin.create>;
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
    regionsCreateSpy.mockRestore();
    timelineCreateSpy.mockRestore();
    vi.restoreAllMocks();
    mockedInstances.length = 0;
  });

  async function importFirstTrack(target: HTMLDivElement) {
    const importBtn = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Importar',
    );
    expect(importBtn).toBeTruthy();
    importBtn!.click();
    await new Promise((r) => setTimeout(r, 50));

    const importRowBtn = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Importar' && b !== importBtn,
    );
    expect(importRowBtn).toBeTruthy();
    importRowBtn!.click();
    await new Promise((r) => setTimeout(r, 50));
  }

  it('registers Regions and Timeline plugins via registerPlugin(), not via create()', async () => {
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(DAWPage, { target });

    await importFirstTrack(target);

    expect(createSpy).toHaveBeenCalledTimes(1);
    const options = createSpy.mock.calls[0][0];
    expect(options.plugins).toBeUndefined();

    expect(mockedInstances.length).toBe(1);
    const registered = mockedInstances[0].getRegisteredPlugins();
    expect(registered.length).toBe(2);
    expect(regionsCreateSpy).toHaveBeenCalledTimes(1);
    expect(timelineCreateSpy).toHaveBeenCalledTimes(1);

    document.body.removeChild(target);
  });

  it('creates a region and emits region-created when the user adds a region', async () => {
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(DAWPage, { target });

    await importFirstTrack(target);
    // Wait for the mocked ready event so the waveform is ready.
    await new Promise((r) => setTimeout(r, 20));

    const addRegionBtn = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Añadir región',
    ) as HTMLButtonElement | undefined;
    expect(addRegionBtn).toBeTruthy();
    expect(addRegionBtn!.disabled).toBe(false);

    const regionCreatedHandler = vi.fn();
    const registered = mockedInstances[0].getRegisteredPlugins();
    const regionsPlugin = registered[0] as ReturnType<typeof createMockRegionsPlugin>;
    regionsPlugin.on('region-created', regionCreatedHandler);

    addRegionBtn!.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(regionsPlugin.getRegions().length).toBe(1);
    expect(regionCreatedHandler).toHaveBeenCalledTimes(1);
    const createdRegion = regionsPlugin.getRegions()[0];
    expect(createdRegion.start).toBe(0);
    expect(createdRegion.end).toBeGreaterThan(0);

    document.body.removeChild(target);
  });
});
