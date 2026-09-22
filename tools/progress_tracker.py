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
    ):
        self.total_steps = max(int(total_steps), 1)
        self.window_seconds = float(window_seconds)
        self.min_window_seconds = float(min_window_seconds)
        self.max_samples = int(max_samples)
        self.smooth_alpha = float(smooth_alpha)

        # (elapsed_seconds, progress_0_100); seed with t=0 so the very first
        # update can already produce a global-average ETA estimate.
        self.samples: list[tuple[float, float]] = [(0.0, 0.0)]
        self.last_eta: float | None = None
        self.current_step_idx: int | None = None
        self.step_progress: dict[int, float] = {}
        self.step_status: dict[int, str] = {}

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
        return {
            "eta": round(eta),
            "progress": round(progress, 4),
            "overall_progress": round(overall, 4),
        }

    def _compute_eta(self, elapsed: float, progress: float, finished: bool) -> float:
        """Return an ETA in seconds; never 0 while work is unfinished."""
        eta: float = 0.0
        if finished:
            self.last_eta = 0.0
            return 0.0

        if progress >= 100.0:
            # Step reports 100% but the pipeline has not declared completion yet;
            # keep a small, honest ETA instead of claiming zero while work may
            # still continue.
            eta = self.last_eta if self.last_eta is not None else 1.0
            eta = max(eta, 1.0)
        else:
            rate = self._rate_from_window(elapsed, progress)
            if rate is not None and rate > 0:
                new_eta = (100.0 - progress) / rate
                if self.last_eta is None:
                    self.last_eta = new_eta
                else:
                    self.last_eta = (
                        self.smooth_alpha * new_eta
                        + (1.0 - self.smooth_alpha) * self.last_eta
                    )
            # If rate is zero / unavailable, keep the previous ETA so it does
            # not collapse to 0 during a stall.
            eta = self.last_eta if self.last_eta is not None else 0.0

        # Never publish eta: 0 while work is unfinished.
        if eta < 1.0:
            eta = 1.0
        return eta

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

    def update_step(
        self,
        step_idx: int,
        status: str,
        progress: float,
        elapsed: float,
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
        self.set_step_status(step_idx, status, progress)

        # ``completed`` means this step is done but the pipeline may continue,
        # so ETA must not collapse to 0 until the whole job is ``done``.
        finished = status == "done"
        return self.update(elapsed, progress, finished=finished)

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
        }

    @classmethod
    def from_dict(cls, data: dict) -> "ProgressTracker":
        tracker = cls(
            total_steps=data.get("total_steps", 1),
            window_seconds=data.get("window_seconds", 30.0),
            min_window_seconds=data.get("min_window_seconds", 3.0),
            max_samples=data.get("max_samples", 50),
            smooth_alpha=data.get("smooth_alpha", 0.3),
        )
        tracker.samples = data.get("samples", [(0.0, 0.0)])
        tracker.last_eta = data.get("last_eta")
        tracker.current_step_idx = data.get("current_step_idx")
        tracker.step_progress = {int(k): v for k, v in data.get("step_progress", {}).items()}
        tracker.step_status = {int(k): v for k, v in data.get("step_status", {}).items()}
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
) -> dict:
    """Update a multi-step status file and persist tracker state.

    The status file receives:
      * ``progress`` / ``overall_progress`` = weighted global progress (0-100)
      * ``step_progress``                 = progress of the current step (0-100)
      * ``step``                          = human step name when provided,
                                            step index otherwise
      * ``step_idx``                      = numeric step index
      * ``eta`` / ``elapsed``             = honest global ETA and elapsed seconds
    """
    status_file = Path(status_file)
    tracker = load_tracker(status_file, total_steps=total_steps)
    data = load_or_init(status_file)
    result = tracker.update_step(step_idx, status, progress, elapsed)

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
    if steps_state_file:
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
        python3 tools/progress_tracker.py update-step <status_file> <step_idx> <status> <progress> <elapsed> <total_steps> [step_name] [steps_state_file]
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

    elif cmd == "update-step" and len(sys.argv) >= 8:
        status_file = sys.argv[2]
        step_idx = int(sys.argv[3])
        status = sys.argv[4]
        progress = float(sys.argv[5])
        elapsed = float(sys.argv[6])
        total_steps = int(sys.argv[7])
        step_name = sys.argv[8] if len(sys.argv) > 8 else None
        steps_state_file = sys.argv[9] if len(sys.argv) > 9 else None
        update_step_status(
            status_file,
            step_idx,
            status,
            progress,
            elapsed,
            total_steps,
            step_name=step_name,
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
