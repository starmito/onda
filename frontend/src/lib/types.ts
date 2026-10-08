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
 * Mapa de instrumentos a nombres de icono de Lucide.  Nunca se usa un icono
 * genérico para un instrumento real: cada stem tiene (o se le asigna) su
 * propio icono.
 */
const STEM_ICONS: Record<string, string> = {
  drums: 'drum',
  bass: 'clef-bass',
  guitar: 'guitar',
  piano: 'piano',
  other: 'sliders-horizontal',
  vocals: 'mic-vocal',
  instrumental: 'music',
  // Instrumentos extra que pueden aparecer en modelos multi-stem.
  keyboard: 'piano',
  synth: 'piano',
  strings: 'clef-treble',
  violin: 'violin',
  cello: 'cello',
  brass: 'trumpet',
  trumpet: 'trumpet',
  saxophone: 'saxophone',
  sax: 'saxophone',
  flute: 'music',
  woodwinds: 'music',
  organ: 'piano',
  accordion: 'music',
  harmonica: 'music',
  choir: 'users',
  voice: 'mic-vocal',
  lead: 'mic',
  backing: 'mic',
  // Fallbacks de nombres alternativos.
  no_vocals: 'music',
  accompaniment: 'music',
  music: 'music',
};

function instrumentIcon(name: string): string {
  const key = name.toLowerCase();
  if (STEM_ICONS[key]) return STEM_ICONS[key];
  // Subcadena conocida dentro de nombres compuestos (p. ej. "lead_vocal").
  for (const [stem, icon] of Object.entries(STEM_ICONS)) {
    if (key.includes(stem)) return icon;
  }
  return 'music';
}

/**
 * Devuelve el nombre del icono propio del stem.  Si no hay uno específico,
 * devuelve el icono genérico de música.
 */
export function stemIcon(type: string): string {
  return instrumentIcon(type);
}

/**
 * Capitaliza la primera letra de un nombre de stem para mostrarlo.
 */
function capitalizeStem(name: string): string {
  if (!name) return name;
  return name[0].toUpperCase() + name.slice(1);
}

/**
 * Etiqueta visible para un stem: solo el nombre capitalizado (sin emoji).
 */
export function stemDisplayName(name: string): string {
  return capitalizeStem(name);
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
