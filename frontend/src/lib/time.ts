/**
 * Formats an ETA in seconds into a short, human-readable string.
 *
 * Only returns a value when the input is a positive number (or numeric string).
 * Zero, negative, NaN and missing values render as an empty string so the UI
 * never shows pointless "0 s" labels.
 */
export function formatEta(seconds: number | string | undefined | null): string {
  const value = typeof seconds === 'string' ? parseFloat(seconds) : seconds;
  if (value == null || Number.isNaN(value) || value <= 0) {
    return '';
  }

  if (value < 60) {
    return `≈ ${Math.round(value)} s`;
  }

  const mins = Math.round(value / 60);
  return `≈ ${mins} min`;
}
