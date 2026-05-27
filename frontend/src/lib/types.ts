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

export function groupBySong(stems: ResultStem[]): ResultGroup[] {
  const map = new Map<string, ResultStem[]>();
  for (const s of stems) {
    const list = map.get(s.song) || [];
    list.push(s);
    map.set(s.song, list);
  }
  return Array.from(map.entries()).map(([song, stems]) => ({ song, stems }));
}

const EMOJIS: Record<string, string> = { drums: '🥁', bass: '🎸', other: '🎹', vocals: '🎤', instrumental: '🎵' };
export function stemEmoji(type: string): string { return EMOJIS[type] || '🎵'; }
