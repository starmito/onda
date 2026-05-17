"""onda pitch — Rubberband pitch shifting.
Shifts pitch of all stems except drums by N semitones.
Uses rubberband CLI (must be installed: apt install rubberband-cli).
"""

import os
import sys
import shutil
import subprocess


SUPPORTED_STEMS = ["drums", "bass", "other", "vocals"]


def run_pitch(args):
    """Run pitch shifting from CLI args."""
    semitones = args.semitones
    skip = set(args.skip) if args.skip else set()

    print(f"🌊 onda pitch — {semitones:+g} semitones")
    print(f"   Input dir: {args.input_dir}")
    print(f"   Skip: {', '.join(skip) if skip else 'none'}")

    if not os.path.isdir(args.input_dir):
        print(f"ERROR: Input directory not found: {args.input_dir}")
        sys.exit(1)

    os.makedirs(args.output, exist_ok=True)

    for stem in SUPPORTED_STEMS:
        input_file = os.path.join(args.input_dir, f"{stem}.wav")
        output_file = os.path.join(args.output, f"{stem}.wav")

        if not os.path.isfile(input_file):
            print(f"   ⚠ Skipping {stem}.wav (not found)")
            continue

        if stem in skip:
            print(f"   🔒 {stem}.wav (copied, no shift)")
            shutil.copy2(input_file, output_file)
            continue

        print(f"   🎵 {stem}.wav → +{semitones:g} semitones...")

        cmd = ["rubberband", "-p", str(semitones), input_file, output_file]
        result = subprocess.run(cmd, capture_output=True, text=True)

        if result.returncode != 0:
            print(f"ERROR: rubberband failed on {stem}.wav")
            print(result.stderr[-300:])
            sys.exit(1)

        size_mb = os.path.getsize(output_file) / 1e6
        print(f"      ✓ {stem}.wav ({size_mb:.0f}MB)")

    print(f"✅ Done! Output in {args.output}/")
