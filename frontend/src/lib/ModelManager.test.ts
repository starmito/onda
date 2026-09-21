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
      { name: 'segment_size', value: 1101, default: 1101, min: 64, max: 2048, step: 1, editable: true, type: 'int' },
      { name: 'num_overlap', value: 2, default: 2, min: 1, max: 16, step: 1, editable: true, type: 'int' },
      { name: 'chunk_size', value: 485100, default: 485100, min: 0, max: 1000000, step: 1, editable: true, type: 'int' },
      { name: 'batch_size', value: 1, default: 1, min: 0, max: 32, step: 1, editable: true, type: 'int' },
      { name: 'device', value: 'cuda', default: 'cuda', editable: true, type: 'choice', choices: ['cuda', 'cpu'] },
    ],
  };

  const mdxFlags: ModelFlagsResponse = {
    model: 'mdx23c_d1581',
    flags: [
      { name: 'segment_size', value: 256, default: 256, min: 64, max: 2048, step: 1, editable: true, type: 'int' },
      { name: 'num_overlap', value: 2, default: 2, min: 1, max: 16, step: 1, editable: true, type: 'int' },
      { name: 'batch_size', value: 1, default: 1, min: 0, max: 32, step: 1, editable: true, type: 'int' },
      { name: 'device', value: 'cuda', default: 'cuda', editable: true, type: 'choice', choices: ['cuda', 'cpu'] },
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
    global.fetch = mockFetch('BS-Rofo-SW-Fixed');

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
    global.fetch = mockFetch('mdx23c_d1581');

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
});
