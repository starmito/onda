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
        value: 0,
        default: 0,
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

  it('shows "medido" label with sample count and measured flags when VRAM calculation is measured', async () => {
    const measuredCalc: VRAMCalculatorResponse = {
      models: [{
        name: 'BS-Rofo-SW-Fixed',
        type: 'vocal',
        vram_mb: 2441,
        source: 'measured',
        measured_mb: 2441,
        measured_n: 128,
        measured_ts: '2026-09-24T19:11:39Z',
        measured_flags: 'batch_size=1, chunk_size=0, segment_size=1101',
        estimated_mb: 2000,
      }],
      total_vram_mb: 2441,
      available_vram_mb: 15475,
      free_after_mb: 13034,
      fits: true,
      reliable: true,
    };

    globalThis.fetch = vi.fn().mockImplementation(async (url: string | URL) => {
      const u = url.toString();
      if (u.includes('/api/models/list')) {
        return { ok: true, status: 200, json: async () => models } as Response;
      }
      if (u.includes('/api/gpu/info')) {
        return { ok: true, status: 200, json: async () => gpuInfo } as Response;
      }
      if (u.includes('models') && u.includes('config')) {
        return { ok: true, status: 200, json: async () => swFlags } as Response;
      }
      if (u.includes('/api/gpu/vram-calculator')) {
        return { ok: true, status: 200, json: async () => measuredCalc } as Response;
      }
      throw new Error(`Unexpected fetch: ${u}`);
    });

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    await vi.waitFor(
      () => expect(target.textContent).toContain('medido'),
      { timeout: 2000 },
    );

    const text = target.textContent ?? '';
    expect(text).toContain('n=128');
    expect(text).toContain('batch_size=1');
    expect(text).toContain('segment_size=1101');
    expect(text).toContain('2.4 GB');
    expect(text).not.toContain('estimado');

    unmount(app);
  });

  it('shows "medido" label with model-level flags fallback when VRAM calculation uses cascade level 2', async () => {
    const measuredCalc: VRAMCalculatorResponse = {
      models: [{
        name: 'BS-Rofo-SW-Fixed',
        type: 'vocal',
        vram_mb: 4137,
        source: 'measured',
        measured_mb: 4137,
        measured_n: 92,
        measured_ts: '2026-09-24T19:11:39Z',
        measured_flags: 'a nivel modelo',
        estimated_mb: 2000,
      }],
      total_vram_mb: 4137,
      available_vram_mb: 15475,
      free_after_mb: 11338,
      fits: true,
      reliable: true,
    };

    globalThis.fetch = vi.fn().mockImplementation(async (url: string | URL) => {
      const u = url.toString();
      if (u.includes('/api/models/list')) {
        return { ok: true, status: 200, json: async () => models } as Response;
      }
      if (u.includes('/api/gpu/info')) {
        return { ok: true, status: 200, json: async () => gpuInfo } as Response;
      }
      if (u.includes('models') && u.includes('config')) {
        return { ok: true, status: 200, json: async () => swFlags } as Response;
      }
      if (u.includes('/api/gpu/vram-calculator')) {
        return { ok: true, status: 200, json: async () => measuredCalc } as Response;
      }
      throw new Error(`Unexpected fetch: ${u}`);
    });

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    await vi.waitFor(
      () => expect(target.textContent).toContain('medido'),
      { timeout: 2000 },
    );

    const text = target.textContent ?? '';
    expect(text).toContain('n=92');
    expect(text).toContain('a nivel modelo');
    expect(text).toContain('4.0 GB');
    expect(text).not.toContain('estimado');

    unmount(app);
  });

  it('sends the backend model identifier and the model\'s own flags to the VRAM calculator', async () => {
    const viperFlags: ModelFlagsResponse = {
      model: 'BS_Roformer_Viperx',
      flags: [
        {
          name: 'segment_size',
          value: 2048,
          default: 2048,
          min: 64,
          max: 2048,
          step: 1,
          editable: true,
          type: 'int',
          description: 'dim_t',
          affects: ['quality', 'vram'],
          better_side: 'quality',
        },
        {
          name: 'num_overlap',
          value: 8,
          default: 8,
          min: 1,
          max: 16,
          step: 1,
          editable: true,
          type: 'int',
          description: 'overlap',
          affects: ['quality', 'vram'],
          better_side: 'quality',
        },
        {
          name: 'chunk_size',
          value: 0,
          default: 0,
          min: 0,
          max: 600,
          step: 1,
          editable: true,
          type: 'int',
          description: 'chunk',
          affects: ['quality', 'vram'],
          better_side: 'quality',
        },
        {
          name: 'batch_size',
          value: 2,
          default: 2,
          min: 1,
          max: 8,
          step: 1,
          editable: true,
          type: 'int',
          description: 'batch',
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
          description: 'device',
          affects: [],
          better_side: '',
        },
      ],
    };

    const demucsFlags: ModelFlagsResponse = {
      model: 'htdemucs_ft',
      flags: [
        {
          name: 'segment',
          value: 7,
          default: 7,
          min: 0,
          max: 7,
          step: 1,
          editable: true,
          type: 'int',
          description: 'demucs segment',
          affects: ['quality', 'vram'],
          better_side: 'quality',
        },
        {
          name: 'shifts',
          value: 10,
          default: 10,
          min: 1,
          max: 20,
          step: 1,
          editable: true,
          type: 'int',
          description: 'shifts',
          affects: ['quality', 'vram'],
          better_side: 'quality',
        },
        {
          name: 'jobs',
          value: 8,
          default: 8,
          min: 1,
          max: 16,
          step: 1,
          editable: true,
          type: 'int',
          description: 'jobs',
          affects: ['speed'],
          better_side: 'speed',
        },
        {
          name: 'device',
          value: 'cuda',
          default: 'cuda',
          editable: true,
          type: 'choice',
          choices: ['cuda', 'cpu'],
          description: 'device',
          affects: [],
          better_side: '',
        },
      ],
    };

    const allModels: LocalModelsResponse = {
      models: [
        {
          name: 'BS-Rofo-SW-Fixed',
          installed_name: 'BS_Roformer_SW_6stem',
          display_name: 'BS_Roformer_SW_6stem',
          category: 'Roformer',
          type: 'bs_roformer',
          size_mb: 668,
          path: 'models/VR_Models/BS_Roformer_SW_6stem/BS-Rofo-SW-Fixed.ckpt',
        },
        {
          name: 'BS_Roformer_Viperx',
          display_name: 'BS_Roformer_Viperx',
          category: 'Vocal',
          type: 'bs_roformer',
          size_mb: 312,
          path: 'models/VR_Models/BS_Roformer_Viperx/BS_Roformer_Viperx.ckpt',
        },
        {
          name: 'htdemucs_ft',
          display_name: 'htdemucs_ft',
          category: 'Demucs',
          type: 'demucs',
          size_mb: 82,
          path: 'models/Demucs_Models/htdemucs_ft/htdemucs_ft.th',
        },
      ],
    };

    function mockForModel(initialModel: string, captured: { model: string; url: string }[]) {
      return vi.fn().mockImplementation(async (url: string | URL) => {
        const u = url.toString();
        if (u.includes('/api/models/list')) {
          return { ok: true, status: 200, json: async () => allModels } as Response;
        }
        if (u.includes('/api/gpu/info')) {
          return { ok: true, status: 200, json: async () => gpuInfo } as Response;
        }
        if (u.includes('models') && u.includes('config')) {
          if (u.includes('BS_Roformer_Viperx')) return { ok: true, status: 200, json: async () => viperFlags } as Response;
          if (u.includes('htdemucs_ft')) return { ok: true, status: 200, json: async () => demucsFlags } as Response;
          return { ok: true, status: 200, json: async () => swFlags } as Response;
        }
        if (u.includes('/api/gpu/vram-calculator')) {
          captured.push({ model: initialModel, url: u });
          return { ok: true, status: 200, json: async () => vramCalc } as Response;
        }
        throw new Error(`Unexpected fetch: ${u}`);
      });
    }

    for (const initialModel of ['BS_Roformer_SW_6stem', 'BS_Roformer_Viperx', 'htdemucs_ft']) {
      const captured: { model: string; url: string }[] = [];
      globalThis.fetch = mockForModel(initialModel, captured);

      const app = mount(ModelManager, {
        target,
        props: { initialModel },
      });

      await vi.waitFor(() => expect(getFlagLabels().length).toBeGreaterThan(0), { timeout: 2000 });
      // Wait for the debounced VRAM calculator call.
      await vi.waitFor(() => expect(captured.length).toBeGreaterThan(0), { timeout: 2000 });

      const call = captured[0];
      expect(call.url).toContain(`models=${initialModel}`);

      if (initialModel === 'BS_Roformer_SW_6stem') {
        expect(call.url).toContain('segment_size=1101');
        expect(call.url).toContain('num_overlap=2');
        expect(call.url).toContain('chunk_size=0');
        expect(call.url).toContain('batch_size=1');
        expect(call.url).not.toContain('shifts=');
      } else if (initialModel === 'BS_Roformer_Viperx') {
        expect(call.url).toContain('segment_size=2048');
        expect(call.url).toContain('num_overlap=8');
        expect(call.url).toContain('chunk_size=0');
        expect(call.url).toContain('batch_size=2');
        expect(call.url).not.toContain('shifts=');
      } else if (initialModel === 'htdemucs_ft') {
        expect(call.url).toContain('demucs_segment=7');
        expect(call.url).toContain('shifts=10');
        expect(call.url).toContain('jobs=8');
        expect(call.url).not.toContain('segment_size=');
      }

      unmount(app);
    }
  });

  it('shows "estimado" label and fallback reason when VRAM calculation is estimated', async () => {
    const estimatedCalc: VRAMCalculatorResponse = {
      models: [{
        name: 'BS-Rofo-SW-Fixed',
        type: 'vocal',
        vram_mb: 2000,
        source: 'estimated',
        estimated_mb: 2000,
        fallback_reason: 'no hay medición para este modelo y dispositivo',
      }],
      total_vram_mb: 2000,
      available_vram_mb: 15475,
      free_after_mb: 13475,
      fits: true,
      reliable: true,
    };

    globalThis.fetch = vi.fn().mockImplementation(async (url: string | URL) => {
      const u = url.toString();
      if (u.includes('/api/models/list')) {
        return { ok: true, status: 200, json: async () => models } as Response;
      }
      if (u.includes('/api/gpu/info')) {
        return { ok: true, status: 200, json: async () => gpuInfo } as Response;
      }
      if (u.includes('models') && u.includes('config')) {
        return { ok: true, status: 200, json: async () => swFlags } as Response;
      }
      if (u.includes('/api/gpu/vram-calculator')) {
        return { ok: true, status: 200, json: async () => estimatedCalc } as Response;
      }
      throw new Error(`Unexpected fetch: ${u}`);
    });

    const app = mount(ModelManager, {
      target,
      props: { initialModel: 'BS-Rofo-SW-Fixed' },
    });

    await vi.waitFor(
      () => expect(target.textContent).toContain('estimado'),
      { timeout: 2000 },
    );

    const text = target.textContent ?? '';
    expect(text).toContain('no hay medición');
    expect(text).toContain('2.0 GB');
    expect(text).not.toContain('medido');

    unmount(app);
  });
});
