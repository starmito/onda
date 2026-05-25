# Onda

Separación de audio profesional con soporte GPU. Basado en UVR (Ultimate Vocal Remover).

## Stack

- **Backend**: Go (API, pipeline, CLI)
- **Frontend**: Tauri + Svelte + TypeScript
- **Inferencia**: Python/PyTorch (intocable, dentro de contenedor Docker)

## Pipeline

| Preset | Vocal | Stems | VRAM |
|--------|-------|-------|------|
| Turbo | MelBand KJ | Demucs htdemucs_ft | ~8GB |
| Balance | PolarFormer | Demucs htdemucs_ft | ~12GB |
| Master | PolarFormer | Demucs + Bass dedicado | ~12GB |
| Ultimate | 4 pases dedicados | Drums/Bass/Other/Vocals | ~12GB |

## Estado

v2.0.0-alpha — Fase 1 completada (core Go pipeline funcional).
