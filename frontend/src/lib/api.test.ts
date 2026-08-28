import { describe, it, expect, vi, afterEach } from 'vitest';
import { detectBpm } from './api';

describe('detectBpm', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('devuelve el BPM desde /api/audio/tempo', async () => {
    (globalThis as any).fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({ bpm: 128.5, beats: [0, 0.46875], duration: 10 }),
    });

    const result = await detectBpm('beat.wav');

    expect(result.bpm).toBe(128.5);
    expect(result.duration).toBe(10);
    expect(fetch).toHaveBeenCalledWith('/api/audio/tempo?file=beat.wav');
  });

  it('lanza un error si el endpoint falla', async () => {
    (globalThis as any).fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      json: async () => ({}),
    });

    await expect(detectBpm('missing.wav')).rejects.toThrow('BPM detection failed');
  });
});
