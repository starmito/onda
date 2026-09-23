#!/usr/bin/env python3
"""Headless BS PolarFormer ONNX inference.

Usage:
    python3 inference_polarformer.py [model_dir] [input_audio] [output_dir]
                                     [num_overlap] [--device cuda|cpu]
                                     [--chunk-size N] [--batch-size N]
                                     [--progress-file FILE]
                                     [--pipeline-status FILE]

Reuses the PolarFormer ONNX separation logic from onda.polarformer.  Writes
per-chunk progress and pipeline_status.json so pipeline.sh can report real-time
progress exactly like inference_mdx.py, inference_onnx.py and
inference_universal.py.

Model detection rule (documented also in pipeline.sh):
    * explicit --vocal-type polarformer, OR
    * a YAML config in the model directory with ``model.use_pope: True``, OR
    * the model directory/onnx filename contains ``polarformer``.
"""

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from onda.polarformer import run_polarformer


def _parse_args(argv=None):
    parser = argparse.ArgumentParser(
        description="Headless BS PolarFormer ONNX source separation."
    )
    parser.add_argument("model", help="Model directory or .onnx path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument(
        "output", nargs="?", default="output_polarformer", help="Output directory"
    )
    parser.add_argument(
        "overlap", nargs="?", type=int, default=2, help="Overlap factor (default: 2)"
    )
    parser.add_argument(
        "--device", default="cuda", choices=["cuda", "cpu"], help="Device (default: cuda)"
    )
    parser.add_argument("--config", help="Explicit JSON/YAML config path")
    parser.add_argument("--chunk-size", type=int, help="Chunk size in samples")
    parser.add_argument("--batch-size", type=int, help="Inference batch size")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    parser.add_argument("--step-idx", type=int, default=0,
                        help="Step index for multi-step progress tracking")
    parser.add_argument("--total-steps", type=int, default=1,
                        help="Total number of steps for global progress")
    parser.add_argument("--step-id", default=None,
                        help="Stable step id for the UI steps list")
    parser.add_argument("--step-name", default=None,
                        help="Readable step name for the UI steps list")
    return parser.parse_args(argv)


def main(argv=None):
    args = _parse_args(argv)
    run_polarformer(args)


if __name__ == "__main__":
    main()
