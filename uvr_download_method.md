# Método de descarga de UVR

## Fuente
Repositorio: https://github.com/Anjok07/ultimatevocalremovergui
Archivo clave: `gui_data/constants.py`

## Método
UVR descarga modelos desde **GitHub Releases**, NO desde HuggingFace.

### URL base
```
NORMAL_REPO = "https://github.com/TRvlvr/model_repo/releases/download/all_public_uvr_models/"
```

### Catálogo
Los modelos se identifican por **hash MD5** en archivos JSON remotos:

| Tipo | URL | Modelos |
|---|---|---|
| VR (Roformer, MelBand, SCnet) | `.../vr_model_data/model_data_new.json` | 30 |
| MDX (Kim Vocal, UVR-MDX) | `.../mdx_model_data/model_data_new.json` | 86 |
| Demucs | `.../demucs_model_data/model_name_mapper.json` | 32 |
| **Total** | | **148 modelos** |

### Formato de descarga
Cada modelo se descarga como:
```
{NORMAL_REPO}{hash}
```
Ejemplo: `https://github.com/TRvlvr/model_repo/releases/download/all_public_uvr_models/0d0e6d143046b0eecc41a22e60224582`

El hash se usa como nombre de archivo.

### Name mappers
- MDX: `.../mdx_model_data/model_name_mapper.json` → 46 nombres legibles
- Demucs: `.../demucs_model_data/model_name_mapper.json` → 32 nombres legibles
- VR: el name mapper no existe (404). Los nombres vienen del propio nombre del archivo descargado o se muestran como hash.

### Relevancia para Onda
Nuestro backend actual usa `huggingface_hub.snapshot_download()`. Para descargar desde GitHub Releases necesitamos:
1. Añadir soporte para descarga directa (wget/curl) desde GitHub Releases
2. Mapear hashes a nombres legibles
3. Organizar en las categorías correctas (VR_Models/, MDX_Net_Models/, Demucs_Models/)
