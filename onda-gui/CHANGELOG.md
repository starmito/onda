# Changelog

Todas las modificaciones notables de este proyecto se documentan en este archivo.

El formato está basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
y este proyecto adhiere a [Semantic Versioning](https://semver.org/lang/es/).

## [1.3.9] - 2025-05-23

### Corregido
- Grupo pitch no reproducía al reactivar tras cambio de grupo (`src=""` residual en activateGroup)
- Error de sintaxis (forEach duplicado) que rompía todo el JavaScript y deshabilitaba drag & drop

## [1.3.8] - 2025-05-23

### Modificado
- Simplificado ciclo de vida de audio: solo pausa y reset, sin limpiar src

### Corregido
- Conexiones de audio no se liberaban al cambiar de grupo activo

## [1.3.7] - 2025-05-23

### Añadido
- Cabecera con logo 🌊 Onda y número de versión

### Modificado
- Limpieza de audio.src al desactivar grupo para liberar conexiones HTTP

## [1.3.0] - 2025-05-22

### Añadido
- Sistema de grupo activo: solo un grupo con audio a la vez
- Resaltado visual del grupo activo (borde accent)

## [1.2.0] - 2025-05-22

### Añadido
- Botón TONO para rubberband con estilo mute/solo
- Slider de pitch con valor editable
- Export y delete en grupo pitch

## [1.1.0] - 2025-05-22

### Añadido
- Endpoint independiente para rubberband
- Columna «tono» en Results con checkbox y slider

## [1.0.0] - 2025-05-22

### Añadido
- Primera versión funcional completa con Python API
- Pipeline Viperx + Demucs
- Seek slider con waveform por stem
- Solo/Mute por stem
