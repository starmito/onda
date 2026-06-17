#!/usr/bin/env python3
"""Genera un WAV de prueba sintético para validar despliegues de Onda.

Usa únicamente la librería estándar de Python. Produce un archivo estéreo de
10 segundos a 44100 Hz con cuatro frecuencias sinusoidales mezcladas sin
silencios ni fragmentos con copyright.

Uso:
    python3 scripts/gen-test-audio.py

El archivo se escribe en la raíz del repositorio: test_sound.wav
"""
import math
import os
import struct
import wave

DURATION = 10.0
SAMPLE_RATE = 44100
NUM_CHANNELS = 2
SAMPLE_WIDTH = 2  # 16-bit PCM
FREQUENCIES = [200, 440, 880, 1760]
AMPLITUDE = 0.25
REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUTPUT_PATH = os.path.join(REPO_ROOT, "test_sound.wav")


def generate_sample(t: float) -> float:
    """Suma de sinusoides con amplitud fija."""
    return sum(AMPLITUDE * math.sin(2.0 * math.pi * freq * t) for freq in FREQUENCIES)


def main() -> None:
    num_samples = int(DURATION * SAMPLE_RATE)

    with wave.open(OUTPUT_PATH, "wb") as wav:
        wav.setnchannels(NUM_CHANNELS)
        wav.setsampwidth(SAMPLE_WIDTH)
        wav.setframerate(SAMPLE_RATE)

        for i in range(num_samples):
            t = i / SAMPLE_RATE
            sample = generate_sample(t)
            # 16-bit PCM signed; 32767.0 evita clipping cuando suma = 1.0
            pcm_value = int(max(-32768, min(32767, sample * 32767.0)))
            # Estéreo: mismo sample en ambos canales
            wav.writeframes(struct.pack("<hh", pcm_value, pcm_value))

    print(f"Generado: {OUTPUT_PATH}")
    print(f"  Duración: {DURATION}s | Frecuencia: {SAMPLE_RATE}Hz")
    print(f"  Canales: {NUM_CHANNELS} | Frecuencias: {FREQUENCIES}")


if __name__ == "__main__":
    main()
