# Informe — Encargo 37: Grafo real de dependencias Python (v3.5.7)

- **Rama:** `fix/v3.5.7`
- **Fecha:** 2026-09-24
- **Estado:** ✅ Completado. **No se retira ninguna dependencia**.

## Resumen ejecutivo

Se ejecutaron las herramientas `deptry` y `vulture` en un entorno virtual aislado (`.hermes/.venv-deps/`) y se analizaron sus resultados. Todas las dependencias declaradas en `requirements-common.txt`, `requirements-docker.txt` y `pyproject.toml` están justificadas: o bien se importan directamente en el código de Onda, o bien son dependencias transitivas necesarias para `demucs`, `onnxruntime` o la pila CUDA. El único código señalado por `vulture` es un handler de señal cuyos parámetros son obligatorios aunque no se usen.

**Conclusión:** no hay dependencias muertas demostrables en este ciclo. El análisis crudo se deja en `.hermes/deps-deptry.txt` y `.hermes/deps-vulture.txt`.

## Herramientas y entorno

```bash
python3 -m venv .hermes/.venv-deps
.hermes/.venv-deps/bin/pip install deptry vulture
```

Ejecuciones:

```bash
.hermes/.venv-deps/bin/deptry . > .hermes/deps-deptry.txt 2>&1
.hermes/.venv-deps/bin/vulture onda/ backend/ tools/ --min-confidence 80 > .hermes/deps-vulture.txt 2>&1
```

> El entorno virtual se eliminó tras generar los informes para dejar el árbol limpio.

## Resultados de `deptry`

`deptry` reportó 38 problemas, la gran mayoría `DEP001` (módulo importado pero no declarado) porque el entorno de análisis **no tenía instaladas las dependencias del proyecto**; por tanto no pudo resolver módulos como `yaml`, `onnxruntime`, `einops`, etc. Esos avisos son falsos positivos de resolución, no indicios de dependencias muertas.

Los únicos avisos relevantes para este encargo son los `DEP002` (dependencia declarada pero no usada en el código fuente analizado):

| Paquete | ¿Declarado? | ¿Usado? | Decisión |
|---------|-------------|---------|----------|
| `torchvision` | `pyproject.toml`, `requirements-docker.txt` | No aparece como import `import torchvision` en el código Python de Onda, pero sí en `tools/bench-baseline.sh`, `Dockerfile`, `entrypoint.sh` y `requirements.lock`. Es dependencia de la pila CUDA/torch. | **No retirar** |
| `sphn` | `pyproject.toml`, `requirements-docker.txt` | No se importa directamente en Onda, pero `demucs==4.1.0` se instala con `--no-deps` y `sphn` es su dependencia. Documentado en `docs/dependencies-notes.md`. | **No retirar** |
| `PyYAML` | `pyproject.toml`, `requirements-common.txt`, `requirements-docker.txt` | Importado como `yaml` en `onda/manifest.py`, `onda/mdx.py`, `onda/onnx_mdx.py`, `onda/polarformer.py`, `onda/scnet.py`, `onda/vocal.py`, `inference_universal.py`, `backend/scripts/import_uvr_defaults.py`, `pipeline.sh` y múltiples tests. | **No retirar** |

### Verificaciones con `git grep`

- `torchvision`: presente en `tools/bench-baseline.sh`, `Dockerfile`, `entrypoint.sh`, `requirements.lock`, `AGENTS.md`, `CHANGELOG.md`.
- `sphn`: presente en `Dockerfile`, `docs/dependencies-notes.md`, `requirements.lock` (dependencia de demucs instalado con `--no-deps`).
- `yaml` / `PyYAML`: presente en decenas de archivos Python, shell y tests (ver `git grep -n "yaml\|PyYAML"`).

## Resultados de `vulture`

```text
tools/demucs_worker.py:62: unused variable 'frame' (100% confidence)
tools/demucs_worker.py:62: unused variable 'signum' (100% confidence)
```

Ambas variables pertenecen a un handler de señales (`signal.signal(..., handler)`). La firma del handler debe aceptar esos argumentos aunque no se utilicen; no es código muerto real.

**Decisión:** no se modifica `tools/demucs_worker.py`.

## Prueba negativa del guardián `tools/check-deps.sh`

Para demostrar que el guardián detecta una dependencia no deseada, se inyectó temporalmente `resampy` (paquete retirado en v3.5.6) en `requirements-docker.txt`:

```text
== check-deps: requirements vs estado instalado ==
[ ok ] requirements.lock: 85 paquetes leidos

-- Paquetes retirados --
[FAIL] requirements-docker.txt:34  paquete retirado 'resampy' detectado: resampy==0.4.3

-- requirements-common.txt --
[ ok ] requirements-common.txt: 11 paquetes declarados, todos con '==', todos coherentes con requirements.lock

-- requirements-docker.txt --
[info] requirements-docker.txt:31  'onnxruntime-gpu==1.30.0' -> NO aparece en requirements.lock (instalado fuera de site-packages, o no instalado)
[info] requirements-docker.txt:34  'resampy==0.4.3' -> NO aparece en requirements.lock (instalado fuera de site-packages, o no instalado)
[ ok ] requirements-docker.txt: 19 paquetes declarados, todos con '==', todos coherentes con requirements.lock

-- Resumen --
[info] requirements.lock: 85 paquetes instalados (referencia)
[info] declarados en los requirements: 30 (0 sin fijar)
[info] solo en el lock (dependencias transitivas, no declaradas a mano): 68

RESULTADO: 1 error(es), 0 aviso(s), 5 info
RC: 1
```

Tras revertir la inyección:

```text
== check-deps: requirements vs estado instalado ==
[ ok ] requirements.lock: 85 paquetes leidos

-- Paquetes retirados --

-- requirements-common.txt --
[ ok ] requirements-common.txt: 11 paquetes declarados, todos con '==', todos coherentes con requirements.lock

-- requirements-docker.txt --
[info] requirements-docker.txt:31  'onnxruntime-gpu==1.30.0' -> NO aparece en requirements.lock (instalado fuera de site-packages, o no instalado)
[ ok ] requirements-docker.txt: 18 paquetes declarados, todos con '==', todos coherentes con requirements.lock

-- Resumen --
[info] requirements.lock: 85 paquetes instalados (referencia)
[info] declarados en los requirements: 29 (0 sin fijar)
[info] solo en el lock (dependencias transitivas, no declaradas a mano): 68

RESULTADO: OK — 0 errores, 0 aviso(s), 4 info
RC: 0
```

## Verificación final

Todas las verificaciones obligatorias pasaron:

- `python3 -m pytest -q` → `158 passed, 2 skipped`
- `cd backend && go build ./... && go test ./... -count=1` → todos los paquetes OK
- `cd frontend && npm run check && npm run test && npm run build` → 0 errores, 120 tests pasados, build OK
- `bash tools/check-deps.sh` → `RESULTADO: OK — 0 errores`
- `bash tools/check-gitignore.sh` → `RESULTADO: OK - 0 fallos`

## Conclusión

No se encontraron dependencias Python ni código muerto demostrables para retirar en `fix/v3.5.7`. Los tres candidatos señalados por `deptry` (`torchvision`, `sphn`, `PyYAML`) están justificados, y el aviso de `vulture` es un falso positivo de handler de señales. El árbol de trabajo permanece limpio y el repositorio sigue pasando todos los tests y guardianes.
