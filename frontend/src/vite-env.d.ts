/// <reference types="svelte" />
/// <reference types="vite/client" />

declare module 'essentia.js/dist/essentia.js-core.es.js' {
  class Essentia {
    constructor(EssentiaWASM: any, isDebug?: boolean);
    arrayToVector(array: Float32Array | number[]): any;
    KeyExtractor(vector: any): { key: string; scale: string; strength: number };
  }
  export default Essentia;
}

declare module 'essentia.js/dist/essentia-wasm.es.js' {
  export const EssentiaWASM: any;
}
