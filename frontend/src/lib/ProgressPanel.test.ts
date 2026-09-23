import { describe, it, expect, beforeEach } from 'vitest';
import { mount, unmount } from 'svelte';
import ProgressPanel from './ProgressPanel.svelte';
import type { QueueJobStep } from './api';

describe('ProgressPanel', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);
  });

  function render(props: Partial<Record<keyof ProgressPanel['$$prop_def'], unknown>> = {}) {
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

  it('renders one bar per step when steps are provided', () => {
    const steps: QueueJobStep[] = [
      { id: 's1', name: 'Paso uno', status: 'done', progress: 100, eta: 0, elapsed: 10 },
      { id: 's2', name: 'Paso dos', status: 'running', progress: 45, eta: 120, elapsed: 30 },
      { id: 's3', name: 'Paso tres', status: 'queued', progress: 0, eta: 0, elapsed: 0 },
    ];

    const { app } = render({ steps });

    const rows = target.querySelectorAll('[data-testid="step-row"]');
    expect(rows.length).toBe(3);

    const fills = target.querySelectorAll('.step-bar-fill');
    expect(fills.length).toBe(3);

    const pcts = Array.from(target.querySelectorAll('.step-pct')).map((el) => el.textContent);
    expect(pcts).toEqual(['100%', '45%', '0%']);

    const names = Array.from(target.querySelectorAll('.step-name')).map((el) => el.textContent);
    expect(names).toEqual(['Paso uno', 'Paso dos', 'Paso tres']);

    const badges = Array.from(target.querySelectorAll('.badge'));
    expect(badges.map((el) => el.textContent)).toEqual(['Terminado', 'En curso', 'Pendiente']);

    unmount(app);
  });

  it('falls back to the legacy single global bar when no steps are provided', () => {
    const { app } = render({ progress: 0.65 });

    expect(target.querySelector('[data-testid="steps-list"]')).toBeNull();
    expect(target.querySelectorAll('[data-testid="step-row"]').length).toBe(0);

    const globalFill = target.querySelector('.progress-bar-fill') as HTMLDivElement;
    expect(globalFill).not.toBeNull();
    expect(globalFill.style.width).toBe('65%');

    expect(target.querySelector('.progress-pct')?.textContent).toBe('65%');

    unmount(app);
  });

  it('never shows a 100% global percentage while the job is running', () => {
    const steps: QueueJobStep[] = [
      { id: 's1', name: 'Paso uno', status: 'done', progress: 100 },
      { id: 's2', name: 'Paso dos', status: 'running', progress: 99 },
    ];

    const { app } = render({ progress: 0.99, status: 'running', steps });

    const globalPct = target.querySelector('.progress-pct')?.textContent;
    expect(globalPct).toBe('99%');
    expect(globalPct).not.toBe('100%');

    unmount(app);
  });

  it('highlights the running step and keeps pending steps grey', () => {
    const steps: QueueJobStep[] = [
      { id: 's1', name: 'Paso uno', status: 'done', progress: 100 },
      { id: 's2', name: 'Paso dos', status: 'running', progress: 45 },
      { id: 's3', name: 'Paso tres', status: 'queued', progress: 0 },
    ];

    const { app } = render({ steps });

    const running = target.querySelector('.step-running');
    expect(running).not.toBeNull();
    expect(running?.querySelector('.step-name')?.textContent).toBe('Paso dos');

    const pendingFill = target.querySelector('.step-bar-fill.pending') as HTMLDivElement;
    expect(pendingFill).not.toBeNull();
    expect(pendingFill.style.width).toBe('0%');

    unmount(app);
  });

  it('renders step ETA when the backend provides it', () => {
    const steps: QueueJobStep[] = [
      { id: 's1', name: 'Paso uno', status: 'running', progress: 45, eta: 125 },
    ];

    const { app } = render({ steps });

    const eta = target.querySelector('.step-eta');
    expect(eta).not.toBeNull();
    expect(eta?.textContent).toContain('≈ 2 min');

    unmount(app);
  });
});
