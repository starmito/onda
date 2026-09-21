# Ruta AMD/ROCm retirada

## Qué se retiró

A partir de la versión **v3.5.0** se ha eliminado del repositorio la ruta de
soporte para GPU AMD/ROCm:

- `Dockerfile.amd`
- `docker-compose.rocm.yml`
- `requirements-docker-amd.txt`
- Rama ROCm en `entrypoint.sh` (instalación de wheels `+rocm7.1` y detección de
  `/dev/kfd`)
- Detección de ROCm en `onda/detect_gpu.sh`
- Referencias en `deploy.sh`, `Makefile`, `README.md`, `.env.example`, frontend y
  API

## Por qué

Adri decidió que **a medio plazo no se va a trabajar en la ruta ROCm/AMD**.
Mantener ficheros y ramas de código que nadie valida genera "drift" silencioso,
confunde a quienes despliegan el proyecto y acumula dependencias sin fijar (por
ejemplo, `requirements-docker-amd.txt` contenía 29 versiones sin pinchar).

Retirarlo es más limpio que dejarlo podrirse.

## Estado de las dependencias

Las dependencias ROCm ya no se mantienen ni se instalan en la imagen unificada.
No se garantiza que una build actual funcione con `onnxruntime-rocm` ni con
wheels `torch+rocm`.

## Cómo recuperarlo

La ruta completa está en la historia de git anterior a `feat/v3.5.0`. Los
ficheros clave a rescatar son los listados en "Qué se retiró". Si algún día
vuelve a ser prioridad, se puede reconstruir a partir de ese punto.

## Alternativa actual

- **NVIDIA CUDA**: ruta principal y validada.
- **CPU**: fallback universal, sin requisitos extra.
