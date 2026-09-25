import { describe, it, expect, beforeEach } from 'vitest';
import { mount, unmount, type ComponentProps } from 'svelte';
import ProgressPanel from './ProgressPanel.svelte';
import type { QueueJob, QueueJobStep } from './api';

describe('ProgressPanel', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);
  });

  function render(props: Partial<ComponentProps<typeof ProgressPanel>> = {}) {
    const app = mount(ProgressPanel, {
      target,
      props: {
        status: 'running',
        step: 'Separando',
        song: 'cancion.wav',
        eta: '',
        device: 'cuda',
        model: '',
        flags: '',
        progress: 0.5,
        ...props,
      } as any,
    });
    return { app };
  }

  it('renders one bar per queued song and a proportional global bar', () => {
    const jobs: QueueJob[] = [
      { song: 'cancion-a', status: 'done', progress: 100 },
      { song: 'cancion-b', status: 'waiting', progress: 0 },
    ];

    const { app } = render({ jobs, progress: 0.5 });

    const global = target.querySelector('[data-testid="global-progress"]');
    expect(global).not.toBeNull();
    expect(global?.querySelector('.progress-pct')?.textContent).toBe('50%');
    expect(global?.querySelector('.progress-count')?.textContent).toBe('1/2 canciones');

    const rows = target.querySelectorAll('[data-testid="job-row"]');
    expect(rows.length).toBe(2);

    const names = Array.from(target.querySelectorAll('.job-name')).map((el) => el.textContent);
    expect(names).toEqual(['cancion-a', 'cancion-b']);

    const pcts = Array.from(target.querySelectorAll('.job-pct')).map((el) => el.textContent);
    expect(pcts).toEqual(['100%', '0%']);

    unmount(app);
  });

  it('global bar is 33% with three songs and one finished', () => {
    const jobs: QueueJob[] = [
      { song: 'uno', status: 'done', progress: 100 },
      { song: 'dos', status: 'waiting', progress: 0 },
      { song: 'tres', status: 'waiting', progress: 0 },
    ];

    const { app } = render({ jobs, progress: 1 / 3 });

    expect(target.querySelectorAll('[data-testid="job-row"]').length).toBe(3);
    expect(target.querySelector('.progress-pct')?.textContent).toBe('33%');
    expect(target.querySelector('.progress-count')?.textContent).toBe('1/3 canciones');

    unmount(app);
  });

  it('shows per-song step and ETA for the active job', () => {
    const jobs: QueueJob[] = [
      {
        song: 'cancion',
        status: 'processing',
        progress: 70,
        current_step: 2,
        total_steps: 2,
        step_name: 'Demucs',
        eta: 120,
        device: 'cuda',
      },
    ];

    const { app } = render({ jobs, progress: 0.7 });

    const step = target.querySelector('.job-step');
    expect(step).not.toBeNull();
    expect(step?.textContent).toBe('Paso 2/2 (Demucs)');

    const eta = target.querySelector('.job-eta');
    expect(eta).not.toBeNull();
    expect(eta?.textContent).toContain('≈ 2 min');

    unmount(app);
  });

  it('renders a CPU badge for jobs that ran on CPU', () => {
    const jobs: QueueJob[] = [
      { song: 'cpu-song', status: 'done', progress: 100, device: 'cpu', ran_on_cpu: true },
      { song: 'gpu-song', status: 'processing', progress: 50, device: 'cuda' },
    ];

    const { app } = render({ jobs, progress: 0.75 });

    const badges = target.querySelectorAll('.job-device');
    expect(badges.length).toBe(2);
    expect(badges[0].textContent).toBe('CPU');
    expect(badges[0].classList.contains('cpu')).toBe(true);
    expect(badges[1].textContent).toBe('GPU');

    unmount(app);
  });

  it('falls back to the legacy single global bar when no jobs are provided', () => {
    const { app } = render({ progress: 0.65 });

    expect(target.querySelector('[data-testid="global-progress"]')).toBeNull();
    expect(target.querySelector('[data-testid="jobs-list"]')).toBeNull();

    const globalFill = target.querySelector('.progress-bar-fill') as HTMLDivElement;
    expect(globalFill).not.toBeNull();
    expect(globalFill.style.width).toBe('65%');

    expect(target.querySelector('.progress-pct')?.textContent).toBe('65%');

    unmount(app);
  });

  it('falls back to per-step bars when only steps are provided', () => {
    const steps: QueueJobStep[] = [
      { id: 's1', name: 'Paso uno', status: 'done', progress: 100, eta: 0, elapsed: 10 },
      { id: 's2', name: 'Paso dos', status: 'running', progress: 45, eta: 120, elapsed: 30 },
      { id: 's3', name: 'Paso tres', status: 'queued', progress: 0, eta: 0, elapsed: 0 },
    ];

    const { app } = render({ steps });

    const rows = target.querySelectorAll('[data-testid="step-row"]');
    expect(rows.length).toBe(3);

    const pcts = Array.from(target.querySelectorAll('.step-pct')).map((el) => el.textContent);
    expect(pcts).toEqual(['100%', '45%', '0%']);

    unmount(app);
  });

  it('never shows a 100% global percentage while the job is running', () => {
    const jobs: QueueJob[] = [
      { song: 'a', status: 'done', progress: 100 },
      { song: 'b', status: 'processing', progress: 99 },
    ];

    const { app } = render({ jobs, progress: 0.995, status: 'running' });

    const globalPct = target.querySelector('.progress-pct')?.textContent;
    expect(globalPct).toBe('99%');
    expect(globalPct).not.toBe('100%');

    unmount(app);
  });
});
