/**
 * Essentia loader – carga siempre desde el bundle local de npm.
 *
 * Usa los builds ESM reales de essentia.js@0.1.3:
 *   - essentia.js-core.es.js  -> clase Essentia (default export)
 *   - essentia-wasm.es.js     -> EssentiaWASM (named export)
 *
 * Se instancia una única vez como `new Essentia(EssentiaWASM)` y se cachea.
 * No quedan URLs externas ni fallback por CDN en el camino normal.
 *
 * Los imports son dinámicos para no bloquear el arranque de la app y poder
 * degradar con elegancia si el WASM no está disponible.
 */

export interface EssentiaInstance {
  arrayToVector(array: Float32Array | number[]): any;
  KeyExtractor(vector: any): { key: string; scale: string; strength: number };
}

export class EssentiaLoadError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'EssentiaLoadError';
  }
}

let cached: EssentiaInstance | null = null;
let loading: Promise<EssentiaInstance> | null = null;

/**
 * Carga e instancia Essentia desde el bundle local.
 * Devuelve la misma instancia cacheada en llamadas sucesivas.
 */
export async function loadEssentia(): Promise<EssentiaInstance> {
  if (cached) return cached;
  if (loading) return loading;

  loading = (async () => {
    try {
      const [{ default: Essentia }, { EssentiaWASM }] = await Promise.all([
        import('essentia.js/dist/essentia.js-core.es.js'),
        import('essentia.js/dist/essentia-wasm.es.js'),
      ]);
      const instance: EssentiaInstance = new Essentia(EssentiaWASM);
      cached = instance;
      return instance;
    } catch (err) {
      console.warn('No se pudo cargar essentia.js desde el bundle local:', err);
      throw new EssentiaLoadError(
        'No se pudo inicializar el análisis de audio local. ' +
          'La detección de tonalidad no estará disponible.',
      );
    } finally {
      loading = null;
    }
  })();

  return loading;
}

/**
 * Devuelve la instancia de Essentia ya cargada.
 * Lanza EssentiaLoadError si aún no se ha cargado.
 */
export function getEssentia(): EssentiaInstance {
  if (!cached) {
    throw new EssentiaLoadError(
      'Essentia no está cargado. Llama a loadEssentia() primero.',
    );
  }
  return cached;
}
