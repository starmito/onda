# AGENTS.md — Reglas de trabajo para agentes de código (OpenCode)

Reglas OBLIGATORIAS para cualquier agente que trabaje en este repositorio. Incumplirlas es un fallo grave.

## 🚫 PROHIBIDO (sin excepciones)

- **NUNCA usar `/tmp` ni `/var/tmp`**: el sandbox de seguridad lo bloquea (auto-reject) y rompe el flujo de trabajo. Cualquier archivo temporal de depuración se crea DENTRO del repo (p.ej. `debug/` o en la raíz) y se borra antes de commitear.
- **NUNCA `git add -A` ni `git add .`**: añadir SIEMPRE archivos explícitos.
- **NUNCA añadir al commit**: `config/`, `*.orig`, `frontend/dist/`, `node_modules/`, `.git/` ni ningún artefacto de build.
- **NUNCA desplegar**: `deploy.sh`, `docker compose up -d`, etc. El robot de auto-deploy se encarga. (SÍ se puede: `docker compose build` sin `up`, o verificación nativa, para comprobar que compila.)
- **NUNCA tocar `config/`** (config local con secretos) ni archivos `*.orig`.
- **NUNCA usar `sudo`** ni instalar paquetes del sistema (el sandbox lo bloquea).

## ✅ SIEMPRE

- Compilar y pasar TODOS los tests antes de commitear:
  - `cd backend && go build ./... && go test ./...`
  - `pytest` desde la raíz del repo
- Ejecutar los **guardianes del repo** antes de commitear cambios de dependencias o código de terceros:
  - `tools/check-deps.sh` — asegura que los requirements Python están fijados y coinciden con `requirements.lock`.
  - `tools/check-licenses.sh` — bloquea licencias prohibidas (AGPL-*, GPL-2.0, GPL-3.0, SSPL-*, BUSL-*) en dependencias de producción del frontal, dependencias Python conocidas y código vendorizado (`lib_v5/`); también verifica que `go mod tidy -diff` esté vacío. Funciona sin red si los módulos de Go están en caché; si no, avisa y sigue.
  - Ambos guardianes se ejecutan automáticamente en la suite via `tests/unit/test_guards.py`.
- Si un test necesita `aubio`/`sox` y no están instalados, usar el patrón `skipIfMissingBinary` ya existente (saltar limpiamente, no fallar).
- Commits conventional (`feat:`, `fix:`, `test:`, `refactor:`, `chore:`) y push a `origin/feat/v3.2.0`.
- Las versiones salen de los TAGS de git (`onda-vX.Y.Z`, `gui-vX.Y.Z`) en tiempo de build — NUNCA hardcodear versiones a mano. `build.sh` y `deploy.sh` ya tienen esa lógica; NO duplicarla.
- Si necesitas helpers temporales de depuración: crearlos dentro del repo y borrarlos antes del commit.

## Contexto del proyecto

- **Onda**: separador de fuentes musicales (Demucs/UVR) + DAW ligero. Backend Go (`backend/`), frontend Svelte (`frontend/`), pipeline Python (`onda/`).
- La API real usa rutas `/app/input/` (NO `/input/` — las referencias viejas a `/input/` son drift obsoleto).
- `onda-gui/` fue eliminado en v3.2.0: no existe ya como directorio ni como servicio.
