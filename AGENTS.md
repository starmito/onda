# AGENTS.md — Reglas de trabajo para agentes de código (OpenCode)

Reglas OBLIGATORIAS para cualquier agente que trabaje en este repositorio. Incumplirlas es un fallo grave.

## 🚫 PROHIBIDO (sin excepciones)

- **NUNCA usar `/tmp` ni `/var/tmp`**: el sandbox de seguridad lo bloquea (auto-reject) y rompe el flujo de trabajo. Cualquier archivo temporal de depuración se crea DENTRO del repo (p.ej. `debug/` o en la raíz) y se borra antes de commitear.
- **NUNCA `git add -A` ni `git add .`**: añadir SIEMPRE archivos explícitos.
- **NUNCA añadir al commit**: `config/`, `*.orig`, `frontend/dist/`, `node_modules/`, `.git/`, artefactos de build ni logs de depuración.
- **NUNCA desplegar**: `deploy.sh`, `docker compose up -d`, etc. El robot de auto-deploy se encarga. (SÍ se puede: `docker compose build` sin `up`, o verificación nativa, para comprobar que compila.)
- **NUNCA tocar `config/`** (config local con secretos) ni archivos `*.orig`.
- **NUNCA usar `sudo`** ni instalar paquetes del sistema (el sandbox lo bloquea).

## 🧪 BANCO DE PRUEBAS — la ÚNICA vía para probar de verdad (orden de Adri, 08-oct-2026)

- **Comando único:** `./prueba.sh` desde la raíz del repo. Hace: construir la imagen `onda:prueba` del
  árbol de trabajo actual (**sin crear tags de git**) → recrear SOLO el contenedor del banco de pruebas →
  verificar su health. Opciones: `--build` (solo construir), `--health` (solo comprobar), `--logs`.
- **Contenedor:** `onda-prueba`, puerto **3010**, con **GPU** (`runtime: nvidia`) y su backend CUDA propio.
  La versión que reporta lleva el sufijo `-prueba` para no confundirla con producción.
- **NUEVO PROHIBIDO:** crear tags de git para probar · contenedores temporales `onda-verif-*` ·
  `docker compose down` · editar a mano el compose del banco de pruebas.
- **GPU SIEMPRE:** el banco de pruebas corre con GPU. Nunca `--device cpu` (el paso vocal en CPU tarda
  minutos u horas y puede agotar los 7,4 GB del host).
- **Evidencia obligatoria** en el informe: el health del 3010 y el caso funcional concreto que probaste.

## ✅ SIEMPRE

- Compilar y pasar TODOS los tests antes de commitear:
  - `cd backend && go build ./... && go test ./...`
  - `pytest` desde la raíz del repo
  - `cd frontend && npm run check && npm run test && npm run build`
- Ejecutar los **guardianes del repo** antes de commitear cambios de dependencias o código de terceros:
  - `tools/check-gitignore.sh` — asegura que `.hermes/` y rutas ignoradas no están trackeadas.
  - `tools/check-deps.sh` — asegura que los requirements Python están fijados y coinciden con `requirements.lock`.
  - `tools/check-licenses.sh` — bloquea licencias prohibidas (AGPL-*, GPL-2.0, GPL-3.0, SSPL-*, BUSL-*) en dependencias de producción del frontal, dependencias Python conocidas y código vendorizado (`lib_v5/`); también verifica que `go mod tidy -diff` esté vacío. Funciona sin red si los módulos de Go están en caché; si no, avisa y sigue.
  - Los tres guardianes se ejecutan automáticamente en la suite via `tests/unit/test_guards.py`.
- Si un test necesita `aubio`/`sox` y no están instalados, usar el patrón `skipIfMissingBinary` ya existente (saltar limpiamente, no fallar).
- Declarar una dependencia exige que alguien la importe en el código de Onda (repo + `lib_v5`) o que un paquete instalado la requiera (`pip show <pkg>` → `Required-by:`). Usar siempre ambas comprobaciones; no dejar dependencias muertas en los requirements.
- Commits conventional (`feat:`, `fix:`, `test:`, `refactor:`, `docs:`, `chore:`) y **push de tu rama de trabajo** (`origin/trabajo/<tema>`). **NUNCA** cambies `VERSION` ni crees tags: eso es solo para publicar una versión, y lo decide Adri (ver la norma de abajo).
- La versión sale del fichero `VERSION` en la raíz del repo. `build.sh` y `deploy.sh` leen `VERSION` y validan que `onda/_version.py`, `pyproject.toml` y `frontend/package.json` coincidan. NUNCA hardcodear versiones a mano ni duplicar la lógica de versionado.
- Si necesitas helpers temporales de depuración: crearlos dentro del repo y borrarlos antes del commit.

## 📌 ESTRUCTURA DE TRABAJO — norma fija (acordada con Adri el 08-oct-2026)

**La idea:** el recetario (el código) se mejora hoja a hoja y **cada hoja sube a la nube** según se prueba, pero **no se
saca edición nueva (versión) hasta que Adri lo diga**. Los cocineros (producción) siguen con la edición que ya tienen.

1. **Todo lo terminado se sube a GitHub.** Al cerrar una tarea, su rama está subida y `main` está al día.
   Verificación de cierre: **0 commits sin subir**.
2. **Las ramas se llaman por lo que son**: `trabajo/<tema>` (p. ej. `trabajo/borrado-input`).
   **Prohibido** poner el número de versión en el nombre (`fix/v3.5.6` ✗) — mezcla «qué cambias» con «qué edición es».
3. **La versión NO se toca en el trabajo diario.** Nada de bumps, ni tags, ni entradas de CHANGELOG por cada cambio:
   los cambios se **acumulan**. **Publicar una versión es decisión exclusiva de Adri**; solo entonces se hace:
   subir `VERSION` (+ `onda/_version.py`, `pyproject.toml`, `frontend/package.json`), entrada en `CHANGELOG`,
   tags `onda-vX.Y.Z` + `gui-vX.Y.Z`, push y despliegue a producción (con su OK).
4. **Una versión publicada = un tag.** Nunca se crean tags para probar ni para marcar puntos intermedios.

| Quién | Sí hace | No hace |
|---|---|---|
| **OpenCode** | trabajar en su rama `trabajo/<tema>`, commits convencionales, **subir su rama**, `./prueba.sh`, informe con evidencia real | no toca `main`, ni tags, ni `VERSION`, ni producción, ni `deploy.sh` |
| **Manita** | preparar encargos, verificación independiente en el banco (:3010), contenedores por Arcane, **sincronizar GitHub** (`main` y tags) cuando Adri lo autoriza, vigilar que no haya desfase | no decide versiones ni publica |
| **Adri** | decidir **qué se publica y cuándo** · autorizar el pase a producción | — |

**Al publicar (semver):** arreglo → PATCH (v3.5.17 → v3.5.18) · mejora → MINOR (v3.5.17 → v3.6.0) · ruptura → MAJOR (v4.0.0).
En caso de duda entre X e Y, **preguntar a Adri**.

## Contexto del proyecto

- **Onda**: separador de fuentes musicales (Demucs, ViperX, MDX/SCNet/ONNX) + DAW ligero. Backend Go (`backend/`), frontend Svelte 5 (`frontend/`), pipeline Python (`onda/`).
- **Ramas**: `main` es la versión buena y **siempre está al día en GitHub**. Cada tarea trabaja en una rama **`trabajo/<tema>`** (por lo que cambias, nunca con el número de versión). No tocar `main` ni los tags.
- **Raíz de datos única**: todos los datos de usuario y configuración viven bajo la raíz configurada (`ONDA_DATA_DIR`, por defecto `/app/data` en el contenedor). El backend y `pipeline.sh` resuelven siempre rutas relativas a esa raíz. Las rutas fijas `/app/input/`, `/app/output/`, `/app/config/` y el bare `/input/` son **drift obsoleto**.
- **Variables de entorno relevantes** (orden de precedencia: env > ajuste persistido > defecto):
  - `ONDA_DATA_DIR` — raíz de datos (contiene `input/`, `output/`, `daw-data/`, `input_rubberband/`, `config/`, `logs/`, `models/`).
  - `ONDA_SETTINGS_FILE` — fichero persistente de ajustes (`/app/data/config/.onda-settings.json` por defecto). *Nota:* aunque contiene `data_root`, `config_dir` y `export_dir`, el fichero en sí vive fuera de la raíz de datos para sobrevivir a cambios de raíz.
  - `ONDA_CONFIG_DIR` — carpeta de configuración (debe estar dentro de `ONDA_DATA_DIR` en el contenedor).
  - `ONDA_EXPORT_DIR` — carpeta destino de exportaciones del DAW.
  - `ONDA_APP_DIR` — directorio de la aplicación (`/app` en contenedor, raíz del repo en nativo).
  - `ONDA_PORT` — puerto expuesto por docker-compose (default 3000).
  - `ONDA_ALLOW_UNTAGGED=1` — permite builds de desarrollo sin tag de release.
- **Cachés persistentes dentro de la raíz de datos** (sobreviven a recreaciones del contenedor):
  - `HF_HOME` → `${ONDA_DATA_DIR}/.cache/huggingface`
  - `TORCH_HOME` → `${ONDA_DATA_DIR}/.cache/torch`
  - `NUMBA_CACHE_DIR` → `${ONDA_DATA_DIR}/.cache/numba`
  - `XDG_CACHE_HOME` → `${ONDA_DATA_DIR}/.cache/xdg`
- **Regla de las flags de modelo**: `shifts`, `segment`, `batch`, `jobs` y `device` se gestionan **solo** en **Ajustes → Modelos**. Los presets no mandan flags; la petición de separación ya no puede sobreescribirlas. El rango declarado de cada slider siempre puede representar el valor guardado (por ejemplo, `shifts` de Demucs usa el rango real del manifiesto).
- **VRAM real**: el guard de VRAM lee la memoria del dispositivo vía `nvidia-smi`. Si no puede leer la VRAM disponible, Onda **no lanza** el trabajo (estado `blocked_no_gpu`). El usuario puede forzar con `force_vram`, pero el bloqueo por defecto es seguro.
- `onda-gui/` fue eliminado en v3.2.0: no existe ya como directorio ni como servicio.

## Cómo medimos

Para no engañarnos con mejoras o regresiones, Onda usa un protocolo **baseline → un cambio aislado → medir otra vez**. Nunca se miden dos cambios en la misma corrida.

### Herramientas

- `tools/bench-baseline.sh` — mide la versión desplegada de Onda hablando con su API:
  - Genera un clip de prueba fijo (`.hermes/bench/fixtures/`) si no existe.
  - Lee el preset y las flags efectivas de **Ajustes → Modelos** por API y las guarda en el JSON.
  - Lanza un trabajo y muestrea `GET /api/queue/status` y `GET /api/processes/status` con intervalo configurable.
  - Mide duración total, duración por paso, VRAM pico/media vía `nvidia-smi`, dispositivo usado, número/nombre de stems y energía/crest de cada stem.
  - Guarda versiones: Onda (backend/pipeline/frontend), torch, torchvision, onnxruntime y demucs.
  - Escribe `.hermes/bench/<fecha>-<etiqueta>.json` e imprime un resumen.

  Uso típico:
  ```bash
  bash tools/bench-baseline.sh --label base --preset "Voces Total"
  ```

- `tools/bench_compare.py` — compara dos JSON de benchmark:
  - Muestra una tabla de deltas por métrica (tiempo total, por paso, VRAM, energía por stem).
  - Emite un veredicto con umbrales explícitos (default: ±5 % tiempos, ±10 % VRAM, ±2 dB energía).
  - Avisa claramente cuando las versiones no coinciden.
  - Códigos de salida: `0` igual, `1` cambio, `2` no comparable.

  Uso típico:
  ```bash
  python3 tools/bench_compare.py .hermes/bench/<fecha>-base.json .hermes/bench/<fecha>-nuevo.json
  ```

### Reglas de la medición

1. Correr la base con la versión actual desplegada.
2. Aplicar **un solo cambio** (código, configuración o dependencia).
3. Volver a medir con las mismas flags y el mismo clip.
4. Comparar con `bench_compare.py` y registrar el veredicto.
5. No lanzar benchmarks de GPU automáticamente en CI; son pruebas manuales coordinadas.
