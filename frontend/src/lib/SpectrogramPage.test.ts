import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { mount, unmount, type ComponentProps } from 'svelte';
import { tick } from 'svelte';
import SpectrogramPage from './SpectrogramPage.svelte';

describe('SpectrogramPage', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);

    // jsdom does not implement DataTransfer/DragEvent, so provide minimal mocks.
    if (typeof DataTransfer === 'undefined') {
      (globalThis as any).DataTransfer = class MockDataTransfer {
        items: { add: (file: File) => void }[] = [];
        files: File[] = [];
        types: string[] = [];
        add(file: File) {
          this.files.push(file);
        }
      };
    }
    if (typeof DragEvent === 'undefined') {
      (globalThis as any).DragEvent = class MockDragEvent extends Event {
        dataTransfer: any;
        constructor(type: string, init: any) {
          super(type, init);
          this.dataTransfer = init?.dataTransfer ?? null;
        }
      };
    }
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function render(props: Partial<ComponentProps<typeof SpectrogramPage>> = {}) {
    const app = mount(SpectrogramPage, {
      target,
      props: props as any,
    });
    return { app };
  }

  it('clicking the empty state triggers the hidden file input', async () => {
    const { app } = render();
    await tick();

    const input = target.querySelector('input[type="file"]') as HTMLInputElement;
    expect(input).not.toBeNull();

    const clickSpy = vi.spyOn(input, 'click');

    const emptyState = target.querySelector('.empty-state') as HTMLElement;
    expect(emptyState).not.toBeNull();
    emptyState.click();
    await tick();

    expect(clickSpy).toHaveBeenCalledTimes(1);

    unmount(app);
  });

  it('dropping an audio file loads it into the page', async () => {
    const { app } = render();
    await tick();

    const file = new File(['audio'], 'test.wav', { type: 'audio/wav' });
    const dt = new DataTransfer() as any;
    dt.add(file);
    const dropEvent = new DragEvent('drop', {
      bubbles: true,
      cancelable: true,
      dataTransfer: dt,
    });

    const page = target.querySelector('.spectrogram-page') as HTMLElement;
    expect(page).not.toBeNull();
    page.dispatchEvent(dropEvent);
    await tick();

    // After a successful drop the empty state should disappear because audioSrc is set.
    expect(target.querySelector('.empty-state')).toBeNull();
    expect(target.querySelector('.player-card')).not.toBeNull();

    unmount(app);
  });

  it('selecting a file via the input loads it into the page', async () => {
    const { app } = render();
    await tick();

    const input = target.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['audio'], 'test.wav', { type: 'audio/wav' });
    // jsdom does not allow assigning a synthetic FileList to input.files;
    // mock the property directly.
    Object.defineProperty(input, 'files', {
      value: [file] as unknown as FileList,
      configurable: true,
    });
    input.dispatchEvent(new Event('change', { bubbles: true }));
    await tick();

    expect(target.querySelector('.empty-state')).toBeNull();
    expect(target.querySelector('.player-card')).not.toBeNull();

    unmount(app);
  });
});
