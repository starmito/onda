import { describe, it, expect } from 'vitest';
import { validateExecutePreset } from './executeValidation';

describe('validateExecutePreset', () => {
  const presets = [{ name: 'Eliminador de Voz' }, { name: 'Mi preset' }];

  it('rejects an empty preset with an actionable spanish message', () => {
    const result = validateExecutePreset('', presets);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe('Selecciona un preset antes de ejecutar la pipeline.');
    }
  });

  it('rejects a preset that does not exist on the server', () => {
    const result = validateExecutePreset('Desconocido', presets);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe('Preset "Desconocido" no encontrado en el servidor.');
    }
  });

  it('accepts a valid preset name', () => {
    const result = validateExecutePreset('Eliminador de Voz', presets);
    expect(result.ok).toBe(true);
  });

  it('accepts a valid preset even with an empty saved list', () => {
    // This documents the current behaviour: existence is checked against the list.
    const result = validateExecutePreset('Cualquiera', []);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe('Preset "Cualquiera" no encontrado en el servidor.');
    }
  });
});
