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

/**
 * Mapa de instrumentos a emojis propios.  Nunca se usa un icono genérico para
 * un instrumento real: cada stem tiene (o se le asigna) su propio icono.
 */
const STEM_EMOJIS: Record<string, string> = {
  drums: '🥁',
  bass: '🎻',
  guitar: '🎸',
  piano: '🎹',
  other: '🎛️',
  vocals: '🎤',
  instrumental: '🎵',
  // Instrumentos extra que pueden aparecer en modelos multi-stem.
  keyboard: '🎹',
  synth: '🎹',
  strings: '🎻',
  violin: '🎻',
  cello: '🎻',
  brass: '🎺',
  trumpet: '🎺',
  saxophone: '🎷',
  sax: '🎷',
  flute: '🪈',
  woodwinds: '🪈',
  organ: '🎹',
  accordion: '🪗',
  harmonica: '🎶',
  choir: '👥',
  voice: '🎤',
  lead: '🎙️',
  backing: '🎙️',
  // Fallbacks de nombres alternativos.
  no_vocals: '🎵',
  accompaniment: '🎵',
  music: '🎵',
};

function instrumentEmoji(name: string): string {
  const key = name.toLowerCase();
  if (STEM_EMOJIS[key]) return STEM_EMOJIS[key];
  // Subcadena conocida dentro de nombres compuestos (p. ej. "lead_vocal").
  for (const [stem, emoji] of Object.entries(STEM_EMOJIS)) {
    if (key.includes(stem)) return emoji;
  }
  return '🎵';
}

/**
 * Devuelve el emoji propio del stem.  Si no hay uno específico, devuelve un
 * emoji de instrumento musical (nunca genérico para un instrumento real).
 */
export function stemEmoji(type: string): string {
  return instrumentEmoji(type);
}

/**
 * Capitaliza la primera letra de un nombre de stem para mostrarlo.
 */
function capitalizeStem(name: string): string {
  if (!name) return name;
  return name[0].toUpperCase() + name.slice(1);
}

/**
 * Etiqueta visible para un stem: emoji + nombre capitalizado.
 */
export function stemDisplayName(name: string): string {
  return `${stemEmoji(name)} ${capitalizeStem(name)}`;
}

/**
 * Extrae el nombre exacto del stem a partir del nombre de fichero que genera
 * el pipeline.  El pipeline emite `<canción>_<stem>.wav`; si se conoce la
 * canción se elimina ese prefijo.  También se descartan las marcas de tono
 * (`_pitch+2`) y la extensión.
 */
export function detectStemType(name: string, song?: string): string {
  let base = name;
  const ext = base.lastIndexOf('.');
  if (ext > 0) {
    base = base.slice(0, ext);
  }
  // Quitar marca de cambio de tono, si existe (p. ej. vocals_pitch+2).
  base = base.replace(/_pitch[+-]\d+$/, '');
  // Quitar prefijo de canción.
  if (song) {
    const prefix = song + '_';
    if (base.toLowerCase().startsWith(prefix.toLowerCase())) {
      base = base.slice(prefix.length);
    }
  }
  return base.toLowerCase();
}
