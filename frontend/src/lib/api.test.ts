import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { buildVRAMCalculatorParams, getVRAMCalculator } from './api';

describe('buildVRAMCalculatorParams', () => {
  it('sends num_overlap as the count, not as a fraction', () => {
    const params = buildVRAMCalculatorParams('BS_Roformer_Viperx', {
      segment_size: 2048,
      num_overlap: 8,
      chunk_size: 0,
      batch_size: 2,
      device: 'cuda',
    });

    expect(params.models).toBe('BS_Roformer_Viperx');
    expect(params.segment_size).toBe(2048);
    expect(params.overlap).toBe(8);
    expect(params.chunk_size).toBe(0);
    expect(params.batch_size).toBe(2);
  });

  it('includes chunk_size=0 for BS_Roformer_SW_6stem (Adri config)', () => {
    const params = buildVRAMCalculatorParams('BS_Roformer_SW_6stem', {
      segment_size: 1101,
      num_overlap: 2,
      chunk_size: 0,
      batch_size: 1,
      device: 'cuda',
    });

    expect(params.segment_size).toBe(1101);
    expect(params.overlap).toBe(2);
    expect(params.chunk_size).toBe(0);
    expect(params.batch_size).toBe(1);
  });
});

describe('getVRAMCalculator URL', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        models: [],
        total_vram_mb: 0,
        available_vram_mb: 16000,
        free_after_mb: 16000,
        fits: true,
        reliable: true,
      }),
    } as Response);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function getCalledUrl(): string {
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    return String(fetchSpy.mock.calls[0][0]);
  }

  it('sends num_overlap count and chunk_size=0 for BS_Roformer_Viperx', async () => {
    const params = buildVRAMCalculatorParams('BS_Roformer_Viperx', {
      segment_size: 2048,
      num_overlap: 8,
      chunk_size: 0,
      batch_size: 2,
      device: 'cuda',
    });

    await getVRAMCalculator(params);

    const url = getCalledUrl();
    expect(url).toContain('num_overlap=8');
    expect(url).toContain('chunk_size=0');
    expect(url).toContain('segment_size=2048');
    expect(url).toContain('batch_size=2');
    expect(url).not.toContain('overlap=0.125');
    expect(url).not.toMatch(/[?&]overlap=/);
  });

  it('sends num_overlap count and chunk_size=0 for BS_Roformer_SW_6stem', async () => {
    const params = buildVRAMCalculatorParams('BS_Roformer_SW_6stem', {
      segment_size: 1101,
      num_overlap: 2,
      chunk_size: 0,
      batch_size: 1,
      device: 'cuda',
    });

    await getVRAMCalculator(params);

    const url = getCalledUrl();
    expect(url).toContain('num_overlap=2');
    expect(url).toContain('chunk_size=0');
    expect(url).toContain('segment_size=1101');
    expect(url).toContain('batch_size=1');
    expect(url).not.toContain('overlap=0.5');
    expect(url).not.toMatch(/[?&]overlap=/);
  });
});
