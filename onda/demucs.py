"""onda demucs — HTDemucs stem separation.
Separates audio into drums, bass, other, vocals.
Uses the demucs CLI under the hood.
"""

import os
import sys
import subprocess


def run_demucs(args):
    """Run HTDemucs separation from CLI args."""
    print(f"🌊 onda demucs — {args.model}")
    print(f"   Input: {args.input}")
    print(f"   Device: {args.device}")

    if not os.path.isfile(args.input):
        print(f"ERROR: Input file not found: {args.input}")
        sys.exit(1)

    os.makedirs(args.output, exist_ok=True)

    # demucs installed via pip in the active venv
    cmd = [
        sys.executable, "-m", "demucs",
        "-o", args.output,
        "-d", args.device,
        args.model,
        args.input,
    ]
    print(f"   Running: demucs -o {args.output} -d {args.device} {args.model} ...")

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        print(f"ERROR: demucs failed (exit {result.returncode})")
        print(result.stderr[-500:])
        sys.exit(1)

    # Show output
    model_clean = args.model.replace("_ft", "")
    out_dir = os.path.join(args.output, model_clean)
    for root, dirs, files in os.walk(out_dir):
        for f in sorted(files):
            if f.endswith(".wav"):
                filepath = os.path.join(root, f)
                size_mb = os.path.getsize(filepath) / 1e6
                print(f"   ✓ {f} ({size_mb:.0f}MB)")

    print(f"✅ Done! Output in {out_dir}")
