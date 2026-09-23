import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount } from 'svelte';
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

  function render(props: Partial<Record<keyof PipelineView['$$prop_def'], unknown>> = {}) {
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

  it('renders per-step progress bars when the active job exposes steps', () => {
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
      currentProgress: 0.8,
    });

    const rows = target.querySelectorAll('[data-testid="step-row"]');
    expect(rows.length).toBe(2);

    const pcts = Array.from(target.querySelectorAll('.step-pct')).map((el) => el.textContent);
    expect(pcts).toContain('100%');
    expect(pcts).toContain('60%');

    const globalPct = target.querySelector('.progress-pct')?.textContent;
    expect(globalPct).toBe('80%');

    unmount(app);
  });

  it('keeps the legacy global bar when the active job has no steps', () => {
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
        progress: 55,
        current_step: 1,
        total_steps: 1,
        step_name: 'Separando',
        device: 'cuda',
      },
    ];

    const { app } = render({
      queueFiles,
      queueJobs,
      separating: true,
      hidePresetSelector: true,
      currentProgress: 0.55,
    });

    expect(target.querySelector('[data-testid="steps-list"]')).toBeNull();
    expect(target.querySelector('.progress-pct')?.textContent).toBe('55%');

    unmount(app);
  });
});
