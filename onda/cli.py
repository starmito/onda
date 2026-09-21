#!/usr/bin/env python3
"""onda — Audio source separation pipeline.
Three independent subcommands for GUI integration:
  onda viperx   input.wav → vocals + instrumental
  onda demucs   instrumental.wav → drums, bass, other, vocals
  onda pitch    stems_dir → pitch-shifted stems
"""

import argparse
import json
import sys


def main():
    parser = argparse.ArgumentParser(
        prog="onda",
        description="Audio source separation pipeline (BS-Roformer → HTDemucs → Rubberband)",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # ── viperx ──────────────────────────────────────────────
    vx = sub.add_parser("viperx", help="BS-Roformer source separation → vocals + instrumental")
    vx.add_argument("input", help="Input audio file (.wav, .mp3, .flac...)")
    vx.add_argument("-m", "--model", required=True, help="Path to .ckpt checkpoint")
    vx.add_argument("-c", "--config", help="Path to .yaml config (auto-detected if same dir)")
    vx.add_argument("--overlap", type=int, default=8, help="Overlap chunks (default: 8)")
    vx.add_argument("-o", "--output", default="output_viperx", help="Output directory")
    vx.add_argument("--device", default="cuda", choices=["cuda","cpu"], help="Device (default: cuda)")

    # ── demucs ──────────────────────────────────────────────
    dm = sub.add_parser("demucs", help="HTDemucs stem separation → drums, bass, other, vocals")
    dm.add_argument("input", help="Input audio file (instrumental)")
    dm.add_argument("-m", "--model", default="htdemucs", help="Demucs model (htdemucs, htdemucs_ft)")
    dm.add_argument("-o", "--output", default="output_demucs", help="Output directory")
    dm.add_argument("--device", default="cuda", choices=["cuda","cpu"], help="Device (default: cuda)")

    # ── pitch ───────────────────────────────────────────────
    pt = sub.add_parser("pitch", help="Rubberband pitch shifting (all stems except drums)")
    pt.add_argument("input_dir", help="Directory with drums/bass/other/vocals.wav")
    pt.add_argument("-s", "--semitones", type=float, default=2.0, help="Semitones to shift (default: 2)")
    pt.add_argument("-o", "--output", default="output_pitchshift", help="Output directory")
    pt.add_argument("--skip", nargs="*", default=["drums"], help="Stems to skip (default: drums)")

    # ── manifest ────────────────────────────────────────────
    mf = sub.add_parser("manifest", help="Generate model.manifest.json files")
    mf.add_argument("model_dir", nargs="?", help="Single model directory to generate")
    mf.add_argument("--regenerate", action="store_true", help="Regenerate all manifests under the models root")
    mf.add_argument("--models-dir", default=None, help="Models root directory (default: data/models)")
    mf.add_argument("--dry-run", action="store_true", help="Print manifests without writing files")

    args = parser.parse_args()

    if args.command == "viperx":
        from onda.viperx import run_viperx
        run_viperx(args)
    elif args.command == "demucs":
        from onda.demucs import run_demucs
        run_demucs(args)
    elif args.command == "pitch":
        from onda.pitch import run_pitch
        run_pitch(args)
    elif args.command == "manifest":
        from onda.manifest import (
            default_models_dir,
            generate_manifest,
            regenerate_manifests,
            write_manifest,
        )

        if args.model_dir:
            manifest = generate_manifest(args.model_dir)
            print(json.dumps(manifest, indent=2, ensure_ascii=False))
            if not args.dry_run:
                write_manifest(manifest, args.model_dir)
        elif args.regenerate:
            models_dir = args.models_dir or default_models_dir()
            results = regenerate_manifests(models_dir, write=not args.dry_run)
            print(json.dumps(results, indent=2, ensure_ascii=False))
        else:
            parser.error("Provide a model_dir or use --regenerate")


if __name__ == "__main__":
    main()
