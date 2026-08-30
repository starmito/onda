/**
 * Essentia loader with CDN fallback.
 *
 * The project pins essentia.js@0.1.3 (the latest published version at the time
 * of writing). If the npm bundle cannot be imported (e.g. broken CJS/ESM
 * resolution, missing WASM), we fall back to loading the official UMD/web
 * builds from unpkg.
 */

const ESSENTIA_VERSION = '0.1.3';
const CDN_BASE = `https://unpkg.com/essentia.js@${ESSENTIA_VERSION}/dist`;

export interface EssentiaModule {
  Essentia: any;
  EssentiaWASM: any;
}

let cached: EssentiaModule | null = null;

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (typeof document === 'undefined') {
      reject(new Error('Cannot load scripts outside a browser environment'));
      return;
    }
    const script = document.createElement('script');
    script.src = src;
    script.crossOrigin = 'anonymous';
    script.async = true;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`Failed to load ${src}`));
    document.head.appendChild(script);
  });
}

async function loadFromBundle(): Promise<EssentiaModule | null> {
  const mod: any = await import('essentia.js');
  const Essentia = mod?.Essentia ?? mod?.default?.Essentia;
  const EssentiaWASM = mod?.EssentiaWASM ?? mod?.default?.EssentiaWASM;
  if (Essentia && EssentiaWASM) {
    return { Essentia, EssentiaWASM };
  }
  return null;
}

async function loadFromCDN(): Promise<EssentiaModule | null> {
  if (typeof window === 'undefined') return null;

  // Load the WASM backend first, then the core API. The web build exposes
  // window.EssentiaWASM; the core UMD build exposes window.Essentia.
  await loadScript(`${CDN_BASE}/essentia-wasm.web.js`);
  await loadScript(`${CDN_BASE}/essentia.js-core.umd.js`);

  const Essentia = (window as any).Essentia;
  const EssentiaWASM = (window as any).EssentiaWASM;
  if (Essentia && EssentiaWASM) {
    return { Essentia, EssentiaWASM };
  }
  return null;
}

export async function loadEssentia(): Promise<EssentiaModule> {
  if (cached) return cached;

  try {
    const fromBundle = await loadFromBundle();
    if (fromBundle) {
      cached = fromBundle;
      return cached;
    }
  } catch (err) {
    console.warn('essentia.js bundle load failed, trying CDN fallback:', err);
  }

  const fromCDN = await loadFromCDN();
  if (fromCDN) {
    cached = fromCDN;
    return cached;
  }

  throw new Error(
    'No se pudo cargar essentia.js ni desde el bundle local ni desde el CDN. ' +
      `Versión esperada: ${ESSENTIA_VERSION}.`,
  );
}
