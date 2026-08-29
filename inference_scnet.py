#!/usr/bin/env python3
"""Headless SCNet inference.

Usage:
    python3 inference_scnet.py [model_dir] [input_audio] [output_dir]
                               [--device cuda|cpu]
                               [--progress-file FILE] [--pipeline-status FILE]

Reuses the SCNet separation logic from onda.scnet.  Writes per-chunk progress
and pipeline_status.json so pipeline.sh can report real-time progress exactly
like inference_universal.py and inference_mdx.py.
"""

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from onda.scnet import run_scnet


def _parse_args(argv=None):
    parser = argparse.ArgumentParser(
        description="Headless SCNet source separation."
    )
    parser.add_argument("model", help="Model directory or .ckpt path")
    parser.add_argument("input", help="Input audio file")
    parser.add_argument("output", nargs="?", default="output_scnet",
                        help="Output directory (default: output_scnet)")
    parser.add_argument("--device", default="cuda", choices=["cuda", "cpu"],
                        help="Device (default: cuda)")
    parser.add_argument("--config", help="Explicit YAML config path")
    parser.add_argument("--progress-file", help="Per-chunk progress JSON file")
    parser.add_argument("--pipeline-status", help="pipeline_status.json file")
    return parser.parse_args(argv)


def main(argv=None):
    args = _parse_args(argv)
    run_scnet(args)


if __name__ == "__main__":
    main()
