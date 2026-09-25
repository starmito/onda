import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount, type ComponentProps } from 'svelte';
import PipelineView from './PipelineView.svelte';
import type { QueueFile } from './queueDefaults';
import type { QueueJob } from './api';

describe('PipelineView', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);
  });

  function render(props: Partial<ComponentProps<typeof PipelineView>> = {}) {
    const onQueueChange = vi.fn();
    const onViewResult = vi.fn();

    const queueFiles: QueueFile[] = [
      {
        file: new File([], 'song.wav'),
        id: 'done-1',
        status: 'done',
        checked: false,
        path: 'uploads/song.wav',
      },
    ];

    const app = mount(PipelineView, {
      target,
      props: {
        queueFiles,
        savedPresets: [],
        hidePresetSelector: true,
        onQueueChange,
        onViewResult,
        ...props,
      } as any,
    });

    return { app, onQueueChange, onViewResult };
  }

  it('clicking the checkbox toggles the row but does not trigger onViewResult', () => {
    const { app, onQueueChange, onViewResult } = render();

    const checkbox = target.querySelector('input[type="checkbox"]') as HTMLInputElement;
    expect(checkbox).not.toBeNull();

    checkbox.click();

    expect(onViewResult).not.toHaveBeenCalled();
    expect(onQueueChange).toHaveBeenCalledTimes(1);

    const updated = onQueueChange.mock.calls[0][0] as QueueFile[];
    expect(updated[0].checked).toBe(true);
    expect(updated[0].userTouched).toBe(true);

    unmount(app);
  });

  it('clicking the rest of the done row triggers onViewResult', () => {
    const { app, onViewResult } = render();

    const row = target.querySelector('.queue-row') as HTMLDivElement;
    expect(row).not.toBeNull();
    expect(row.role).toBe('button');

    // Click on the progress area, well outside the checkbox, should navigate.
    const progress = target.querySelector('.queue-progress') as HTMLSpanElement;
    expect(progress).not.toBeNull();
    progress.click();

    expect(onViewResult).toHaveBeenCalledTimes(1);

    unmount(app);
  });

  it('shows a CPU badge next to a queue row that ran on CPU', () => {
    const queueFiles: QueueFile[] = [
      {
        file: new File([], 'cpu-song.wav'),
        id: 'cpu-1',
        status: 'done',
        checked: false,
        path: 'uploads/cpu-song.wav',
      },
    ];
    const queueJobs: QueueJob[] = [
      {
        song: 'cpu-song',
        status: 'done',
        progress: 100,
        device: 'cpu',
        gpu_type: 'N/A',
        ran_on_cpu: true,
      },
    ];

    const { app } = render({ queueFiles, queueJobs });

    const badge = target.querySelector('.cpu-row-badge');
    expect(badge).not.toBeNull();
    expect(badge!.textContent).toBe('CPU');

    unmount(app);
  });

  it('does not show a CPU badge for GPU jobs', () => {
    const queueFiles: QueueFile[] = [
      {
        file: new File([], 'cuda-song.wav'),
        id: 'cuda-1',
        status: 'done',
        checked: false,
        path: 'uploads/cuda-song.wav',
      },
    ];
    const queueJobs: QueueJob[] = [
      {
        song: 'cuda-song',
        status: 'done',
        progress: 100,
        device: 'cuda',
        gpu_type: 'NVIDIA GeForce RTX 5060 Ti',
        ran_on_cpu: false,
      },
    ];

    const { app } = render({ queueFiles, queueJobs });

    const badge = target.querySelector('.cpu-row-badge');
    expect(badge).toBeNull();

    unmount(app);
  });

  it('renders per-song progress bars when jobs are provided', () => {
    const queueFiles: QueueFile[] = [
      {
        file: new File([], 'song.wav'),
        id: 's1',
        status: 'processing',
        checked: false,
        path: 'uploads/song.wav',
      },
    ];
    const queueJobs: QueueJob[] = [
      {
        song: 'song',
        status: 'processing',
        progress: 50,
        current_step: 2,
        total_steps: 2,
        step_name: 'Modelo B',
        device: 'cuda',
        steps: [
          { id: 'step-a', name: 'Modelo A', status: 'done', progress: 100, eta: 0, elapsed: 30 },
          { id: 'step-b', name: 'Modelo B', status: 'running', progress: 60, eta: 90, elapsed: 20 },
        ],
      },
    ];

    const { app } = render({
      queueFiles,
      queueJobs,
      separating: true,
      hidePresetSelector: true,
      currentProgress: 0.5,
    });

    const rows = target.querySelectorAll('[data-testid="job-row"]');
    expect(rows.length).toBe(1);

    const pcts = Array.from(target.querySelectorAll('.job-pct')).map((el) => el.textContent);
    expect(pcts).toContain('50%');

    const globalPct = target.querySelector('.progress-pct')?.textContent;
    expect(globalPct).toBe('50%');

    unmount(app);
  });

  it('shows the progress panel for API-launched jobs without local queue files', () => {
    const queueFiles: QueueFile[] = [];
    const queueJobs: QueueJob[] = [
      {
        song: 'api-song',
        status: 'processing',
        progress: 40,
        current_step: 1,
        total_steps: 2,
        step_name: 'Voz',
        device: 'cuda',
        steps: [
          { id: 'vocal', name: 'Voz', status: 'running', progress: 80, eta: 10, elapsed: 20 },
          { id: 'demucs', name: 'Demucs', status: 'queued', progress: 0, eta: 0, elapsed: 0 },
        ],
      },
    ];

    const { app } = render({
      queueFiles,
      queueJobs,
      separating: true,
      pipelineStatus: 'running',
      hidePresetSelector: true,
      currentProgress: 0.4,
    });

    expect(target.querySelector('[data-testid="jobs-list"]')).not.toBeNull();
    const rows = target.querySelectorAll('[data-testid="job-row"]');
    expect(rows.length).toBe(1);

    const pcts = Array.from(target.querySelectorAll('.job-pct')).map((el) => el.textContent);
    expect(pcts).toContain('40%');

    unmount(app);
  });

  it('renders one bar per song when multiple jobs are active', () => {
    const queueFiles: QueueFile[] = [
      {
        file: new File([], 'a.wav'),
        id: 'a1',
        status: 'processing',
        checked: false,
        path: 'uploads/a.wav',
      },
      {
        file: new File([], 'b.wav'),
        id: 'b1',
        status: 'waiting',
        checked: false,
        path: 'uploads/b.wav',
      },
    ];
    const queueJobs: QueueJob[] = [
      { song: 'a', status: 'processing', progress: 50, current_step: 1, total_steps: 1, step_name: 'Voz', device: 'cuda' },
      { song: 'b', status: 'waiting', progress: 0 },
    ];

    const { app } = render({
      queueFiles,
      queueJobs,
      separating: true,
      hidePresetSelector: true,
      currentProgress: 0.25,
    });

    const rows = target.querySelectorAll('[data-testid="job-row"]');
    expect(rows.length).toBe(2);

    const names = Array.from(target.querySelectorAll('.job-name')).map((el) => el.textContent);
    expect(names).toEqual(['a', 'b']);

    expect(target.querySelector('.progress-count')?.textContent).toBe('0/2 canciones');

    unmount(app);
  });

  it('clicking the row remove button calls DELETE /api/queue/{song}', async () => {
    globalThis.fetch = vi.fn((url: RequestInfo | URL) => {
      const path = typeof url === 'string' ? url : url.toString();
      if (path.includes('/api/queue/song')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ status: 'removed', song: 'song' }),
        } as Response);
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) } as Response);
    });
    vi.stubGlobal('confirm', vi.fn(() => true));

    const queueFiles: QueueFile[] = [
      {
        file: new File([], 'song.wav'),
        id: 's1',
        status: 'waiting',
        checked: false,
        path: 'uploads/song.wav',
      },
    ];
    const onQueueChange = vi.fn();

    const { app } = render({ queueFiles, onQueueChange });
    await new Promise((r) => setTimeout(r, 50));

    const removeButton = target.querySelector('.btn-remove') as HTMLButtonElement;
    expect(removeButton).not.toBeNull();

    removeButton.click();
    await new Promise((r) => setTimeout(r, 50));

    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls;
    const deleteCall = calls.find((call: any[]) =>
      typeof call[0] === 'string' && call[0].includes('/api/queue/song')
    );
    expect(deleteCall).toBeTruthy();
    expect(deleteCall![1]).toMatchObject({ method: 'DELETE' });
    expect(onQueueChange).toHaveBeenCalled();

    unmount(app);
  });
});
