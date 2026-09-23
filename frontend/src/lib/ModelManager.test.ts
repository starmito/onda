import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { mount, unmount } from 'svelte';
import ModelManager from './ModelManager.svelte';
import type { LocalModelsResponse, ModelFlagsResponse, VRAMCalculatorResponse, GpuInfo } from './api';

describe('ModelManager flags from API', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const swFlags: ModelFlagsResponse = {
    model: 'BS-Rofo-SW-Fixed',
    flags: [
      {
        name: 'segment_size',
        value: 1101,
        default: 1101,
        min: 64,
        max: 2048,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Número de muestras de audio que el modelo procesa en cada ventana de análisis.',
        affects: ['quality', 'vram'],
        better_side: 'quality',
      },
      {
        name: 'num_overlap',
        value: 2,
        default: 2,
        min: 1,
        max: 16,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Número de ventanas solapadas entre segmentos consecutivos.',
        affects: ['quality', 'vram'],
        better_side: 'quality',
      },
      {
        name: 'chunk_size',
        value: 485100,
        default: 485100,
        min: 0,
        max: 1000000,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Duración máxima de cada trozo procesado, en segundos. 0 procesa la canción entera.',
        affects: ['quality', 'vram'],
        better_side: 'quality',
      },
      {
        name: 'batch_size',
        value: 1,
        default: 1,
        min: 0,
        max: 32,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Número de segmentos procesados a la vez.',
        affects: ['vram', 'speed'],
        better_side: 'vram',
      },
      {
        name: 'device',
        value: 'cuda',
        default: 'cuda',
        editable: true,
        type: 'choice',
        choices: ['cuda', 'cpu'],
        description: 'Dispositivo de cálculo: CUDA (GPU) o CPU.',
        affects: [],
        better_side: '',
      },
    ],
  };

  const mdxFlags: ModelFlagsResponse = {
    model: 'mdx23c_d1581',
    flags: [
      {
        name: 'segment_size',
        value: 256,
        default: 256,
        min: 64,
        max: 2048,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Número de muestras de audio que el modelo procesa en cada ventana de análisis.',
        affects: ['quality', 'vram'],
        better_side: 'quality',
      },
      {
        name: 'num_overlap',
        value: 2,
        default: 2,
        min: 1,
        max: 16,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Número de ventanas solapadas entre segmentos consecutivos.',
        affects: ['quality', 'vram'],
        better_side: 'quality',
      },
      {
        name: 'batch_size',
        value: 1,
        default: 1,
        min: 0,
        max: 32,
        step: 1,
        editable: true,
        type: 'int',
        description: 'Número de segmentos procesados a la vez.',
        affects: ['vram', 'speed'],
        better_side: 'vram',
      },
      {
        name: 'device',
        value: 'cuda',
        default: 'cuda',
        editable: true,
        type: 'choice',
        choices: ['cuda', 'cpu'],
        description: 'Dispositivo de cálculo: CUDA (GPU) o CPU.',
        affects: [],
        better_side: '',
      },
    ],
  };

  const models: LocalModelsResponse = {
    models: [
      {
        name: 'BS-Rofo-SW-Fixed',
        display_name: 'BS_Roformer_SW_6stem',
        category: 'Roformer',
        type: 'bs_roformer',
        size_mb: 668,
        vram_estimate_mb: 668,
        path: 'models/VR_Models/BS_Roformer_SW_6stem/BS-Rofo-SW-Fixed.ckpt',
      },
      {
        name: 'mdx23c_d1581',
        display_name: 'MDX23C_D1581',
        category: 'MDX',
        type: 'mdx23c',
        size_mb: 341,
        vram_estimate_mb: 341,
        path: 'models/MDX_Net_Models/MDX23C_D1581/mdx23c_d1581.ckpt',
      },
    ],
  };

  const gpuInfo: GpuInfo = {
    name: 'nvidia-smi',
    vram_total_mb: 16311,
    vram_used_mb: 376,
    vram_free_mb: 15475,
    temperature_c: 45,
    runtime: 'nvidia-smi',
    ok: true,
    usable_by_torch: true,
  };

  const vramCalc: VRAMCalculatorResponse = {
    models: [{ name: 'test', type: 'vocal', vram_mb: 1200 }],
    total_vram_mb: 1200,
    available_vram_mb: 15475,
    free_after_mb: 14275,
    fits: true,
    reliable: true,
  };

  function mockFetch(initialModel: string) {
    return vi.fn().mockImplementation(async (url: string | URL) => {
      const u = url.toString();
      if (u.includes('/api/models/list')) {
        return {
          ok: true,
          status: 200,
          json: async () => models,
        } as Response;
      }
      if (u.includes('/api/gpu/info')) {
        return {
          ok: true,
          status: 200,
          json: async () => gpuInfo,
        } as Response;
      }
      if (u.includes('models') && u.includes('config')) {
        return {
          ok: true,
          status: 200,
          json: async () => (initialModel === 'mdx23c_d1581' ? mdxFlags : swFlags),
        } as Response;
      }
      if (u.includes('/api/gpu/vram-calculator')) {
        return {
          ok: true,
          status: 200,
          json: async () => vramCalc,
        } as Response;
      }
      throw new Error(`Unexpected fetch: ${u}`);
    });
  }

  function getFlagLabels() {
    return Array.from(target.querySelectorAll('label[for^="flag-"]')).map(
      (el) => (el.textContent ?? '').split(':')[0].trim(),
    );
  }

  it('renders only the flags returned by the API for BS_Roformer_SW_6stem', async () => {
    globalThis.fetch = mockFetch('BS-Rofo-SW-Fixed');

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    // Wait for async loads and the debounced VRAM calculator.
    await vi.waitFor(() => expect(getFlagLabels().length).toBeGreaterThan(0), { timeout: 2000 });

    const labels = getFlagLabels();
    expect(labels).toContain('segment_size');
    expect(labels).toContain('num_overlap');
    expect(labels).toContain('chunk_size');
    expect(labels).toContain('batch_size');
    expect(labels).toContain('device');

    unmount(app);
  });

  it('renders only the flags returned by the API for MDX23C_D1581 (no chunk_size)', async () => {
    globalThis.fetch = mockFetch('mdx23c_d1581');

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'mdx23c_d1581' },
    });

    await vi.waitFor(() => expect(getFlagLabels().length).toBeGreaterThan(0), { timeout: 2000 });

    const labels = getFlagLabels();
    expect(labels).toContain('segment_size');
    expect(labels).toContain('num_overlap');
    expect(labels).toContain('batch_size');
    expect(labels).toContain('device');
    expect(labels).not.toContain('chunk_size');

    unmount(app);
  });

  it('renders descriptions and slider corner labels from flag metadata', async () => {
    globalThis.fetch = mockFetch('BS-Rofo-SW-Fixed');

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    await vi.waitFor(() => expect(getFlagLabels().length).toBeGreaterThan(0), { timeout: 2000 });

    const overlapField = target.querySelector('[data-flag-name="num_overlap"]');
    expect(overlapField?.textContent).toContain('Número de ventanas solapadas');

    const overlapLabels = overlapField?.querySelectorAll('.slider-labels span');
    expect(overlapLabels?.length).toBe(2);
    expect(overlapLabels?.[0].textContent).toBe('menos solape · menos calidad');
    expect(overlapLabels?.[1].textContent).toBe('más solape · más calidad');

    const batchField = target.querySelector('[data-flag-name="batch_size"]');
    const batchLabels = batchField?.querySelectorAll('.slider-labels span');
    expect(batchLabels?.[0].textContent).toBe('menos segmentos · menos VRAM');
    expect(batchLabels?.[1].textContent).toBe('más segmentos · más VRAM');

    unmount(app);
  });

  it('does not render slider labels for flags without affects metadata', async () => {
    globalThis.fetch = mockFetch('BS-Rofo-SW-Fixed');

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    await vi.waitFor(() => expect(getFlagLabels().length).toBeGreaterThan(0), { timeout: 2000 });

    const deviceField = target.querySelector('[data-flag-name="device"]');
    expect(deviceField?.querySelector('.slider-labels')).toBeNull();

    unmount(app);
  });

  it('sends the real value (not the visual one) when an inverted slider changes', async () => {
    let savedBody: Record<string, unknown> | null = null;

    globalThis.fetch = vi.fn().mockImplementation(async (url: string | URL, init?: RequestInit) => {
      const u = url.toString();
      if (u.includes('/api/models/list')) {
        return { ok: true, status: 200, json: async () => models } as Response;
      }
      if (u.includes('/api/gpu/info')) {
        return { ok: true, status: 200, json: async () => gpuInfo } as Response;
      }
      if (u.includes('models') && u.includes('config') && init?.method === 'POST') {
        savedBody = JSON.parse(init.body as string);
        return { ok: true, status: 200, json: async () => ({ ok: 'ok', detail: 'saved' }) } as Response;
      }
      if (u.includes('models') && u.includes('config')) {
        return { ok: true, status: 200, json: async () => swFlags } as Response;
      }
      if (u.includes('/api/gpu/vram-calculator')) {
        return { ok: true, status: 200, json: async () => vramCalc } as Response;
      }
      throw new Error(`Unexpected fetch: ${u}`);
    });

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    await vi.waitFor(() => expect(getFlagLabels().length).toBeGreaterThan(0), { timeout: 2000 });

    // chunk_size is inverted: visual 0 (left) = real max (1000000).
    const chunkInput = target.querySelector('#flag-chunk_size') as HTMLInputElement;
    chunkInput.value = '0';
    chunkInput.dispatchEvent(new Event('input', { bubbles: true }));

    const applyBtn = target.querySelector('.btn-apply') as HTMLButtonElement;
    applyBtn.click();

    await vi.waitFor(() => expect(savedBody).not.toBeNull(), { timeout: 2000 });
    expect((savedBody as unknown as { flags: Record<string, unknown> }).flags.chunk_size).toBe(1000000);

    unmount(app);
  });
});
