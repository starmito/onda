#!/usr/bin/env python3
"""Progress tracking helper for the Onda pipeline.

Provides a small, stateful tracker that estimates ETA from recent progress
samples instead of averaging from t=0, writes pipeline_status.json atomically,
and computes a weighted overall_progress for chained steps.

State is persisted next to the status file so the tracker can be invoked
repeatedly from shell scripts without losing its history.
"""

import json
import os
import sys
import time
from pathlib import Path


class ProgressTracker:
    """Track progress samples and compute honest ETA / overall progress.

    Unit convention: every public progress value is expressed in the 0-100
    (percentage) range.  ``progress``, ``overall_progress`` and
    ``step_progress`` are always percentages.  Callers that still hold a
    0-1 fraction must multiply by 100 before calling any update method.

    ETA is derived from the rate observed over the most recent samples, then
    smoothed with an exponential moving average.  If progress stalls the ETA is
    frozen at the last non-zero estimate instead of dropping to zero.
    """

    def __init__(
        self,
        total_steps: int = 1,
        window_seconds: float = 30.0,
        min_window_seconds: float = 3.0,
        max_samples: int = 50,
        smooth_alpha: float = 0.3,
        min_samples_for_eta: int = 3,
        min_seconds_for_eta: float = 3.0,
        min_progress_for_eta: float = 2.0,
        max_total_multiplier: float = 10.0,
        eta_spike_ratio: float = 3.0,
        min_eta_seconds: float = 1.0,
    ):
        self.total_steps = max(int(total_steps), 1)
        self.window_seconds = float(window_seconds)
        self.min_window_seconds = float(min_window_seconds)
        self.max_samples = int(max_samples)
        self.smooth_alpha = float(smooth_alpha)

        # Honest ETA guardrails.  We do not publish an ETA until we have enough
        # real samples, enough elapsed time and enough progress to estimate a
        # rate that is not just noise.  Once we do, we clamp the estimate so it
        # never implies a total duration wildly larger than the time already
        # spent, and we reject sudden spikes that would make the UI jump from
        # seconds to hours.
        self.min_samples_for_eta = max(int(min_samples_for_eta), 2)
        self.min_seconds_for_eta = float(min_seconds_for_eta)
        self.min_progress_for_eta = float(min_progress_for_eta)
        self.max_total_multiplier = float(max_total_multiplier)
        self.eta_spike_ratio = float(eta_spike_ratio)
        self.min_eta_seconds = float(min_eta_seconds)

        # (elapsed_seconds, progress_0_100); seed with t=0 so the very first
        # update can already produce a global-average ETA estimate.
        self.samples: list[tuple[float, float]] = [(0.0, 0.0)]
        self.last_eta: float | None = None
        self.current_step_idx: int | None = None
        self.step_progress: dict[int, float] = {}
        self.step_status: dict[int, str] = {}
        # Highest overall_progress ever published; guards against backwards
        # jumps when a new step starts or a spurious write occurs.
        self.max_overall_progress: float = 0.0
        # Per-step metadata for the UI: id, name, status, progress, eta, elapsed.
        # Persisted in tracker state and published in pipeline_status.json.
        self.steps: list[dict] = []

    def _clamp(self, value: float, lo: float = 0.0, hi: float = 100.0) -> float:
        return max(lo, min(hi, value))

    def _rate_from_window(self, elapsed: float, progress: float) -> float | None:
        """Return progress rate (% per second) using recent samples."""
        self.samples.append((float(elapsed), float(progress)))
        cutoff = elapsed - self.window_seconds
        # Drop samples outside the window, but always keep at least the first one
        # so we can fall back to the global average if the recent window is tiny.
        while len(self.samples) > 1 and self.samples[1][0] < cutoff:
            self.samples.pop(0)
        while len(self.samples) > self.max_samples:
            self.samples.pop(0)

        # Try the recent window first.
        window = [s for s in self.samples if s[0] >= elapsed - self.window_seconds]
        rate = self._rate(window)
        if rate is not None:
            return rate

        # Fall back to the whole history if we do not yet have enough data.
        return self._rate(self.samples)

    def _rate(self, samples: list[tuple[float, float]]) -> float | None:
        if len(samples) < 2:
            return None
        first_elapsed, first_progress = samples[0]
        last_elapsed, last_progress = samples[-1]
        delta_elapsed = last_elapsed - first_elapsed
        delta_progress = last_progress - first_progress
        if delta_elapsed < self.min_window_seconds or delta_progress <= 0:
            return None
        return delta_progress / delta_elapsed

    def update(
        self,
        elapsed: float,
        progress: float,
        finished: bool = False,
    ) -> dict[str, float]:
        """Compute ETA and enforce monotonic step progress.

        ``progress`` is expected in the 0-100 range.  The returned dict contains
        ``eta`` (seconds), ``progress`` (clamped and non-decreasing for the
        current step) and ``overall_progress`` (weighted when total_steps > 1).
        """
        if finished:
            progress = 100.0
        progress = self._clamp(progress)
        # Step progress cannot go backwards within a step.  When the current
        # step has no recorded baseline yet (new step) we accept the reported
        # value, which lets it restart from 0.
        baseline = self.step_progress.get(self.current_step_idx, 0.0) if self.current_step_idx is not None else 0.0
        progress = max(progress, baseline)
        if self.current_step_idx is not None:
            self.step_progress[self.current_step_idx] = progress

        eta = self._compute_eta(elapsed, progress, finished)
        overall = self._overall_progress(progress)
        # Belt: the published global progress can never decrease while work is
        # still running. Real fixes live in the caller order, but this guard
        # protects downstream UI/backend code from transient backwards jumps.
        if finished:
            overall = 100.0
            self.max_overall_progress = 100.0
        else:
            overall = max(overall, self.max_overall_progress)
            self.max_overall_progress = max(self.max_overall_progress, overall)
        return {
            "eta": round(eta),
            "progress": round(progress, 4),
            "overall_progress": round(overall, 4),
        }

    def _real_sample_count(self) -> int:
        """Number of real progress samples, excluding the t=0 seed."""
        return max(0, len(self.samples) - 1)

    def _have_enough_data_for_eta(self, elapsed: float, progress: float) -> bool:
        """True when the sample window is wide enough to trust a rate estimate."""
        if self._real_sample_count() < self.min_samples_for_eta:
            return False
        if elapsed < self.min_seconds_for_eta:
            return False
        if progress < self.min_progress_for_eta:
            return False
        return True

    def _compute_eta(self, elapsed: float, progress: float, finished: bool) -> float:
        """Return an ETA in seconds; 0 means "not enough data yet".

        The UI renders ``eta: 0`` as "--" / "calculating..." so we never emit
        a made-up number while the sample window is still too small.  Once a
        real estimate exists we return it as-is; the configured floor is no
        longer published as a fake "1 s" estimate.
        """
        if finished:
            self.last_eta = 0.0
            return 0.0

        if progress >= 100.0:
            # Step reports 100% but the pipeline has not declared completion yet.
            # Do not invent a floor value; the UI hides eta=0 as "calculating...".
            return 0.0

        rate = self._rate_from_window(elapsed, progress)
        if rate is None or rate <= 0:
            # Not enough data or progress stalled.  Keep the previous ETA if we
            # ever had a sane one; otherwise publish 0 (calculating...).
            return self.last_eta if self.last_eta is not None else 0.0

        if not self._have_enough_data_for_eta(elapsed, progress):
            # Window too small: do not guess.  Hold any previous estimate but
            # keep returning 0 until we are confident.
            return self.last_eta if self.last_eta is not None else 0.0

        new_eta = (100.0 - progress) / rate

        # Sanity cap: the total estimated duration cannot be orders of magnitude
        # larger than the time already invested.  This prevents the "77 min"
        # spikes caused by a temporary stall in the recent window.
        max_sane_eta = elapsed * self.max_total_multiplier
        new_eta = min(new_eta, max_sane_eta)

        # Spike guard: a sudden jump upward without a matching rate drop is
        # almost always noise from a bursty progress report.  Keep the last
        # published estimate until the new one stabilises.  Drops are allowed
        # immediately so the ETA can converge downward.
        if self.last_eta is not None and new_eta > self.last_eta * self.eta_spike_ratio:
            new_eta = self.last_eta

        if self.last_eta is None:
            self.last_eta = new_eta
        else:
            self.last_eta = (
                self.smooth_alpha * new_eta
                + (1.0 - self.smooth_alpha) * self.last_eta
            )

        # Do not publish the floor as an estimate.  A value below 1 s rounds to
        # 0 and the UI hides it, which is honest for very short remaining work.
        return self.last_eta

    def _overall_progress(self, current_progress: float) -> float:
        """Weighted overall: completed steps count fully, current step partial.

        The current step is *not* counted twice: its contribution is
        ``current_progress`` while previous completed steps contribute 100%.
        Until every step is completed the value is clamped strictly below 100.
        """
        if self.total_steps <= 1:
            return 100.0 if current_progress >= 100.0 else current_progress

        completed_before = sum(
            1
            for idx, status in self.step_status.items()
            if status in ("completed", "done") and idx != self.current_step_idx
        )
        total = completed_before * 100.0 + current_progress
        overall = total / self.total_steps

        all_done = all(
            status in ("completed", "done")
            for status in self.step_status.values()
        ) and len(self.step_status) >= self.total_steps

        if all_done:
            return 100.0
        # Clamp strictly below 100 until every step is done.
        return min(overall, 99.99)

    def set_step_status(self, step_idx: int, status: str, progress: float | None = None):
        """Record per-step status and optional progress for multi-step mode."""
        self.step_status[step_idx] = status
        if progress is not None:
            self.step_progress[step_idx] = self._clamp(progress)

    def _public_step_status(self, status: str, progress: float) -> str:
        """Map internal tracker status to the public step status vocabulary."""
        if status == "failed":
            return "failed"
        if status in ("completed", "done") and progress >= 100.0:
            return "done"
        if status in ("processing", "running", "completed"):
            return "running"
        return "queued"

    def init_steps(self, steps: list[dict]) -> None:
        """Initialize the full list of steps with stable ids and readable names.

        When the tracker already knows the state of a step (for example because
        a previous per-step invocation completed it), the published entry is
        preserved so the UI does not momentarily show a completed step as
        queued again.
        """
        old_steps = self.steps
        self.steps = []
        for i, s in enumerate(steps):
            entry = {
                "id": s.get("id", ""),
                "name": s.get("name", ""),
                "status": "queued",
                "progress": 0,
                "eta": 0,
                "elapsed": 0,
            }
            if i < len(old_steps) and old_steps[i].get("status") not in (
                None,
                "",
                "queued",
            ):
                prev = old_steps[i]
                entry["status"] = prev["status"]
                entry["progress"] = prev["progress"]
                entry["eta"] = prev.get("eta", 0)
                entry["elapsed"] = prev.get("elapsed", 0)
            self.steps.append(entry)

    def update_step_list(
        self,
        step_idx: int,
        status: str,
        progress: float,
        eta: float,
        elapsed: float,
        step_id: str | None = None,
        step_name: str | None = None,
    ) -> None:
        """Update the per-step entry that is published in pipeline_status.json."""
        # Ensure the list is long enough; lazily grow it for callers that never
        # call init_steps.
        while len(self.steps) <= step_idx:
            self.steps.append({
                "id": step_id or "",
                "name": step_name or "",
                "status": "queued",
                "progress": 0,
                "eta": 0,
                "elapsed": 0,
            })

        entry = self.steps[step_idx]
        if step_id is not None:
            entry["id"] = step_id
        if step_name is not None:
            entry["name"] = step_name
        entry["status"] = self._public_step_status(status, progress)
        entry["progress"] = round(self._clamp(progress), 4)
        entry["eta"] = round(eta)
        entry["elapsed"] = round(elapsed)

    def update_step(
        self,
        step_idx: int,
        status: str,
        progress: float,
        elapsed: float,
        step_id: str | None = None,
        step_name: str | None = None,
    ) -> dict[str, float]:
        """Update a single step in multi-step mode and return ETA/overall.

        The returned ``progress`` is the *step* progress; ``overall_progress``
        is the weighted global value.
        """
        # When the step changes, reset the sample window and ETA baseline so the
        # new step starts from an honest 0 and does not drag the previous step's
        # rate into its estimate.
        if self.current_step_idx != step_idx:
            self.current_step_idx = step_idx
            self.samples = [(0.0, 0.0)]
            self.last_eta = None

        # Enforce monotonic progress within the same step.  A step that was
        # completed previously keeps its 100%.
        prev = self.step_progress.get(step_idx)
        if prev is not None and status == "processing":
            progress = max(progress, prev)

        # Capture the previous status *before* overwriting it so the safety
        # belt below can detect a premature "done" write.
        prev_status = self.step_status.get(step_idx)
        self.set_step_status(step_idx, status, progress)

        # ``completed`` means this step is done but the pipeline may continue,
        # so ETA must not collapse to 0 until the whole job is ``done``.
        # ``done`` always means 100 % for this step.
        finished = status == "done"

        # Safety belt: a step cannot be marked done before it has ever been
        # reported as running/processing. The root cause (pipeline.sh remapping
        # the final "done" write to the last step in intermediate backend
        # invocations) is fixed there; this guard protects the UI from any
        # other stray premature "done" write.
        if finished and prev_status in (None, "", "queued", "waiting"):
            finished = False
            status = "processing"
            progress = max(progress, self.step_progress.get(step_idx, 0.0))
            self.set_step_status(step_idx, status, progress)

        if finished:
            progress = 100.0
        result = self.update(elapsed, progress, finished=finished)
        self.update_step_list(
            step_idx, status, result["progress"], result["eta"], elapsed,
            step_id=step_id, step_name=step_name,
        )
        return result

    def tick(self, elapsed: float) -> dict[str, float]:
        """Refresh ETA/elapsed without changing the reported progress.

        Uses the last known step progress so the background elapsed updater
        does not publish a fake progress value.
        """
        last_progress = (
            self.step_progress.get(self.current_step_idx, 0.0)
            if self.current_step_idx is not None
            else 0.0
        )
        return self.update(elapsed, last_progress, finished=False)

    def reset_step_progress(self):
        """Reset the per-call progress baseline so the next step starts at 0."""
        self.samples = [(0.0, 0.0)]
        self.last_eta = None
        if self.current_step_idx is not None:
            self.step_progress.pop(self.current_step_idx, None)

    def to_dict(self) -> dict:
        return {
            "total_steps": self.total_steps,
            "samples": self.samples,
            "last_eta": self.last_eta,
            "current_step_idx": self.current_step_idx,
            "step_progress": self.step_progress,
            "step_status": self.step_status,
            "steps": self.steps,
            "window_seconds": self.window_seconds,
            "min_window_seconds": self.min_window_seconds,
            "max_samples": self.max_samples,
            "smooth_alpha": self.smooth_alpha,
            "min_samples_for_eta": self.min_samples_for_eta,
            "min_seconds_for_eta": self.min_seconds_for_eta,
            "min_progress_for_eta": self.min_progress_for_eta,
            "max_total_multiplier": self.max_total_multiplier,
            "eta_spike_ratio": self.eta_spike_ratio,
            "min_eta_seconds": self.min_eta_seconds,
            "max_overall_progress": self.max_overall_progress,
        }

    @classmethod
    def from_dict(cls, data: dict) -> "ProgressTracker":
        tracker = cls(
            total_steps=data.get("total_steps", 1),
            window_seconds=data.get("window_seconds", 30.0),
            min_window_seconds=data.get("min_window_seconds", 3.0),
            max_samples=data.get("max_samples", 50),
            smooth_alpha=data.get("smooth_alpha", 0.3),
            min_samples_for_eta=data.get("min_samples_for_eta", 4),
            min_seconds_for_eta=data.get("min_seconds_for_eta", 5.0),
            min_progress_for_eta=data.get("min_progress_for_eta", 3.0),
            max_total_multiplier=data.get("max_total_multiplier", 10.0),
            eta_spike_ratio=data.get("eta_spike_ratio", 3.0),
            min_eta_seconds=data.get("min_eta_seconds", 1.0),
        )
        tracker.samples = data.get("samples", [(0.0, 0.0)])
        tracker.last_eta = data.get("last_eta")
        tracker.current_step_idx = data.get("current_step_idx")
        tracker.step_progress = {int(k): v for k, v in data.get("step_progress", {}).items()}
        tracker.step_status = {int(k): v for k, v in data.get("step_status", {}).items()}
        tracker.steps = data.get("steps", [])
        tracker.max_overall_progress = data.get("max_overall_progress", 0.0)
        return tracker


def tracker_state_path(status_file: str | Path) -> Path:
    return Path(str(status_file) + ".tracker.json")


def load_tracker(status_file: str | Path, total_steps: int = 1) -> ProgressTracker:
    state_path = tracker_state_path(status_file)
    if state_path.exists():
        try:
            with open(state_path) as f:
                data = json.load(f)
            tracker = ProgressTracker.from_dict(data)
            # Allow the caller to update total_steps when step config is known.
            if total_steps != tracker.total_steps:
                tracker.total_steps = max(int(total_steps), 1)
            return tracker
        except Exception:
            pass
    return ProgressTracker(total_steps=total_steps)


def save_tracker(status_file: str | Path, tracker: ProgressTracker) -> None:
    state_path = tracker_state_path(status_file)
    tmp = state_path.with_suffix(state_path.suffix + ".tmp")
    with open(tmp, "w") as f:
        json.dump(tracker.to_dict(), f)
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, state_path)


def write_status_atomic(path: str | Path, data: dict) -> None:
    """Write ``data`` to ``path`` atomically (temp file + rename)."""
    path = Path(path)
    tmp = path.with_suffix(path.suffix + ".tmp")
    with open(tmp, "w") as f:
        json.dump(data, f)
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)


def load_or_init(path: str | Path) -> dict:
    """Load an existing status file or return an empty dict."""
    path = Path(path)
    if not path.exists():
        return {}
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return {}


def update_status(
    status_file: str | Path,
    elapsed: float,
    progress: float,
    finished: bool = False,
    extra: dict | None = None,
) -> dict:
    """Update a legacy-mode status file and persist tracker state."""
    status_file = Path(status_file)
    tracker = load_tracker(status_file)
    data = load_or_init(status_file)
    elapsed = max(float(elapsed), float(data.get("elapsed", 0.0)))
    result = tracker.update(elapsed, progress, finished=finished)
    data.update(result)
    if extra:
        data.update(extra)
    write_status_atomic(status_file, data)
    save_tracker(status_file, tracker)
    return data


def load_steps_state(steps_state_file: str | Path) -> list[dict]:
    try:
        with open(steps_state_file) as f:
            return json.load(f).get("steps", [])
    except Exception:
        return []


def init_steps_status(
    status_file: str | Path,
    steps: list[dict],
    total_steps: int,
) -> dict:
    """Initialize the full step list in tracker state and status file.

    Each step must provide at least ``id`` and ``name``; the tracker fills the
    runtime fields (status, progress, eta, elapsed) with queued/0 values.
    """
    status_file = Path(status_file)
    tracker = load_tracker(status_file, total_steps=total_steps)
    tracker.init_steps(steps)
    data = load_or_init(status_file)
    data["steps"] = tracker.steps
    data["total_steps"] = total_steps
    write_status_atomic(status_file, data)
    save_tracker(status_file, tracker)
    return data


def update_step_status(
    status_file: str | Path,
    step_idx: int,
    status: str,
    progress: float,
    elapsed: float,
    total_steps: int,
    extra: dict | None = None,
    steps_state_file: str | Path | None = None,
    step_name: str | None = None,
    step_id: str | None = None,
    step_display_name: str | None = None,
) -> dict:
    """Update a multi-step status file and persist tracker state.

    The status file receives:
      * ``progress`` / ``overall_progress`` = weighted global progress (0-100)
      * ``step_progress``                 = progress of the current step (0-100)
      * ``step``                          = human step name when provided,
                                            step index otherwise
      * ``step_idx``                      = numeric step index
      * ``eta`` / ``elapsed``             = honest global ETA and elapsed seconds
      * ``steps``                         = list of all steps with per-step status
    """
    status_file = Path(status_file)
    tracker = load_tracker(status_file, total_steps=total_steps)
    data = load_or_init(status_file)
    # ``elapsed`` is a global stopwatch: it must never tick backwards.
    elapsed = max(float(elapsed), float(data.get("elapsed", 0.0)))
    effective_step_id = step_id if step_id is not None else step_name
    effective_step_name = step_display_name if step_display_name is not None else (step_id if step_id is not None else step_name)
    result = tracker.update_step(
        step_idx, status, progress, elapsed,
        step_id=effective_step_id, step_name=effective_step_name,
    )

    # ``progress`` is the global value; keep ``step_progress`` for the UI.
    data["progress"] = result["overall_progress"]
    data["overall_progress"] = result["overall_progress"]
    data["step_progress"] = result["progress"]
    data["eta"] = result["eta"]
    data["elapsed"] = elapsed
    data["step_idx"] = step_idx
    if step_name is not None:
        try:
            data["step"] = int(step_name)
        except ValueError:
            data["step"] = step_name
    else:
        data["step"] = step_idx

    # If every step is already completed we can safely report ``done``,
    # otherwise keep the pipeline ``running`` while a step is merely
    # ``completed``.
    all_done = (
        all(s in ("completed", "done") for s in tracker.step_status.values())
        and len(tracker.step_status) >= total_steps
    )
    if all_done:
        data["status"] = "done"
    elif status == "completed":
        data["status"] = "running"
    else:
        data["status"] = status

    # Publish the per-step list.  If the tracker already knows the full list,
    # use it; otherwise fall back to the legacy steps_state_file once.
    if tracker.steps:
        data["steps"] = tracker.steps
    elif steps_state_file:
        data["steps"] = load_steps_state(steps_state_file)
    if extra:
        data.update(extra)
    write_status_atomic(status_file, data)
    save_tracker(status_file, tracker)
    return data


def tick_status(status_file: str | Path, elapsed: float) -> dict:
    """Refresh ``elapsed`` and ``eta`` without touching progress values."""
    status_file = Path(status_file)
    tracker = load_tracker(status_file)
    data = load_or_init(status_file)
    elapsed = max(float(elapsed), float(data.get("elapsed", 0.0)))
    result = tracker.tick(elapsed)
    data["eta"] = result["eta"]
    data["elapsed"] = elapsed
    write_status_atomic(status_file, data)
    save_tracker(status_file, tracker)
    return data


def reset_step(status_file: str | Path) -> None:
    """Reset the current-step baseline so the next step reports from 0."""
    status_file = Path(status_file)
    tracker = load_tracker(status_file)
    tracker.reset_step_progress()
    save_tracker(status_file, tracker)


def main():
    """CLI used by pipeline.sh to update status files.

    Usage:
        python3 tools/progress_tracker.py update <status_file> <elapsed> <progress> [finished]
        python3 tools/progress_tracker.py init-steps <status_file> <steps_json> <total_steps>
        python3 tools/progress_tracker.py update-step <status_file> <step_idx> <status> <progress> <elapsed> <total_steps> [step_name] [step_id] [step_display_name] [steps_state_file]
        python3 tools/progress_tracker.py tick <status_file> <elapsed>
        python3 tools/progress_tracker.py reset-step <status_file>
        python3 tools/progress_tracker.py write <status_file> <json_data>
    """
    if len(sys.argv) < 2:
        sys.exit(0)
    cmd = sys.argv[1]

    if cmd == "update" and len(sys.argv) >= 5:
        status_file = sys.argv[2]
        elapsed = float(sys.argv[3])
        progress = float(sys.argv[4])
        finished = (sys.argv[5] if len(sys.argv) > 5 else "false").lower() in (
            "1",
            "true",
            "yes",
        )
        update_status(status_file, elapsed, progress, finished=finished)

    elif cmd == "init-steps" and len(sys.argv) >= 5:
        status_file = sys.argv[2]
        steps_json = sys.argv[3]
        total_steps = int(sys.argv[4])
        try:
            steps = json.loads(steps_json)
        except Exception:
            sys.exit(1)
        init_steps_status(status_file, steps, total_steps)

    elif cmd == "update-step" and len(sys.argv) >= 8:
        status_file = sys.argv[2]
        step_idx = int(sys.argv[3])
        status = sys.argv[4]
        progress = float(sys.argv[5])
        elapsed = float(sys.argv[6])
        total_steps = int(sys.argv[7])
        step_name = sys.argv[8] if len(sys.argv) > 8 else None
        step_id = sys.argv[9] if len(sys.argv) > 9 else None
        step_display_name = sys.argv[10] if len(sys.argv) > 10 else None
        steps_state_file = sys.argv[11] if len(sys.argv) > 11 else None
        update_step_status(
            status_file,
            step_idx,
            status,
            progress,
            elapsed,
            total_steps,
            step_name=step_name,
            step_id=step_id,
            step_display_name=step_display_name,
            steps_state_file=steps_state_file,
        )

    elif cmd == "tick" and len(sys.argv) >= 4:
        tick_status(sys.argv[2], float(sys.argv[3]))

    elif cmd == "reset-step" and len(sys.argv) >= 3:
        reset_step(sys.argv[2])

    elif cmd == "write" and len(sys.argv) >= 4:
        status_file = sys.argv[2]
        try:
            data = json.loads(sys.argv[3])
        except Exception:
            sys.exit(1)
        write_status_atomic(status_file, data)


if __name__ == "__main__":
    main()
