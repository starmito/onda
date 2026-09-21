import type { StemsResponse } from './api';

/**
 * Devuelve una copia de StemsResponse sin el grupo indicado.
 * Útil para actualizar la UI inmediatamente tras borrar un grupo.
 */
export function removeGroup(stems: StemsResponse | null, song: string): StemsResponse | null {
  if (!stems || !stems.output || !(song in stems.output)) return stems;
  const next = { ...stems, output: { ...stems.output } };
  delete next.output[song];
  return next;
}
