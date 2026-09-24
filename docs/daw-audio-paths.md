# Rutas de audio en el DAW y la cola

## Estado actual

Onda tiene una única raíz de datos (`ONDA_DATA_DIR`, por defecto `/app/data` en el
contenedor). Todos los archivos de usuario —incluidos los de audio— viven bajo
ella:

- `input/` — subidas planas de la pestaña principal.
- `daw-data/{cancion}/{original,imports,edits,tmp}/` — proyectos del DAW.
- `output/{cancion}/` — resultados de separación.
- `input_rubberband/` — temporales de pitch-shift.

Las rutas fijas `/input`, `/app/input`, `/output`, `/app/output`, `/daw-data` y
`/app/daw-data` son **drift obsoleto** (ver `AGENTS.md`).

## Dónde se guardan las referencias

- **DAW (efectos, trim, fade, export, EQ):** el frontend mantiene los tracks en
  memoria (`DAWPage.svelte`). Cada track guarda `fileName`, que es una referencia
  relativa a la raíz de datos, por ejemplo `daw-data/{cancion}/original.wav` o
  `daw-data/{cancion}/edits/eq_original.wav`. No hay persistencia de tracks en
  el servidor; la referencia vive en el navegador del usuario.
- **Cola de separación:** `PipelineView.svelte` guarda en cada `QueueFile` el
  campo `path` que devuelve `POST /api/upload`. Hasta la v3.5.6 ese campo era
  una **ruta absoluta** (`/app/data/input/cancion.wav`), lo que hacía que una
  migración de bind-mounts o un cambio de `ONDA_DATA_DIR` rompiera los trabajos
  encolados.
- **Subidas/BPM:** `BpmPage.svelte` extrae siempre el nombre base de la ruta
  antes de llamar a los endpoints, por lo que no conserva rutas absolutas.

## Compatibilidad hacia atrás

Para no perder proyectos o trabajos que contengan referencias antiguas, el
backend normaliza las rutas en dos puntos:

1. **`backend/internal/api/daw_audio.go::normalizeLegacyDAWPath`**
   - Convierte rutas absolutas legacy a referencias relativas bajo la raíz de
     datos actual:
     - `/app/data/input/cancion.wav` → `input/cancion.wav`
     - `/app/input/cancion.wav` → `input/cancion.wav`
     - `/input/cancion.wav` → `input/cancion.wav`
     - `/app/data/daw-data/cancion/original.wav` → `daw-data/cancion/original.wav`
     - `/app/daw-data/...` y `/daw-data/...` → `daw-data/...`
   - Rutas ya relativas se dejan intactas.
   - Rutas absolutas arbitrarias de host (`/home/usuario/...`) se preservan.

2. **`backend/internal/api/server.go::normalizeContainerInput`**
   - Usa la misma normalización para los inputs de la cola de separación.
   - Preserva rutas absolutas explícitas que no sean legacy de Onda.

## Mensajes de error honestos

Cuando un endpoint del DAW no puede localizar el audio, la respuesta 404 incluye
un `help` claro:

- Referencia normal sin ruta original:  
  `El archivo de audio no está disponible. Vuelve a subirlo para continuar.`
- Referencia con ruta original conocida:  
  `El audio de este proyecto ya no está: <ruta antigua>. Vuelve a subirlo o importarlo para continuar.`

El frontend (`DAWAudioNotFoundError`) muestra directamente ese mensaje.

## Tests

- `backend/internal/api/daw_audio_legacy_test.go` — verifica que
  `resolveDAWAudioSource` resuelve referencias legacy cuando el fichero existe
  en la ubicación actual.
- `backend/internal/api/normalize_container_input_test.go` — verifica que
  `normalizeContainerInput` convierte rutas legacy en rutas válidas bajo la raíz
  de datos actual.
- `backend/internal/api/daw_test.go` — incluye casos de importación con ruta
  legacy y mensaje de error 404 con la ruta original.

## Convención para el futuro

- El backend siempre devuelve rutas **relativas a `dataRoot()`** para audio de
  usuario (`input/`, `daw-data/`, `output/`).
- El frontend debe almacenar y enviar esas rutas relativas, nunca construir
  rutas absolutas a mano.
- Cualquier nueva API que reciba una ruta de audio debe pasarla por
  `normalizeLegacyDAWPath` o `normalizeContainerInput`, según corresponda.
