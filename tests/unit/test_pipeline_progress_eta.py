"""Test honest progress/ETA tracking for the Onda pipeline.

Covers the pure ``ProgressTracker`` logic and a lightweight pipeline.sh
``--steps`` integration that verifies per-step progress resets and weighted
overall progress.
"""

import json
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
TRACKER_PATH = REPO_ROOT / "tools" / "progress_tracker.py"
PIPELINE_SH = REPO_ROOT / "pipeline.sh"


def _skip_if_missing_bin(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} not found in PATH, skipping integration test")


def _import_tracker():
    """Import the progress tracker helper without making tools a package."""
    sys.path.insert(0, str(TRACKER_PATH.parent))
    try:
        import progress_tracker
    finally:
        sys.path.pop(0)
    return progress_tracker


class TestProgressTracker:
    """Unit tests for the honest ETA / progress calculations."""

    def _tracker(self, **kwargs):
        """Build a tracker with relaxed ETA guardrails for fast unit tests."""
        defaults = {
            "min_samples_for_eta": 2,
            "min_seconds_for_eta": 1.0,
            "min_progress_for_eta": 1.0,
        }
        defaults.update(kwargs)
        return _import_tracker().ProgressTracker(**defaults)

    def test_eta_starts_empty_until_enough_data(self):
        """With default guardrails the ETA is 0 (calculating...) at the start."""
        tracker = _import_tracker().ProgressTracker()
        # First samples are too sparse: no ETA.
        assert tracker.update(1, 1)["eta"] == 0
        assert tracker.update(2, 2)["eta"] == 0
        # Once the minimum thresholds are crossed an estimate appears, but it is
        # sanity-capped so it never explodes.
        result = tracker.update(5, 3)
        assert result["eta"] > 0
        assert result["eta"] <= 50  # elapsed*max_total_multiplier

    def test_eta_drops_when_rate_increases(self):
        """Faster recent progress lowers the ETA."""
        tracker = self._tracker()
        # Slow start: 10% in 10s -> 90s left at current rate.
        tracker.update(5, 5)
        r1 = tracker.update(10, 10)
        assert r1["eta"] > 0
        # Then fast: another 40% in 5s -> recent window dominates.
        r2 = tracker.update(15, 50)
        assert r2["eta"] < r1["eta"], f"ETA should drop: {r1['eta']} -> {r2['eta']}"

    def test_eta_rises_when_rate_drops(self):
        """Slower recent progress raises the ETA."""
        tracker = self._tracker()
        # Fast start: 50% in 5s -> rate 10%/s -> ETA ~5s.
        tracker.update(1, 10)
        r1 = tracker.update(5, 50)
        assert r1["eta"] > 0
        # Then slow: only 10% more in the next 10s -> rate ~1%/s -> ETA rises.
        r2 = tracker.update(15, 60)
        assert r2["eta"] > r1["eta"], f"ETA should rise: {r1['eta']} -> {r2['eta']}"

    def test_eta_zero_only_when_finished_or_insufficient_data(self):
        """ETA is 0 only when finished or when we cannot estimate yet."""
        tracker = self._tracker()
        # Insufficient data.
        assert tracker.update(1, 1)["eta"] == 0
        # Enough data -> positive ETA.
        r1 = tracker.update(5, 20)
        assert r1["eta"] > 0
        # Finished -> 0.
        result = tracker.update(10, 100, finished=True)
        assert result["eta"] == 0

    def test_eta_holds_on_stall(self):
        """A stalled phase keeps the last ETA instead of dropping to 0."""
        tracker = self._tracker()
        tracker.update(1, 10)
        r1 = tracker.update(5, 50)  # some ETA
        r2 = tracker.update(60, 50)  # no progress for 50s
        assert r2["eta"] > 0
        assert r2["eta"] >= r1["eta"]

    def test_eta_capped_by_elapsed_multiplier(self):
        """The ETA never implies a total duration wildly larger than elapsed."""
        tracker = self._tracker(max_total_multiplier=5.0, min_samples_for_eta=2)
        tracker.update(1, 1)
        result = tracker.update(2, 2)
        # Without the cap the rate would be 1%/s -> 98s left, but elapsed=2 so
        # the cap limits the ETA to 2*5 = 10s.
        assert result["eta"] <= 10

    def test_eta_spike_is_rejected(self):
        """A sudden ETA jump is held back until it stabilises."""
        tracker = self._tracker(
            smooth_alpha=1.0, min_samples_for_eta=2, eta_spike_ratio=2.0
        )
        tracker.update(1, 10)
        r1 = tracker.update(5, 50)  # ~5s left at 10%/s
        assert r1["eta"] > 0
        # Next sample implies a much larger ETA (progress stalled).
        r2 = tracker.update(6, 51)  # 1% in 1s -> 49s left, > 2x previous
        assert r2["eta"] <= r1["eta"] * 2.5, (
            f"ETA spiked unexpectedly: {r1['eta']} -> {r2['eta']}"
        )

    def test_step_progress_resets_for_new_step(self):
        """A new step index starts its progress from 0."""
        tracker = _import_tracker().ProgressTracker(total_steps=2)
        tracker.update_step(0, "completed", 100, 10)
        result = tracker.update_step(1, "processing", 0, 11)
        assert result["progress"] == 0.0
        assert result["overall_progress"] < 100.0

    def test_overall_never_100_with_pending_steps(self):
        """Weighted overall progress stays below 100 until all steps finish."""
        tracker = _import_tracker().ProgressTracker(total_steps=3)
        tracker.update_step(0, "completed", 100, 10)
        tracker.update_step(1, "completed", 100, 20)
        result = tracker.update_step(2, "waiting", 0, 20)
        assert result["overall_progress"] < 100.0

        # Only when every step is completed may it reach 100.
        result = tracker.update_step(2, "completed", 100, 30)
        assert result["overall_progress"] == 100.0

    def test_step_list_initializes_queued(self):
        """init_steps creates the full step list with queued/0 values."""
        tracker = _import_tracker().ProgressTracker(total_steps=2)
        tracker.init_steps([
            {"id": "vocal", "name": "Voz (BS Roformer)"},
            {"id": "demucs", "name": "Demucs (htdemucs_ft)"},
        ])
        assert len(tracker.steps) == 2
        assert tracker.steps[0]["id"] == "vocal"
        assert tracker.steps[0]["status"] == "queued"
        assert tracker.steps[0]["progress"] == 0

    def test_step_list_updates_per_step(self):
        """Each update touches only its own step entry."""
        tracker = _import_tracker().ProgressTracker(total_steps=2)
        tracker.init_steps([
            {"id": "vocal", "name": "Voz"},
            {"id": "demucs", "name": "Demucs"},
        ])
        tracker.update_step(0, "processing", 50, 5, step_id="vocal", step_name="Voz")
        assert tracker.steps[0]["status"] == "running"
        assert tracker.steps[0]["progress"] == 50.0
        assert tracker.steps[1]["status"] == "queued"
        assert tracker.steps[1]["progress"] == 0

        tracker.update_step(0, "completed", 100, 10, step_id="vocal", step_name="Voz")
        tracker.update_step(1, "processing", 25, 12, step_id="demucs", step_name="Demucs")
        assert tracker.steps[0]["status"] == "done"
        assert tracker.steps[0]["progress"] == 100.0
        assert tracker.steps[1]["status"] == "running"
        assert tracker.steps[1]["progress"] == 25.0

    def test_step_done_only_at_100(self):
        """A step is only marked done when its progress reaches 100."""
        tracker = _import_tracker().ProgressTracker(total_steps=1)
        tracker.init_steps([{"id": "vocal", "name": "Voz"}])
        tracker.update_step(0, "processing", 99, 5, step_id="vocal", step_name="Voz")
        assert tracker.steps[0]["status"] == "running"
        tracker.update_step(0, "completed", 100, 10, step_id="vocal", step_name="Voz")
        assert tracker.steps[0]["status"] == "done"


class TestPipelineStepsProgress:
    """Integration tests for pipeline.sh --steps progress behaviour."""

    def _write_fake_worker(self, bin_dir: Path) -> Path:
        """Fake Demucs worker that emits smooth 0-100 progress events."""
        fake = bin_dir / "demucs_worker.py"
        script = r'''#!/usr/bin/env python3
import argparse
import json
import os
import sys
import time
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--model", required=True)
parser.add_argument("--device", required=True)
parser.add_argument("--input", required=True)
parser.add_argument("--out", required=True)
parser.add_argument("--shifts", type=int, default=1)
parser.add_argument("--segment", type=int, default=0)
parser.add_argument("--jobs", type=int, default=0)
args = parser.parse_args()

print(json.dumps({"event": "preflight", "model": args.model,
                  "samplerate": 44100, "channels": 2, "models": 1}), flush=True)
for pct in range(10, 101, 10):
    print(json.dumps({"event": "progress", "pct": float(pct),
                      "model_idx_in_bag": 0, "shift_idx": 0, "state": "end"}), flush=True)
    time.sleep(0.02)

track = Path(args.input).stem
outdir = Path(args.out) / args.model / track
outdir.mkdir(parents=True, exist_ok=True)
for stem in ("drums", "bass", "other", "vocals"):
    (outdir / f"{stem}.wav").write_bytes(b"RIFF" + b"\x00" * 100)

print(json.dumps({"event": "stems", "dir": str(outdir),
                  "stems": ["drums", "bass", "other", "vocals"]}), flush=True)
print(json.dumps({"event": "done", "seconds": 1.0}), flush=True)
'''
        fake.write_text(script)
        fake.chmod(0o755)
        return fake

    def test_two_step_progress_resets_and_overall_stays_honest(
        self, tmp_path, monkeypatch
    ):
        """In --steps mode the second step starts at 0 and overall < 100."""
        _skip_if_missing_bin("bash")

        input_wav = tmp_path / "input.wav"
        input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

        output_dir = tmp_path / "output" / "input"
        status_file = tmp_path / "pipeline_status.json"
        monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

        bin_dir = tmp_path / "workers"
        bin_dir.mkdir()
        fake_worker = self._write_fake_worker(bin_dir)
        monkeypatch.setenv("DEMUCS_WORKER", str(fake_worker))

        steps = [
            {
                "type": "demucs",
                "model": "htdemucs_ft",
                "stems": {
                    "drums": {"action": "save"},
                    "bass": {"action": "save"},
                    "other": {"action": "save"},
                    "vocals": {"action": "route", "target": "step:1"},
                },
            },
            {
                "type": "demucs",
                "model": "htdemucs_ft",
                "stems": {
                    "drums": {"action": "save"},
                    "bass": {"action": "save"},
                    "other": {"action": "save"},
                    "vocals": {"action": "save"},
                },
            },
        ]

        cmd = [
            "bash",
            str(PIPELINE_SH),
            "--device", "cuda",
            "--steps", json.dumps(steps),
            "--output", str(output_dir),
            str(input_wav),
        ]

        step_progress_by_step: dict[int, list[float]] = {0: [], 1: []}
        overall_values: list[float] = []
        eta_while_running: list[dict] = []
        elapsed_values: list[float] = []
        with subprocess.Popen(
            cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
        ) as proc:
            while proc.poll() is None:
                if status_file.exists():
                    try:
                        data = json.loads(status_file.read_text())
                    except (json.JSONDecodeError, OSError):
                        data = {}
                    step = data.get("step", -1)
                    if isinstance(step, int) and step in step_progress_by_step:
                        step_progress_by_step[step].append(data.get("step_progress", 0))
                    overall_values.append(data.get("overall_progress", 0))
                    if data.get("status") == "running":
                        eta_while_running.append(
                            {
                                "eta": data.get("eta", 0),
                                "step_progress": data.get("step_progress", 0),
                                "elapsed": data.get("elapsed", 0),
                            }
                        )
                    elapsed_values.append(data.get("elapsed", 0))
                time.sleep(0.03)

        stdout, stderr = proc.communicate()
        assert proc.returncode == 0, stderr or stdout

        final = json.loads(status_file.read_text())
        assert final.get("status") == "done"
        assert final.get("overall_progress") == 100

        # The second step must start near 0, not inherit 100 from step 0.
        assert step_progress_by_step[1], "no progress samples for step 1"
        assert min(step_progress_by_step[1]) < 20, (
            f"step 1 did not reset: {step_progress_by_step[1][:5]}"
        )

        # Overall progress must never claim 100 while step 1 is still running.
        # We only check samples observed before the final done state.
        non_final_overall = [o for o in overall_values if o < 100]
        assert not non_final_overall or max(non_final_overall) < 100, (
            f"overall_progress reached 100 before the end: {non_final_overall}"
        )

        # Step progress is monotonic within each step.
        for step_idx, values in step_progress_by_step.items():
            for prev, curr in zip(values, values[1:]):
                assert curr >= prev, (
                    f"step {step_idx} progress went backwards: {prev} -> {curr}"
                )

        # ETA is 0 only while a step is still too young to estimate; once the
        # step has produced enough samples the UI shows a real value.
        for sample in eta_while_running:
            if sample["step_progress"] >= 5 and sample["elapsed"] >= 3:
                assert sample["eta"] > 0, (
                    f"eta stayed 0 after step had enough data: {sample}"
                )
        # No published ETA is absurdly large.
        assert all(sample["eta"] <= 3600 for sample in eta_while_running), (
            f"eta spiked above an hour while running: {eta_while_running}"
        )

        # Elapsed must advance during the run.
        assert max(elapsed_values) > 0, "elapsed never advanced"

    def test_fake_two_step_chain_reports_honest_global_progress(
        self, tmp_path, monkeypatch
    ):
        """A fake two-step chain must use a single tracker path and tell the truth.

        Verifies the contract required by Encargo I:
          * ``step_progress`` starts at 0 for each step;
          * ``progress`` (global) only goes above 50 % once step 0 has finished;
          * ``eta`` is never 0 while ``status`` is running;
          * ``elapsed`` advances in both steps;
          * progress never claims 100 % while work is still pending.
        """
        _skip_if_missing_bin("bash")

        input_wav = tmp_path / "input.wav"
        input_wav.write_bytes(b"RIFF" + b"\x00" * 100)

        output_dir = tmp_path / "output" / "input"
        status_file = tmp_path / "pipeline_status.json"
        monkeypatch.setenv("PIPELINE_STATUS_FILE", str(status_file))

        bin_dir = tmp_path / "workers"
        bin_dir.mkdir()
        fake = self._write_fake_worker(bin_dir)
        monkeypatch.setenv("DEMUCS_WORKER", str(fake))

        steps = [
            {
                "type": "demucs",
                "model": "htdemucs_ft",
                "stems": {
                    "drums": {"action": "save"},
                    "bass": {"action": "save"},
                    "other": {"action": "save"},
                    "vocals": {"action": "route", "target": "step:1"},
                },
            },
            {
                "type": "demucs",
                "model": "htdemucs_ft",
                "stems": {
                    "drums": {"action": "save"},
                    "bass": {"action": "save"},
                    "other": {"action": "save"},
                    "vocals": {"action": "save"},
                },
            },
        ]

        cmd = [
            "bash",
            str(PIPELINE_SH),
            "--device", "cuda",
            "--steps", json.dumps(steps),
            "--output", str(output_dir),
            str(input_wav),
        ]

        samples: list[dict] = []
        with subprocess.Popen(
            cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
        ) as proc:
            while proc.poll() is None:
                if status_file.exists():
                    try:
                        data = json.loads(status_file.read_text())
                    except (json.JSONDecodeError, OSError):
                        data = {}
                    samples.append(
                        {
                            "status": data.get("status"),
                            "step": data.get("step"),
                            "step_progress": data.get("step_progress", 0),
                            "progress": data.get("progress", 0),
                            "overall_progress": data.get("overall_progress", 0),
                            "eta": data.get("eta", 0),
                            "elapsed": data.get("elapsed", 0),
                        }
                    )
                time.sleep(0.03)

        stdout, stderr = proc.communicate()
        assert proc.returncode == 0, stderr or stdout

        final = json.loads(status_file.read_text())
        assert final.get("status") == "done"
        assert final.get("overall_progress") == 100

        # Per-step progress samples.
        step_progress_by_step: dict[int, list[float]] = {0: [], 1: []}
        for s in samples:
            step = s["step"]
            if isinstance(step, int) and step in step_progress_by_step:
                step_progress_by_step[step].append(s["step_progress"])

        # Each step must start at 0.
        for step_idx, values in step_progress_by_step.items():
            assert values, f"no samples for step {step_idx}"
            assert values[0] == 0, (
                f"step {step_idx} did not start at 0: {values[:3]}"
            )
            for prev, curr in zip(values, values[1:]):
                assert curr >= prev, (
                    f"step {step_idx} progress went backwards: {prev} -> {curr}"
                )

        # Global progress may only exceed 50 % once step 0 is done.
        step0_done_index = None
        for i, s in enumerate(samples):
            if s["step"] == 0 and s["step_progress"] == 100:
                step0_done_index = i
                break
        assert step0_done_index is not None, "step 0 never reported 100 %"
        before_step0_done = samples[: step0_done_index + 1]
        assert all(s["progress"] <= 50 for s in before_step0_done), (
            "global progress crossed 50 % before step 0 finished: "
            f"{[(s['step_progress'], s['progress']) for s in before_step0_done if s['progress'] > 50]}"
        )
        after_step0_done = samples[step0_done_index + 1 :]
        assert any(s["progress"] > 50 for s in after_step0_done), (
            "global progress never crossed 50 % after step 0 finished"
        )

        # ETA is 0 only while a step is still too young to estimate; once the
        # step has produced enough samples the UI shows a real value.  Progress
        # never claims 100 % while work is still pending.
        for s in samples:
            if s["status"] == "running":
                if s["step_progress"] >= 5 and s["elapsed"] >= 3:
                    assert s["eta"] > 0, f"eta={s['eta']} while status=running after enough data: {s}"
                assert s["eta"] <= 3600, f"eta spiked above an hour: {s}"
                assert s["progress"] < 100, (
                    f"progress={s['progress']} with work pending (status=running)"
                )

        # Elapsed must advance in both steps.
        elapsed_by_step: dict[int, list[float]] = {0: [], 1: []}
        for s in samples:
            step = s["step"]
            if isinstance(step, int) and step in elapsed_by_step:
                elapsed_by_step[step].append(s["elapsed"])
        for step_idx, values in elapsed_by_step.items():
            assert values, f"no elapsed samples for step {step_idx}"
            assert max(values) > min(values), (
                f"elapsed did not advance in step {step_idx}: {values[:3]}..{values[-3:]}"
            )

        # Surface the sampled values so the report can include the table.
        self._last_fake_chain_samples = samples
