import { describe, it, expect } from 'vitest';
import { expandExportName, groupBaseSong, groupPitch } from './exportName';

describe('expandExportName', () => {
  const tpl = '{song} ({pitches}) ({suffix})';

  it('expands the default template normally', () => {
    expect(expandExportName(tpl, 'cancion', 5, '6stems', 'flac')).toBe('cancion (+5) (6stems)');
    expect(expandExportName(tpl, 'cancion', -2, '', 'flac')).toBe('cancion (-2)');
  });

  it('does not duplicate the pitch segment if the song already contains it', () => {
    const song = 'cancion (+5)';
    expect(expandExportName(tpl, song, 5, '', 'flac')).toBe('cancion (+5)');
  });

  it('does not duplicate the suffix segment if the song already contains it', () => {
    const song = 'cancion (6stems)';
    expect(expandExportName(tpl, song, 0, '6stems', 'flac')).toBe('cancion (6stems) (0)');
  });

  it('does not duplicate pitch or suffix for a song that already has both', () => {
    const song = 'cancion (+5) (6stems)';
    expect(expandExportName(tpl, song, 5, '6stems', 'flac')).toBe('cancion (+5) (6stems)');
  });

  it('keeps a non-duplicate suffix distinct from an existing pitch segment', () => {
    const song = 'cancion (+5)';
    expect(expandExportName(tpl, song, 5, 'master', 'flac')).toBe('cancion (+5) (master)');
  });

  it('strips empty parentheses left by an empty suffix', () => {
    expect(expandExportName(tpl, 'cancion', 0, '', 'flac')).toBe('cancion (0)');
  });
});

describe('group parsing', () => {
  it('extracts the base song and pitch from a pitch group', () => {
    expect(groupBaseSong('cancion (pitch +3)')).toBe('cancion');
    expect(groupPitch('cancion (pitch +3)')).toBe(3);
    expect(groupBaseSong('cancion (pitch -2)')).toBe('cancion');
    expect(groupPitch('cancion (pitch -2)')).toBe(-2);
  });

  it('returns the group unchanged when there is no pitch marker', () => {
    expect(groupBaseSong('cancion (+5) (6stems)')).toBe('cancion (+5) (6stems)');
    expect(groupPitch('cancion (+5) (6stems)')).toBe(0);
  });
});
