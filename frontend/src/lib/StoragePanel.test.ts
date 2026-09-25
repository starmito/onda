import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount } from 'svelte';
import StoragePanel from './StoragePanel.svelte';

describe('StoragePanel', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);

    let callCount = 0;
    globalThis.fetch = vi.fn((url: RequestInfo | URL) => {
      const path = typeof url === 'string' ? url : url.toString();
      if (path.includes('/api/storage/usage')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              folders: {
                input: { files: 1, bytes: 100 },
                input_rubberband: { files: 0, bytes: 0 },
                'daw-data': { files: 0, bytes: 0 },
                output: { files: 0, bytes: 0 },
                models: { files: 0, bytes: 0 },
                cache: { files: 0, bytes: 0 },
                exports: { files: 2, bytes: 1234 },
                logs: { files: 0, bytes: 0 },
              },
              free_bytes: 9999,
              models: { entries: 0, bytes: 0 },
              cache: { files: 0, bytes: 0 },
            }),
        } as Response);
      }
      if (path.includes('/api/storage/config')) {
        callCount++;
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              current_root: '/data/root',
              source: 'default',
              exists: true,
              writable: true,
              folders: {},
              mode: 'container',
              candidates: [],
              note: '',
              export_dir: '',
              export_source: 'default',
              export_exists: true,
              export_writable: true,
              config_dir: '',
              config_source: 'default',
              config_exists: true,
              config_writable: true,
            }),
        } as Response);
      }
      if (path.includes('/api/storage/exports/clean')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ action: 'exports', files: 2, bytes: 1234 }),
        } as Response);
      }
      return Promise.resolve({ ok: false, status: 404 } as Response);
    });

    vi.stubGlobal('confirm', vi.fn(() => true));
  });

  function render() {
    const app = mount(StoragePanel, { target, props: {} });
    return app;
  }

  it('renders the exports row with file count and size', async () => {
    const app = render();
    await new Promise((r) => setTimeout(r, 50));

    const rows = target.querySelectorAll('.storage-table tbody tr');
    let exportsRow: HTMLTableRowElement | null = null;
    rows.forEach((row) => {
      if (row.textContent?.includes('Exportaciones')) {
        exportsRow = row as HTMLTableRowElement;
      }
    });

    expect(exportsRow).not.toBeNull();
    expect(exportsRow!.textContent).toContain('2');
    expect(exportsRow!.textContent).toContain('1.21 KB');

    unmount(app);
  });

  it('shows a clean button next to the exports row', async () => {
    const app = render();
    await new Promise((r) => setTimeout(r, 50));

    const buttons = Array.from(target.querySelectorAll('.btn-inline-clean'));
    expect(buttons.length).toBe(1);
    expect(buttons[0].textContent).toContain('Vaciar');

    unmount(app);
  });

  it('calls the exports clean endpoint when the clean button is clicked', async () => {
    const app = render();
    await new Promise((r) => setTimeout(r, 50));

    const button = target.querySelector('.btn-inline-clean') as HTMLButtonElement;
    expect(button).not.toBeNull();

    button.click();
    await new Promise((r) => setTimeout(r, 50));

    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls;
    const cleanCall = calls.find((call: any[]) =>
      typeof call[0] === 'string' && call[0].includes('/api/storage/exports/clean')
    );
    expect(cleanCall).toBeTruthy();
    expect(cleanCall![1]).toMatchObject({ method: 'POST' });

    unmount(app);
  });
});
