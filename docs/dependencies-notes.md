# Notas de dependencias de Onda

Registro de decisiones, bloqueos y hallazgos medidos sobre las dependencias del pipeline.

---

## `demucs` se mantiene en `4.0.1` — bloqueo documentado

### Hecho medido

Durante la Fase 4 de actualización de dependencias se probó `demucs==4.1.0` como candidata en GPU (ruta real de producción, contenedor CUDA, modelo `BS_Roformer_Viperx` + `htdemucs_ft`, misma máquina y mismos minutos que la versión anterior).

Resultado con `demucs 4.1.0`:

- El pipeline **se cae al 65 % de su propio paso** de Demucs.
- Código de salida del contenedor: **1**.
- **Sin ningún mensaje de error** en la salida.
- `pipeline_status.json` se queda congelado en estado `running`.
- Solo se escriben **2 de las 4 pistas** esperadas.

Con `demucs 4.0.1` y el **mismo audio, la misma máquina y los mismos minutos**, el pipeline completa las **4 pistas** sin error.

### Cómo se midió

- Herramienta: `tools/verify-deps.sh` en modo A/B (imagen base vs. imagen con `demucs==4.1.0` instalado encima).
- Entorno: GPU NVIDIA, volumen CUDA en solo lectura (`onda_pytorch-cache:/opt/pytorch-backends:ro`), modelo vocal real de producción montado en `/app/data/models/VR_Models/BS_Roformer_Viperx`.
- Audio: mezcla sintética de 30 s generada por el propio script de verificación.

### Consecuencias adicionales de `demucs 4.1.0`

- Arrastra una dependencia nueva: `sphn`.
- Rompe el paso de Demucs sin dejar traza útil, lo que hace inviable subir de versión hasta entender el motivo.

### Conclusión

**`demucs` se queda en `4.0.1` hasta que se entienda y se corrija el fallo del 65 %.** No subir a `4.1.0`.

### Dependencias relacionadas

La idea de retirar `torchaudio` del proyecto queda **aparcada** porque dependía de la subida a `demucs 4.1.0`. Hasta que Demucs se quede en `4.0.1`, `torchaudio` sigue siendo necesario (Demucs 4.0.1 lo utiliza).

### Estado del resto de dependencias aprobadas en Fase 4

Las siguientes versiones quedan fijadas y validadas con A/A en la imagen `onda:deps-20260920`:

| Paquete      | Versión fijada |
|--------------|----------------|
| `librosa`    | `1.0.0`        |
| `torchcodec` | `0.16.0`       |
| `omegaconf`  | `2.3.1`        |
| `soundfile`  | `0.14.0`       |
| `onnx`       | `1.23.0`       |
| `scipy`      | `1.18.1`       |

`demucs` es la única excepción: **no se toca**, permanece en `4.0.1`.

---

## Referencias

- `requirements-common.txt`
- `requirements-docker.txt`
- `requirements.lock`
- `tools/verify-deps.sh`
- `tools/check-deps.sh`
