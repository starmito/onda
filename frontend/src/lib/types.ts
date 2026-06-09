export interface ResultStem {
  name: string;
  path: string;
  song: string;
  stemType: string;
}

export interface ResultGroup {
  song: string;
  stems: ResultStem[];
}

export function detectStemType(name: string): string {
  const n = name.toLowerCase();
  if (n.includes('drum')) return 'drums';
  if (n.includes('bass')) return 'bass';
  if (n.includes('no_vocals') || n.includes('other')) return 'other';
  if (n.includes('vocal')) return 'vocals';
  if (n.includes('instrumental')) return 'instrumental';
  return 'other';
}

const EMOJIS: Record<string, string> = { drums: '🥁', bass: '🎸', other: '🎹', vocals: '🎤', instrumental: '🎵' };
export function stemEmoji(type: string): string { return EMOJIS[type] || '🎵'; }
