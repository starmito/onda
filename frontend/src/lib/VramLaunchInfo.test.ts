import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { mount, unmount, type ComponentProps } from 'svelte';
import { tick } from 'svelte';
import VramLaunchInfo from './VramLaunchInfo.svelte';
import type { GpuInfo, ModelFlagsResponse, VRAMCalculatorResponse } from './api';

const gpuInfo: GpuInfo = {
  name: 'nvidia-smi',
  vram_total_mb: 8192,
  vram_used_mb: 1024,
  vram_free_mb: 7168,
  temperature_c: 45,
  runtime: 'nvidia-smi',
  ok: true,
  usable_by_torch: true,
};

const modelConfig: ModelFlagsResponse = {
  model: 'htdemucs_ft',
  flags: [
    { name: 'device', value: 'cuda', default: 'cuda', editable: true, type: 'choice', choices: ['cuda', 'cpu'] },
    { name: 'shifts', value: 1, default: 1, min: 0, max: 20, step: 1, editable: true, type: 'int' },
    { name: 'segment', value: 7, default: 7, min: 0, max: 7, step: 1, editable: true, type: 'int' },
    { name: 'jobs', value: 0, default: 0, min: 0, max: 8, step: 1, editable: true, type: 'int' },
  ],
};

const vramCalc: VRAMCalculatorResponse = {
  models: [{ name: 'htdemucs_ft', type: 'demucs', vram_mb: 1800 }],
  total_vram_mb: 1800,
  available_vram_mb: 7168,
  free_after_mb: 5368,
  fits: true,
  reliable: true,
};

vi.mock('./api', async () => {
  const actual = await vi.importActual<typeof import('./api')>('./api');
  return {
    ...actual,
    getGpuInfo: vi.fn(async () => gpuInfo),
    getModelConfig: vi.fn(async () => modelConfig),
    getVRAMCalculator: vi.fn(async () => vramCalc),
  };
});

describe('VramLaunchInfo', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function render(props: Partial<ComponentProps<typeof VramLaunchInfo>> = {}) {
    const app = mount(VramLaunchInfo, {
      target,
      props: props as any,
    });
    return { app };
  }

  it('shows VRAM required and available before launching', async () => {
    const { app } = render({
      steps: [{ id: 'demucs-1', type: 'demucs', model: 'htdemucs_ft', enabled: true, stems: {} }],
    });

    await tick();
    await vi.waitFor(
      () => expect(target.querySelector('[data-testid="vram-launch-info"]')?.textContent).toContain('Cabe'),
      { timeout: 2000 },
    );

    const text = target.querySelector('[data-testid="vram-launch-info"]')?.textContent ?? '';
    expect(text).toContain('estimado');
    expect(text).toContain('libres');

    unmount(app);
  });

  it('warns when VRAM is tight', async () => {
    const { getGpuInfo } = await import('./api');
    vi.mocked(getGpuInfo).mockResolvedValueOnce({ ...gpuInfo, vram_free_mb: 2500 });

    const { app } = render({
      steps: [
        { id: 'demucs-1', type: 'demucs', model: 'htdemucs_ft', enabled: true, stems: {} },
        { id: 'demucs-2', type: 'demucs', model: 'htdemucs_ft', enabled: true, stems: {} },
      ],
    });

    await tick();
    await vi.waitFor(
      () => expect(target.querySelector('[data-testid="vram-launch-info"]')?.textContent).toContain('justo'),
      { timeout: 2000 },
    );

    unmount(app);
  });
});
