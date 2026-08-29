#!/usr/bin/env python3
"""Headless MDX-C (MDX-Net) inference.

Usage:
    python3 inference_mdx.py [model_dir] [input_audio] [output_dir] [overlap]
                             [--batch-size N] [--device cuda|cpu]
                             [--progress-file FILE] [--pipeline-status FILE]

Reuses the MDX-C separation logic from onda.mdx (which mirrors UVR's
SeperateMDXC.demix()).  Writes per-chunk progress and pipeline_status.json
so pipeline.sh can report real-time progress exactly like inference_universal.py.
"""

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from onda.mdx import run_mdx


def _parse_args(argv=None):
    parser = argparse.ArgumentParser(
        description="Headless MDX-C source separation."
    )
    parser.add_argument("model", help="Model directory or .ckpt path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument("output", nargs="?", default="output_mdx",
                        help="Output directory (default: output_mdx)")
    parser.add_argument("overlap", nargs="?", type=int, default=8,
                        help="Overlap factor (default: 8)")
    parser.add_argument("--batch-size", type=int, default=1,
                        help="Inference batch size (default: 1)")
    parser.add_argument("--device", default="cuda", choices=["cuda", "cpu"],
                        help="Device (default: cuda)")
    parser.add_argument("--config", help="Explicit YAML config path")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    return parser.parse_args(argv)


def main(argv=None):
    args = _parse_args(argv)
    run_mdx(args)


if __name__ == "__main__":
    main()
