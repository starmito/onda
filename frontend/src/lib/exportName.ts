/**
 * Helpers for building export file names from the user-configurable template.
 *
 * The template expansion is idempotent: if the base song name already
 * contains the pitch or suffix segment that the template would add, that
 * segment is not duplicated. This prevents names like
 * `song (+5) ((6stems)).flac` when re-exporting an existing mixdown.
 */

export function signedPitch(pitch: number): string {
  return pitch > 0 ? `+${pitch}` : String(pitch);
}

export function groupBaseSong(group: string): string {
  return group.replace(/\s*\(pitch\s*[+-]?\d+\)\s*$/, '').trim();
}

export function groupPitch(group: string): number {
  const match = group.match(/\(pitch\s*([+-]?\d+)\)\s*$/);
  return match ? Number(match[1]) : 0;
}

export function groupDisplayName(group: string): string {
  return `${groupBaseSong(group)} (${signedPitch(groupPitch(group))})`;
}

/**
 * Expand a name template without duplicating pitch or suffix segments that
 * are already present in the song name.
 */
export function expandExportName(
  tpl: string,
  song: string,
  pitch: number,
  suffix: string,
  format: string,
): string {
  const signed = signedPitch(pitch);
  const pitchSegment = `(${signed})`;
  const suffixSegment = suffix ? `(${suffix})` : '';

  let working = tpl;

  // Idempotency: skip a pitch block if the song already contains it.
  if (working.includes('{pitches}') && pitchSegment !== '()' && song.includes(pitchSegment)) {
    working = working.replace(/\(\s*\{pitches\}\s*\)/g, '').replace(/\{pitches\}/g, '');
  }

  // Idempotency: skip a suffix block if the song already contains it.
  if (suffix && working.includes('{suffix}') && song.includes(suffixSegment)) {
    working = working.replace(/\(\s*\{suffix\}\s*\)/g, '').replace(/\{suffix\}/g, '');
  }

  const now = new Date();
  const replacements: Record<string, string> = {
    '{song}': song,
    '{pitches}': signed,
    '{suffix}': suffix,
    '{format}': format,
    '{date}': now.toISOString().slice(0, 10),
    '{time}': now.toTimeString().slice(0, 5).replace(':', '-'),
  };

  let out = working;
  for (const [key, value] of Object.entries(replacements)) {
    out = out.split(key).join(value);
  }

  // Clean up artefacts from skipped blocks.
  out = out.replace(/\(\s*\)/g, '');
  out = out.replace(/\s{2,}/g, ' ');
  return out.trim();
}
