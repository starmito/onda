# Procedencia del código vendorizado en `lib_v5/`

Este directorio contiene código de terceros utilizado por el pipeline de separación de Onda. A continuación se indica de dónde procede cada bloque y bajo qué licencia se distribuye.

## ZFTurbo — Roformer models (MIT)

- **Origen:** https://github.com/ZFTurbo/Music-Source-Separation-Training
- **Licencia:** MIT (`LICENSE-ZFTurbo.txt`)
- **Copyright:** 2024 Roman Solovyev (ZFTurbo)
- **Ficheros:**
  - `lib_v5/bs_roformer.py`
  - `lib_v5/mel_band_roformer.py`
  - `lib_v5/attend.py` (helper compartido por los modelos anteriores)

Estos ficheros se corresponden con los modelos `models/bs_roformer/bs_roformer.py`,
`models/bs_roformer/mel_band_roformer.py` y `models/bs_roformer/attend.py` del repositorio upstream.

## Ultimate Vocal Remover GUI (UVR) — resto de `lib_v5/` y `vr_network/` (MIT)

- **Origen:** https://github.com/Anjok07/ultimatevocalremovergui
- **Licencia:** MIT (`LICENSE-UVR.txt`)
- **Copyright:** 2020-2025 Anjok07, aufr33 y colaboradores de Ultimate Vocal Remover GUI
- **Ficheros:**
  - `lib_v5/mdxnet.py`
  - `lib_v5/modules.py`
  - `lib_v5/tfc_tdf_v3.py`
  - `lib_v5/spec_utils.py`
  - `lib_v5/pyrb.py`
  - `lib_v5/vr_network/*` (`layers.py`, `layers_new.py`, `nets.py`, `nets_new.py`, `model_param_init.py`)
  - `lib_v5/vr_network/modelparams/*.json` (parámetros de modelos VR)
  - `lib_v5/mixer.ckpt` (checkpoint del mezclador VR)

El repositorio upstream no incluye un fichero `LICENSE` independiente en su raíz, pero su
`README.md` indica explícitamente que el código está bajo **MIT License** y GitHub lo clasifica
como MIT.

## Apollo / look2hear (CC BY-SA 4.0)

- **Origen:** https://github.com/JusperLee/Apollo (directorio `look2hear/models/`)
- **Licencia:** Creative Commons Attribution-ShareAlike 4.0 International (`LICENSE-Apollo.txt`)
- **Copyright:** Kai Li (JusperLee) y colaboradores de Apollo
- **Ficheros:**
  - `lib_v5/apollo_model_data/apollo.py`
  - `lib_v5/apollo_model_data/base_model.py`

> Nota: a fecha de esta revisión no se ha encontrado ningún importador activo de
> `apollo_model_data` en `inference_universal.py`, `onda/viperx.py` ni en el resto del
> repo. Los ficheros se mantienen en `lib_v5/` por conservar la procedencia documentada.

## Fuentes consultadas

- `https://raw.githubusercontent.com/ZFTurbo/Music-Source-Separation-Training/main/LICENSE`
- `https://api.github.com/repos/Anjok07/ultimatevocalremovergui` (licencia MIT según metadatos de GitHub; README confirma MIT)
- `https://raw.githubusercontent.com/JusperLee/Apollo/main/LICENSE`
