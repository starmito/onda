import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { mount, unmount, tick } from 'svelte';
import PipelineEditor from './PipelineEditor.svelte';
import type { LocalModelsResponse, PresetData } from './api';

describe('PipelineEditor stems from model manifest', () => {
  let target: HTMLDivElement;

  beforeEach(() => {
    document.body.innerHTML = '';
    target = document.createElement('div');
    document.body.appendChild(target);
    savedPresets = {};
  });

  afterEach(() => {
    unmountAll();
    vi.restoreAllMocks();
  });

  const swSixStem = {
    name: 'BS_Roformer_SW_6stem',
    installed_name: 'BS_Roformer_SW_6stem',
    display_name: 'BS-Rofo-SW-Fixed',
    category: 'Roformer',
    type: 'bs_roformer',
    size_mb: 668,
    vram_estimate_mb: 668,
    path: 'models/VR_Models/BS_Roformer_SW_6stem/BS_Roformer_SW_6stem.ckpt',
    stems: ['bass', 'drums', 'other', 'vocals', 'guitar', 'piano'],
    num_stems: 6,
  };

  const viperx = {
    name: 'BS_Roformer_Viperx',
    installed_name: 'BS_Roformer_Viperx',
    display_name: 'BS_Roformer_Viperx',
    category: 'Roformer',
    type: 'bs_roformer',
    size_mb: 350,
    vram_estimate_mb: 350,
    path: 'models/VR_Models/BS_Roformer_Viperx/BS_Roformer_Viperx.ckpt',
    stems: ['vocals', 'instrumental'],
    num_stems: 2,
  };

  const mdx23c = {
    name: 'mdx23c_d1581',
    installed_name: 'MDX23C_D1581',
    display_name: 'MDX23C_D1581',
    category: 'MDX',
    type: 'mdx23c',
    size_mb: 341,
    vram_estimate_mb: 341,
    path: 'models/MDX_Net_Models/MDX23C_D1581/mdx23c_d1581.ckpt',
    stems: ['vocals', 'instrumental'],
    num_stems: 2,
  };

  const htdemucs = {
    name: 'htdemucs_ft',
    installed_name: 'htdemucs_ft',
    display_name: 'HTDemucs FT',
    category: 'Demucs',
    type: 'demucs',
    path: '',
    size_mb: 2800,
    vram_estimate_mb: 2800,
    stems: ['drums', 'bass', 'other', 'vocals'],
    num_stems: 4,
    manifest_missing: true,
  };

  const noManifest = {
    name: 'mystery_model',
    installed_name: 'mystery_model',
    display_name: 'Mystery Model',
    category: 'VR_Arch',
    type: '',
    size_mb: 100,
    vram_estimate_mb: 100,
    path: 'models/VR_Models/mystery_model.ckpt',
    manifest_missing: true,
  };

  let savedPresets: Record<string, PresetData> = {};

  function mockFetch() {
    return vi.fn().mockImplementation(async (url: string | URL, init?: RequestInit) => {
      const u = url.toString();
      if (u.includes('/api/models/list')) {
        return {
          ok: true,
          status: 200,
          json: async () =>
            ({
              models: [swSixStem, viperx, mdx23c, htdemucs, noManifest],
              categories: ['Roformer', 'MDX', 'Demucs', 'VR_Arch'],
            }) as LocalModelsResponse,
        } as Response;
      }
      if (u === '/api/presets' && (!init || init.method !== 'POST')) {
        return {
          ok: true,
          status: 200,
          json: async () => savedPresets,
        } as Response;
      }
      if (u === '/api/presets' && init?.method === 'POST') {
        const body = JSON.parse(init.body as string) as PresetData;
        savedPresets[body.name] = body;
        return {
          ok: true,
          status: 200,
          json: async () => ({ status: 'ok' }),
        } as Response;
      }
      throw new Error(`Unexpected fetch: ${u}`);
    });
  }

  let mountedApps: any[] = [];

  function unmountAll() {
    for (const app of mountedApps) {
      try {
        unmount(app);
      } catch {
        // ignore
      }
    }
    mountedApps = [];
  }

  function render(props: Partial<Record<keyof PipelineEditor['$$prop_def'], unknown>> = {}) {
    global.fetch = mockFetch();
    const app = mount(PipelineEditor, {
      target,
      props: {
        show: true,
        ...props,
      } as any,
    });
    mountedApps.push(app);
    return { app };
  }

  async function waitForModels() {
    await vi.waitFor(() => {
      const modelSelect = target.querySelector('.config-group:nth-child(2) select') as HTMLSelectElement | null;
      expect(modelSelect?.options.length).toBeGreaterThan(0);
    }, { timeout: 2000 });
  }

  function getStemRows() {
    return Array.from(target.querySelectorAll('.routing-row'));
  }

  function getStemNames() {
    return getStemRows().map((row) => {
      const nameEl = row.querySelector('.routing-stem-name');
      return (nameEl?.textContent ?? '').trim();
    });
  }

  function getEmptyStemMessage() {
    const empty = target.querySelector('.routing-empty');
    return (empty?.textContent ?? '').trim();
  }

  async function selectModel(value: string) {
    const modelSelect = target.querySelector('.config-group:nth-child(2) select') as HTMLSelectElement;
    modelSelect.value = value;
    modelSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await tick();
  }

  async function setStepType(value: 'vocal' | 'demucs') {
    const typeSelect = target.querySelector('.config-group:nth-child(1) select') as HTMLSelectElement;
    typeSelect.value = value;
    typeSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await tick();
  }

  async function addStep() {
    const addButton = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.includes('Añadir paso'),
    );
    addButton?.click();
    await tick();
  }

  async function setPresetName(name: string) {
    const input = target.querySelector('.section input[type="text"]') as HTMLInputElement;
    input.value = name;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await tick();
  }

  function getActionRadio(stepIndex: number, stemIndex: number, action: 'route' | 'save' | 'discard') {
    const rows = getStemRows();
    const row = rows[stemIndex];
    if (!row) return null;
    const inputs = Array.from(row.querySelectorAll('input[type="radio"]'));
    const labels = Array.from(row.querySelectorAll('.routing-radio'));
    const idx = action === 'route' ? 0 : action === 'save' ? 1 : 2;
    return inputs[idx] as HTMLInputElement | null;
  }

  async function setStemAction(stemIndex: number, action: 'route' | 'save' | 'discard') {
    const radio = getActionRadio(0, stemIndex, action);
    if (!radio) throw new Error(`No radio for stem ${stemIndex} action ${action}`);
    radio.checked = true;
    radio.dispatchEvent(new Event('change', { bubbles: true }));
    await tick();
  }

  async function savePreset() {
    const saveButton = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.includes('Guardar Preset'),
    );
    saveButton?.click();
    await tick();
  }

  async function waitForPresetOption(name: string) {
    await vi.waitFor(() => {
      const select = target.querySelector('.preset-select-row select') as HTMLSelectElement | null;
      expect(select).not.toBeNull();
      const option = Array.from(select!.options).find((o) => o.value === name);
      expect(option).toBeDefined();
    }, { timeout: 2000 });
  }

  async function loadPreset(name: string) {
    await waitForPresetOption(name);
    const select = target.querySelector('.preset-select-row select') as HTMLSelectElement;
    select.value = name;
    select.dispatchEvent(new Event('change', { bubbles: true }));
    await tick();
    const loadButton = Array.from(target.querySelectorAll('button')).find(
      (b) => b.textContent?.includes('Cargar'),
    );
    loadButton?.click();
    await tick();
  }

  it('renders the 6 stems from BS_Roformer_SW_6stem', async () => {
    render();
    await addStep();
    await waitForModels();

    await setStepType('demucs');
    await selectModel('BS_Roformer_SW_6stem');

    await vi.waitFor(() => expect(getStemRows().length).toBe(6), { timeout: 2000 });

    expect(getStemNames()).toEqual([
      '🎻 Bass',
      '🥁 Drums',
      '🎛️ Other',
      '🎤 Vocals',
      '🎸 Guitar',
      '🎹 Piano',
    ]);
  });

  it('renders 2 stems for a vocal model and 4 for htdemucs_ft', async () => {
    render();
    await addStep();
    await waitForModels();

    // Vocal model
    await selectModel('BS_Roformer_Viperx');
    await vi.waitFor(() => expect(getStemRows().length).toBe(2), { timeout: 2000 });
    expect(getStemNames()).toEqual(['🎤 Vocals', '🎵 Instrumental']);

    // Demucs model
    await setStepType('demucs');
    await selectModel('htdemucs_ft');
    await vi.waitFor(() => expect(getStemRows().length).toBe(4), { timeout: 2000 });
    expect(getStemNames()).toEqual(['🥁 Drums', '🎻 Bass', '🎛️ Other', '🎤 Vocals']);
  });

  it('shows a warning instead of inventing stems for a model without manifest', async () => {
    render();
    await addStep();
    await waitForModels();

    await setStepType('vocal');
    await selectModel('mystery_model');

    await vi.waitFor(() => {
      expect(getEmptyStemMessage()).toContain('no declara stems');
    }, { timeout: 2000 });

    expect(getStemRows().length).toBe(0);
  });

  it('saves and reloads a 6-stem preset preserving per-stem actions', async () => {
    render();
    await addStep();
    await waitForModels();

    await setStepType('demucs');
    await selectModel('BS_Roformer_SW_6stem');

    await vi.waitFor(() => expect(getStemRows().length).toBe(6), { timeout: 2000 });

    // 3 saved, 3 discarded
    await setStemAction(0, 'save');   // bass
    await setStemAction(1, 'discard'); // drums
    await setStemAction(2, 'save');   // other
    await setStemAction(3, 'discard'); // vocals
    await setStemAction(4, 'save');   // guitar
    await setStemAction(5, 'discard'); // piano

    await setPresetName('SW 6-stem mix');
    await savePreset();

    await vi.waitFor(() => {
      expect(savedPresets['SW 6-stem mix']).toBeDefined();
    }, { timeout: 2000 });

    const saved = savedPresets['SW 6-stem mix'];
    expect(saved.steps[0].model).toBe('BS_Roformer_SW_6stem');
    expect(Object.keys(saved.steps[0].stems)).toEqual([
      'bass', 'drums', 'other', 'vocals', 'guitar', 'piano',
    ]);
    expect(saved.steps[0].stems.bass.action).toBe('save');
    expect(saved.steps[0].stems.drums.action).toBe('discard');
    expect(saved.steps[0].stems.other.action).toBe('save');
    expect(saved.steps[0].stems.vocals.action).toBe('discard');
    expect(saved.steps[0].stems.guitar.action).toBe('save');
    expect(saved.steps[0].stems.piano.action).toBe('discard');

    // Reload from backend
    await loadPreset('SW 6-stem mix');

    await vi.waitFor(() => {
      expect(getStemRows().length).toBe(6);
    }, { timeout: 2000 });

    expect(getStemNames()).toEqual([
      '🎻 Bass',
      '🥁 Drums',
      '🎛️ Other',
      '🎤 Vocals',
      '🎸 Guitar',
      '🎹 Piano',
    ]);

    expect(getActionRadio(0, 0, 'save')?.checked).toBe(true);
    expect(getActionRadio(0, 1, 'discard')?.checked).toBe(true);
    expect(getActionRadio(0, 2, 'save')?.checked).toBe(true);
    expect(getActionRadio(0, 3, 'discard')?.checked).toBe(true);
    expect(getActionRadio(0, 4, 'save')?.checked).toBe(true);
    expect(getActionRadio(0, 5, 'discard')?.checked).toBe(true);
  });

  it('migrates an old 4-stem demucs preset without losing actions', async () => {
    savedPresets = {
      'Old Demucs': {
        name: 'Old Demucs',
        steps: [
          {
            id: 'demucs',
            model: 'htdemucs_ft',
            type: 'demucs',
            enabled: true,
            stems: {
              drums: { action: 'save', target: 'result' },
              bass: { action: 'save', target: 'result' },
              other: { action: 'discard' },
              vocals: { action: 'discard' },
            },
          },
        ],
        locked: false,
      },
    };

    render();

    await loadPreset('Old Demucs');

    await vi.waitFor(() => expect(getStemRows().length).toBe(4), { timeout: 2000 });

    expect(getStemNames()).toEqual(['🥁 Drums', '🎻 Bass', '🎛️ Other', '🎤 Vocals']);
    expect(getActionRadio(0, 0, 'save')?.checked).toBe(true);
    expect(getActionRadio(0, 1, 'save')?.checked).toBe(true);
    expect(getActionRadio(0, 2, 'discard')?.checked).toBe(true);
    expect(getActionRadio(0, 3, 'discard')?.checked).toBe(true);
  });
});
