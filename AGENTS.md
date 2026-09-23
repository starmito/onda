# AGENTS.md — Reglas de trabajo para agentes de código (OpenCode)

Reglas OBLIGATORIAS para cualquier agente que trabaje en este repositorio. Incumplirlas es un fallo grave.

## 🚫 PROHIBIDO (sin excepciones)

- **NUNCA usar `/tmp` ni `/var/tmp`**: el sandbox de seguridad lo bloquea (auto-reject) y rompe el flujo de trabajo. Cualquier archivo temporal de depuración se crea DENTRO del repo (p.ej. `debug/` o en la raíz) y se borra antes de commitear.
- **NUNCA `git add -A` ni `git add .`**: añadir SIEMPRE archivos explícitos.
- **NUNCA añadir al commit**: `config/`, `*.orig`, `frontend/dist/`, `node_modules/`, `.git/`, artefactos de build ni logs de depuración.
- **NUNCA desplegar**: `deploy.sh`, `docker compose up -d`, etc. El robot de auto-deploy se encarga. (SÍ se puede: `docker compose build` sin `up`, o verificación nativa, para comprobar que compila.)
- **NUNCA tocar `config/`** (config local con secretos) ni archivos `*.orig`.
- **NUNCA usar `sudo`** ni instalar paquetes del sistema (el sandbox lo bloquea).

## ✅ SIEMPRE

- Compilar y pasar TODOS los tests antes de commitear:
  - `cd backend && go build ./... && go test ./...`
  - `pytest` desde la raíz del repo
- Ejecutar los **guardianes del repo** antes de commitear cambios de dependencias o código de terceros:
  - `tools/check-gitignore.sh` — asegura que `.hermes/` y rutas ignoradas no están trackeadas.
  - `tools/check-deps.sh` — asegura que los requirements Python están fijados y coinciden con `requirements.lock`.
  - `tools/check-licenses.sh` — bloquea licencias prohibidas (AGPL-*, GPL-2.0, GPL-3.0, SSPL-*, BUSL-*) en dependencias de producción del frontal, dependencias Python conocidas y código vendorizado (`lib_v5/`); también verifica que `go mod tidy -diff` esté vacío. Funciona sin red si los módulos de Go están en caché; si no, avisa y sigue.
  - Los tres guardianes se ejecutan automáticamente en la suite via `tests/unit/test_guards.py`.
- Si un test necesita `aubio`/`sox` y no están instalados, usar el patrón `skipIfMissingBinary` ya existente (saltar limpiamente, no fallar).
- Commits conventional (`feat:`, `fix:`, `test:`, `refactor:`, `docs:`, `chore:`) y push a la rama de trabajo actual (`origin/fix/v3.5.6` en este punto del ciclo de release).
- La versión sale del fichero `VERSION` en la raíz del repo. `build.sh` y `deploy.sh` leen `VERSION` y validan que `onda/_version.py`, `pyproject.toml` y `frontend/package.json` coincidan. NUNCA hardcodear versiones a mano ni duplicar la lógica de versionado.
- Si necesitas helpers temporales de depuración: crearlos dentro del repo y borrarlos antes del commit.

## Contexto del proyecto

- **Onda**: separador de fuentes musicales (Demucs, ViperX, MDX/SCNet/ONNX) + DAW ligero. Backend Go (`backend/`), frontend Svelte 5 (`frontend/`), pipeline Python (`onda/`).
- **Rama de trabajo actual**: `fix/v3.5.6`. No tocar `main` ni tags salvo instrucción explícita.
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
