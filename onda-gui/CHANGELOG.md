# Changelog

Todas las modificaciones notables de este proyecto se documentan en este archivo.

El formato está basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
y este proyecto adhiere a [Semantic Versioning](https://semver.org/lang/es/).

## [1.3.12-alpha] - 2025-05-23

### Corregido
- Conexiones HTTP residuales al borrar grupo pitch: ahora se abortan los audios
  (src="" + load()) antes de eliminar el DOM, evitando saturar el límite de 6
  conexiones del navegador
- Waveform del pitch no se generaba: el check de alpha no permitía reintentos
  tras un fallo. Reemplazado por sistema de flags (dataset.wfState) que permite
  distinguir entre "sin cargar", "cargando", "cargado" y "error"

## [1.3.11-alpha] - 2025-05-23

### Corregido
- Audio del pitch reproducía archivos antiguos tras reprocesar: caché HTTP del
  navegador (max-age=3600 en nginx) servía el archivo viejo al tener la misma URL.
  Fix: cache-busting con `?cb=timestamp` en todas las URLs de audio y descargas.

## [1.3.10-alpha] - 2025-05-23

### Añadido
- Caché de peaks (.peaks.json junto al WAV) — 2ª carga instantánea
- Posición de reproducción se mantiene al cambiar de grupo
- Seek slider se sincroniza al activar un grupo

### Corregido
- Waveform del grupo pitch no se generaba (URL absoluta vs relativa en /api/peaks)
- Reproducción volvía a 0:00 al cambiar de grupo

## [1.3.9-alpha] - 2025-05-23

### Añadido
- Endpoint `/api/peaks` para generar waveform en servidor (~2 KB en vez de 30 MB)
- Indicador de carga `...` en waveforms mientras se generan
- Fase de desarrollo en número de versión (`-alpha`, `-beta`, `-rc`, `-stable`)

### Corregido
- Waveforms tardaban ~30s en aparecer (descargaban el WAV completo)
- Seek slider del grupo pitch mostraba 0:00 / 0:00 (faltaba tracking de duration)
- Grupo pitch no reproducía al reactivar (`src=""` residual en activateGroup)

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
