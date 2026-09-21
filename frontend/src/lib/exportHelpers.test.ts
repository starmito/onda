import { describe, it, expect } from 'vitest';
import { removeGroup } from './exportHelpers';
import type { StemsResponse } from './api';

describe('removeGroup', () => {
  it('devuelve null si la respuesta es null', () => {
    expect(removeGroup(null, 'cancion')).toBeNull();
  });

  it('devuelve la respuesta intacta si el grupo no existe', () => {
    const stems: StemsResponse = {
      output: { 'Otra cancion': ['vocals.wav'] },
      pitch: [],
    };
    expect(removeGroup(stems, 'cancion')).toBe(stems);
  });

  it('elimina el grupo solicitado sin mutar el objeto original', () => {
    const stems: StemsResponse = {
      output: {
        'SUBE Y BAJA': ['vocals.wav', 'instrumental.wav'],
        'Otra cancion': ['drums.wav'],
      },
      pitch: [],
    };
    const next = removeGroup(stems, 'SUBE Y BAJA');
    expect(next).not.toBe(stems);
    expect(next?.output).not.toBe(stems.output);
    expect(Object.keys(next!.output)).toEqual(['Otra cancion']);
    expect(Object.keys(stems.output)).toEqual(['SUBE Y BAJA', 'Otra cancion']);
  });
});
