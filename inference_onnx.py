#!/usr/bin/env python3
"""Headless MDXNet ONNX inference.

Usage:
    python3 inference_onnx.py [model_dir] [input_audio] [output_dir] [overlap]
                              [--device cuda|cpu]
                              [--progress-file FILE] [--pipeline-status FILE]

Reuses the MDXNet ONNX separation logic from onda.onnx_mdx.  Writes per-chunk
progress and pipeline_status.json so pipeline.sh can report real-time progress
exactly like inference_mdx.py and inference_universal.py.
"""

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from onda.onnx_mdx import run_onnx_mdx


def _parse_args(argv=None):
    parser = argparse.ArgumentParser(
        description="Headless MDXNet ONNX source separation."
    )
    parser.add_argument("model", help="Model directory or .onnx path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument("output", nargs="?", default="output_onnx_mdx",
                        help="Output directory (default: output_onnx_mdx)")
    parser.add_argument("overlap", nargs="?", type=float, default=0.25,
                        help="Overlap factor (default: 0.25)")
    parser.add_argument("--device", default="cuda", choices=["cuda", "cpu"],
                        help="Device (default: cuda)")
    parser.add_argument("--config", help="Explicit JSON/YAML config path")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    return parser.parse_args(argv)


def main(argv=None):
    args = _parse_args(argv)
    run_onnx_mdx(args)


if __name__ == "__main__":
    main()
