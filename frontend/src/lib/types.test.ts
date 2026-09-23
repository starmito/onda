import { describe, it, expect } from 'vitest';
import { detectStemType, stemEmoji, stemDisplayName } from './types';

describe('detectStemType', () => {
  it('extracts the exact stem name from a pipeline file name', () => {
    expect(detectStemType('my_song_vocals.wav', 'my_song')).toBe('vocals');
    expect(detectStemType('my_song_drums.wav', 'my_song')).toBe('drums');
    expect(detectStemType('my_song_guitar.wav', 'my_song')).toBe('guitar');
    expect(detectStemType('my_song_piano.wav', 'my_song')).toBe('piano');
  });

  it('ignores pitch-shift suffixes', () => {
    expect(detectStemType('my_song_vocals_pitch+2.wav', 'my_song')).toBe('vocals');
    expect(detectStemType('my_song_bass_pitch-3.wav', 'my_song')).toBe('bass');
  });

  it('returns the base name when no song prefix is provided', () => {
    expect(detectStemType('instrumental.wav')).toBe('instrumental');
  });

  it('does not collapse real instruments to "other"', () => {
    expect(detectStemType('my_song_guitar.wav', 'my_song')).not.toBe('other');
    expect(detectStemType('my_song_piano.wav', 'my_song')).not.toBe('other');
  });
});

describe('stemEmoji', () => {
  it('returns a distinct emoji for each canonical stem', () => {
    const icons = [
      stemEmoji('drums'),
      stemEmoji('bass'),
      stemEmoji('guitar'),
      stemEmoji('piano'),
      stemEmoji('other'),
      stemEmoji('vocals'),
      stemEmoji('instrumental'),
    ];
    expect(new Set(icons).size).toBe(icons.length);
  });

  it('returns an instrument emoji for known real instruments', () => {
    expect(stemEmoji('guitar')).toBe('🎸');
    expect(stemEmoji('piano')).toBe('🎹');
    expect(stemEmoji('drums')).toBe('🥁');
  });
});

describe('stemDisplayName', () => {
  it('combines emoji and capitalized stem name', () => {
    expect(stemDisplayName('vocals')).toBe('🎤 Vocals');
    expect(stemDisplayName('drums')).toBe('🥁 Drums');
    expect(stemDisplayName('guitar')).toBe('🎸 Guitar');
  });
});
