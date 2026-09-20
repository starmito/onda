#!/usr/bin/env python3
"""Demucs worker using the official demucs.api with a JSON event contract.

This script is meant to be launched by pipeline.sh. It reads an audio file,
separates it with demucs and writes one JSON event per line to stdout. Exit
codes are explicit so the caller can distinguish model, audio, abort, write
and unexpected failures.
"""

import argparse
import json
import os
import signal
import sys
import time
import traceback
from pathlib import Path

# Exit codes (must stay in sync with pipeline.sh)
RC_OK = 0
RC_LOAD_MODEL = 20
RC_LOAD_AUDIO = 21
RC_ABORT = 30
RC_WRITE = 40
RC_UNEXPECTED = 99


def emit(event):
    """Write one JSON event line to stdout and flush immediately."""
    print(json.dumps(event, ensure_ascii=False), flush=True)


def parse_args():
    parser = argparse.ArgumentParser(
        description="Run demucs via its official Python API and emit JSON events."
    )
    parser.add_argument("--model", required=True, help="Demucs model name")
    parser.add_argument("--device", required=True, help="Inference device (cuda/cpu)")
    parser.add_argument("--input", required=True, help="Input audio file")
    parser.add_argument("--out", required=True, help="Parent output directory")
    parser.add_argument("--shifts", type=int, default=1, help="Shift-averaging passes")
    parser.add_argument(
        "--segment", type=int, default=0, help="Segment duration in seconds (0 = auto)"
    )
    parser.add_argument("--jobs", type=int, default=0, help="Parallel workers (0 = auto)")
    return parser.parse_args()


class DemucsWorker:
    def __init__(self, args):
        self.args = args
        self._abort = False
        self._last_pct = -1.0
        self.separator = None
        self.models = 1

        # Translate SIGINT into a cooperative abort flag. The callback checks it
        # and raises KeyboardInterrupt, which cancels the current chunk in
        # demucs.apply_model.
        signal.signal(signal.SIGINT, self._on_sigint)

    def _on_sigint(self, signum, frame):
        self._abort = True

    def _callback(self, info):
        """Emit progress events from demucs callback dict.

        pct = 100 * (model_idx_in_bag + (shift_idx + segment_offset/audio_length) / shifts) / models
        """
        if self._abort:
            raise KeyboardInterrupt("abort requested")

        model_idx = info.get("model_idx_in_bag", 0)
        shift_idx = info.get("shift_idx", 0)
        segment_offset = info.get("segment_offset", 0)
        audio_length = info.get("audio_length", 1)
        models = max(info.get("models", 1), 1)
        shifts = max(self.args.shifts, 1)

        # Avoid division by zero on degenerate inputs.
        if audio_length <= 0:
            audio_length = 1

        shift_fraction = (shift_idx + segment_offset / audio_length) / shifts
        pct = 100.0 * (model_idx + shift_fraction) / models
        # Clamp and enforce monotonicity.
        pct = max(0.0, min(100.0, pct))
        if pct < self._last_pct:
            pct = self._last_pct
        self._last_pct = pct

        emit(
            {
                "event": "progress",
                "pct": round(pct, 4),
                "model_idx_in_bag": model_idx,
                "shift_idx": shift_idx,
                "state": info.get("state", ""),
            }
        )

    def _load_model(self):
        import demucs.api

        try:
            self.separator = demucs.api.Separator(
                model=self.args.model,
                repo=None,
                device=self.args.device,
                shifts=self.args.shifts,
                overlap=0.25,
                split=True,
                segment=None if self.args.segment == 0 else self.args.segment,
                jobs=self.args.jobs,
                progress=False,
                callback=self._callback,
            )
        except demucs.api.LoadModelError as exc:
            self._error("LoadModelError", str(exc))
            sys.exit(RC_LOAD_MODEL)
        except Exception as exc:
            self._error("LoadModelError", f"{type(exc).__name__}: {exc}")
            sys.exit(RC_LOAD_MODEL)

        model_count = 1
        try:
            model_obj = self.separator.model
            # BagOfModels exposes a list of submodels.
            if hasattr(model_obj, "models"):
                model_count = len(model_obj.models)
        except Exception:
            pass
        self.models = max(model_count, 1)

        emit(
            {
                "event": "preflight",
                "model": self.args.model,
                "samplerate": self.separator.samplerate,
                "channels": self.separator.audio_channels,
                "models": self.models,
            }
        )

    def _error(self, kind, msg):
        emit({"event": "error", "kind": kind, "msg": msg})
        print(f"{kind}: {msg}", file=sys.stderr)

    def _separate(self):
        import demucs.api

        input_path = Path(self.args.input)
        try:
            _, sources = self.separator.separate_audio_file(input_path)
        except demucs.api.LoadAudioError as exc:
            self._error("LoadAudioError", str(exc))
            sys.exit(RC_LOAD_AUDIO)
        except KeyboardInterrupt:
            self._error("Abort", "separation aborted")
            sys.exit(RC_ABORT)
        except Exception as exc:
            self._error("LoadAudioError", f"{type(exc).__name__}: {exc}")
            sys.exit(RC_LOAD_AUDIO)
        return sources

    def _write_stems(self, sources):
        import demucs.api

        track_name = Path(self.args.input).stem
        out_dir = Path(self.args.out) / self.args.model / track_name
        try:
            out_dir.mkdir(parents=True, exist_ok=True)
        except Exception as exc:
            self._error("WriteError", f"cannot create output dir: {exc}")
            sys.exit(RC_WRITE)

        written = []
        for stem, wav in sources.items():
            path = out_dir / f"{stem}.wav"
            try:
                demucs.api.save_audio(
                    wav,
                    path,
                    samplerate=self.separator.samplerate,
                    clip="rescale",
                    bits_per_sample=16,
                    as_float=False,
                )
                written.append(stem)
            except Exception as exc:
                self._error("WriteError", f"{path}: {exc}")
                sys.exit(RC_WRITE)

        emit({"event": "stems", "dir": str(out_dir), "stems": written})
        return written

    def run(self):
        start = time.time()
        self._load_model()
        sources = self._separate()
        written = self._write_stems(sources)

        # Final sanity check: all expected stems must exist.
        expected = set(sources.keys())
        if set(written) != expected:
            self._error("WriteError", f"missing stems: {expected - set(written)}")
            sys.exit(RC_WRITE)

        emit({"event": "done", "seconds": round(time.time() - start, 3)})
        sys.exit(RC_OK)


def main():
    args = parse_args()
    worker = DemucsWorker(args)
    try:
        worker.run()
    except SystemExit:
        raise
    except Exception as exc:
        emit(
            {
                "event": "error",
                "kind": "Unexpected",
                "msg": f"{type(exc).__name__}: {exc}\n{traceback.format_exc()}",
            }
        )
        print(f"Unexpected: {exc}", file=sys.stderr)
        sys.exit(RC_UNEXPECTED)


if __name__ == "__main__":
    main()
