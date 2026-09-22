import { describe, it, expect } from 'vitest';
import { formatEta } from './time';

describe('formatEta', () => {
  it('returns empty string for missing values', () => {
    expect(formatEta(undefined)).toBe('');
    expect(formatEta(null)).toBe('');
  });

  it('returns empty string for zero, negative or invalid values', () => {
    expect(formatEta(0)).toBe('');
    expect(formatEta(-10)).toBe('');
    expect(formatEta(Number.NaN)).toBe('');
    expect(formatEta('not a number')).toBe('');
  });

  it('formats seconds under a minute', () => {
    expect(formatEta(1)).toBe('≈ 1 s');
    expect(formatEta(15)).toBe('≈ 15 s');
    expect(formatEta(59)).toBe('≈ 59 s');
  });

  it('formats minutes when the value reaches a minute', () => {
    expect(formatEta(60)).toBe('≈ 1 min');
    expect(formatEta(90)).toBe('≈ 2 min');
    expect(formatEta(120)).toBe('≈ 2 min');
  });

  it('accepts numeric strings', () => {
    expect(formatEta('45')).toBe('≈ 45 s');
    expect(formatEta('150')).toBe('≈ 3 min');
  });
});
