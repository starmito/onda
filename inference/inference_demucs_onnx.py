#!/usr/bin/env python3
"""
Demucs ONNX inference via StemSplitio's demucs-onnx package.
Headless — no GUI dependencies. GPU auto-detected via onnxruntime.

Usage:
  python3 inference_demucs_onnx.py <input_audio> [output_dir] [--stems vocals,drums,bass,other]
  python3 inference_demucs_onnx.py --list-models
  python3 inference_demucs_onnx.py --runtime
  python3 -c "from inference_demucs_onnx import separate; separate(...)"
"""

import os
import argparse
import time
import demucs_onnx
import soundfile as sf

MODELS_DIR = os.path.join(os.path.dirname(__file__), '..', 'models', 'Demucs_ONNX')
DEFAULT_STEMS = ['vocals', 'drums', 'bass', 'other']


def separate(
    input_path,
    output_dir=None,
    stems=None,
    model="htdemucs_ft",
    precision="fp32",
    cache_dir=None,
):
    """
    Run Demucs ONNX separation.

    Args:
        input_path: Path to input audio file
        output_dir: Directory for output stems (default: "output")
        stems: List of stems to extract (default: all 4)
        model: Model name (htdemucs_ft, htdemucs, htdemucs_6s)
        precision: fp32 or fp16
        cache_dir: Model cache directory (default: MODELS_DIR)

    Returns:
        dict with stem names -> output file paths
    """
    if stems is None:
        stems = DEFAULT_STEMS
    if cache_dir is None:
        cache_dir = MODELS_DIR
    if output_dir is None:
        output_dir = "output"

    os.makedirs(output_dir, exist_ok=True)

    # Validate input file exists
    if not os.path.isfile(input_path):
        raise FileNotFoundError(f"Archivo de entrada no encontrado: {input_path}")

    # Get input duration for RTF calculation
    try:
        info = sf.info(input_path)
        duration = info.duration
    except Exception:
        duration = 0

    start = time.time()
    try:
        result = demucs_onnx.separate(
            input=input_path,
            output_dir=output_dir,
            model=model,
            stems=stems,
            providers="auto",  # auto-detect CUDA/CPU
            precision=precision,
            cache_dir=cache_dir,
            verbose=False,
            progress=False,
        )
    except Exception as e:
        raise RuntimeError(
            f"Error durante la separación Demucs ONNX: {e}\n"
            f"  Input: {input_path}\n"
            f"  Modelo: {model}\n"
            f"  Stems: {stems}"
        ) from e
    elapsed = time.time() - start

    # Build output file paths (demucs_onnx already wrote them)
    output_files = {}
    for stem in result:
        out_path = os.path.join(output_dir, f"{stem}.wav")
        if os.path.exists(out_path):
            output_files[stem] = out_path
        else:
            # Fallback: return path anyway
            output_files[stem] = out_path

    # Log: processing time and realtime ratio
    if duration > 0:
        rtf = duration / elapsed if elapsed > 0 else float('inf')
        print(f"Demucs ONNX: {duration:.1f}s audio procesado en {elapsed:.1f}s "
              f"({rtf:.1f}x realtime)")
    else:
        print(f"Demucs ONNX: procesado en {elapsed:.1f}s")
    print(f"Stems generados: {', '.join(output_files.keys())}")

    return output_files


def list_models():
    """List available Demucs ONNX models."""
    models = demucs_onnx.list_models()
    print("Modelos Demucs ONNX disponibles:")
    for name, info in models.items():
        kind = info.get('kind', '')
        sources = info.get('sources', '')
        desc = ""
        if kind == "specialist_bag":
            desc = f" — {len(sources.split(','))} stems ({sources}) [specialist bag]"
        elif kind == "specialist":
            desc = f" — especialista ({sources})"
        elif kind == "single":
            desc = f" — {len(sources.split(','))} stems ({sources})"
        print(f"  {name}{desc}")
    return models


def main():
    parser = argparse.ArgumentParser(
        description="Demucs ONNX — separación de stems de audio"
    )
    parser.add_argument("input", nargs="?", help="Archivo de audio de entrada")
    parser.add_argument("output_dir", nargs="?", default="output",
                        help="Directorio de salida (default: output)")
    parser.add_argument("--stems", default="vocals,drums,bass,other",
                        help="Stems a extraer (default: vocals,drums,bass,other)")
    parser.add_argument("--model", default="htdemucs_ft",
                        help="Modelo ONNX (default: htdemucs_ft)")
    parser.add_argument("--precision", default="fp32", choices=["fp32", "fp16"],
                        help="Precisión (default: fp32)")
    parser.add_argument("--list-models", action="store_true",
                        help="Listar modelos disponibles")
    parser.add_argument("--runtime", action="store_true",
                        help="Mostrar info del runtime")

    args = parser.parse_args()

    if args.list_models:
        list_models()
        return

    if args.runtime:
        info = demucs_onnx.describe_runtime()
        print(f"onnxruntime: {info['onnxruntime']}")
        print(f"Providers disponibles: {', '.join(info['available_providers'])}")
        return

    if not args.input:
        parser.error("Se requiere INPUT (archivo de audio)")

    stems = [s.strip() for s in args.stems.split(",")]
    separate(
        input_path=args.input,
        output_dir=args.output_dir,
        stems=stems,
        model=args.model,
        precision=args.precision,
    )


if __name__ == "__main__":
    main()
