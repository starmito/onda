# Notas de dependencias de Onda

Registro de decisiones, bloqueos y hallazgos medidos sobre las dependencias del pipeline.

---

## `demucs` en `4.1.0` — estado actual

`demucs` está fijado en `4.1.0` en `pyproject.toml`, `requirements.lock` y `Dockerfile`. Se instala con `--no-deps` y se declara explícitamente su dependencia `sphn==0.2.1`.

### Historial del bloqueo

Durante la Fase 4 de actualización de dependencias se probó `demucs==4.1.0` como candidata en GPU y se documentó un fallo al 65 % del paso Demucs (solo 2 de 4 pistas escritas, sin mensaje de error). Por eso `demucs` se mantuvo en `4.0.1` en ese momento.

En ciclos posteriores se volvió a validar `4.1.0` y el estado instalado actual (`requirements.lock`) refleja esa versión. Si reaparece el fallo del 65 %, `tools/verify-deps.sh` es la herramienta de referencia para comparar la imagen base contra una candidata.

### Dependencias relacionadas

Con `demucs 4.1.0` ya **no es necesario** `torchaudio`; por eso se retiraron `asteroid` y `torch_audiomentations` del pipeline. `torchaudio` no aparece en `requirements.lock`.

### Estado del resto de dependencias aprobadas

Las siguientes versiones quedan fijadas y validadas:

| Paquete      | Versión fijada |
|--------------|----------------|
| `librosa`    | `1.0.0`        |
| `omegaconf`  | `2.3.1`        |
| `soundfile`  | `0.14.0`       |
| `onnx`       | `1.23.0`       |
| `scipy`      | `1.18.1`       |
| `demucs`     | `4.1.0`        |
| `sphn`       | `0.2.1`        |

### Actualización reciente (v3.5.6)

En v3.5.6 se sincronizaron:
- `numpy` 2.4.6 → 2.5.3
- `onnxruntime-gpu` 1.26.0 → 1.30.0 (instalado en el volumen `/opt/pytorch-backends/<gpu>` por `entrypoint.sh`)
- Frontend: `vite` 8.0.16 → 8.3.0, `svelte` 5.56.3 → 5.57.1, `svelte-check` 4.6.0 → 4.7.6, `@sveltejs/vite-plugin-svelte` 7.3.0 → 7.3.1, `vitest` 4.1.9 → 5.0.1, `wavesurfer.js` 7.12.8 → 8.0.0.
- `typescript` sigue en 6.0.3 porque `svelte-check` declara peer `^5 || ^6` y aún no acepta la v7.

---

## Referencias

- `requirements-common.txt`
- `requirements-docker.txt`
- `requirements.lock`
- `pyproject.toml`
- `Dockerfile`
- `tools/verify-deps.sh`
- `tools/check-deps.sh`
