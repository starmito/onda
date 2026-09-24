import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount } from 'svelte';
import ExportPage from './ExportPage.svelte';

describe('ExportPage filters export files from stem list', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/daw/stems')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              output: {
                cancion1: ['vocals.wav', 'merge_cancion1.flac', 'export_cancion1.wav'],
              },
              pitch: [],
            }),
        } as Response);
      }
      if (url.includes('/api/export/profiles')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              defaultFormat: 'flac',
              formats: {},
            }),
        } as Response);
      }
      return Promise.resolve({ ok: false, status: 404 } as Response);
    });
  });

  it('does not render merge_ or export_ files as stems', async () => {
    const target = document.createElement('div');
    document.body.appendChild(target);
    mount(ExportPage, { target });

    // Allow the component to load stems and profiles.
    await new Promise((r) => setTimeout(r, 100));

    const text = target.textContent || '';
    expect(text).toContain('vocals.wav');
    expect(text).not.toContain('merge_cancion1.flac');
    expect(text).not.toContain('export_cancion1.wav');
  });
});
