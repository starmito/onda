/**
 * Validation helpers for the pipeline execute flow.
 *
 * Keeping this logic in a plain TS module makes it unit-testable without
 * needing to mount Svelte components.
 */

export interface PresetOption {
  name: string;
}

export type ExecuteValidation =
  | { ok: true }
  | { ok: false; message: string };

/**
 * Validates that a preset is selected and exists among the saved presets.
 *
 * The backend accepts an empty preset string, but the "Personalizado" tab is
 * designed around the preset selector, so the UI treats "-- Sin preset --" as
 * an invalid execute target and surfaces an actionable message instead of
 * silently disabling the button.
 */
export function validateExecutePreset(
  presetName: string,
  savedPresets: PresetOption[],
): ExecuteValidation {
  if (!presetName) {
    return {
      ok: false,
      message: 'Selecciona un preset antes de ejecutar la pipeline.',
    };
  }

  const selected = savedPresets.find((p) => p.name === presetName);
  if (!selected) {
    return {
      ok: false,
      message: `Preset "${presetName}" no encontrado en el servidor.`,
    };
  }

  return { ok: true };
}
