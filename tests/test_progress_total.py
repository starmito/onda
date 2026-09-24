"""Unit tests for the real RoFormer/MelBand progress total calculation.

These tests run in the inference environment where ``torch`` is installed;
outside of it (e.g. the host test runner without the heavy ML stack) the
whole module is skipped at collection time via ``pytest.importorskip``.
"""

import math

import pytest

pytest.importorskip("torch")

from inference_universal import _progress_total_for


# Realistic ViperX-like parameters referenced in the bug report.
VIPERX_C = 1048064
VIPERX_STEP = 131008
VIPERX_BATCH_SIZE = 3
SAMPLE_RATE = 44100


def _count_windows(num_samples, step):
    """Simulate the exact sliding-window loop from _process_mix."""
    windows = 0
    i = 0
    while i < num_samples:
        windows += 1
        i += step
    return windows


def test_progress_total_for_30s_clip_is_greater_than_one():
    """Regression: a 30 s clip must produce more than one work unit."""
    num_samples = 30 * SAMPLE_RATE
    total = _progress_total_for(num_samples, VIPERX_C, VIPERX_STEP, VIPERX_BATCH_SIZE)
    assert total > 1, f"expected > 1, got {total}"


def test_progress_total_for_matches_simulated_loop():
    """The helper must agree with a verbatim simulation of the loop."""
    num_samples = 30 * SAMPLE_RATE
    expected = _count_windows(num_samples, VIPERX_STEP)
    got = _progress_total_for(num_samples, VIPERX_C, VIPERX_STEP, VIPERX_BATCH_SIZE)
    assert got == expected, f"expected {expected} windows, got {got}"


def test_progress_total_for_never_below_one():
    """Very short audio must still report at least one unit."""
    assert _progress_total_for(1, VIPERX_C, VIPERX_STEP, VIPERX_BATCH_SIZE) == 1
    assert _progress_total_for(0, VIPERX_C, VIPERX_STEP, VIPERX_BATCH_SIZE) == 1


@pytest.mark.parametrize("num_samples,step", [
    (SAMPLE_RATE, 131008),
    (10 * SAMPLE_RATE, 131008),
    (30 * SAMPLE_RATE, 131008),
    (60 * SAMPLE_RATE, 794624),
])
def test_progress_total_for_various_lengths(num_samples, step):
    """The helper should match the simulated loop for several realistic sizes."""
    expected = _count_windows(num_samples, step)
    got = _progress_total_for(num_samples, VIPERX_C, step, VIPERX_BATCH_SIZE)
    assert got == expected


def test_batch_size_does_not_change_window_count():
    """batch_size only groups windows into flushes; the total window count is unchanged."""
    num_samples = 30 * SAMPLE_RATE
    total_1 = _progress_total_for(num_samples, VIPERX_C, VIPERX_STEP, 1)
    total_8 = _progress_total_for(num_samples, VIPERX_C, VIPERX_STEP, 8)
    assert total_1 == total_8
    assert total_1 == _count_windows(num_samples, VIPERX_STEP)


def test_count_windows_ceil_formula():
    """Sanity check that the simulated loop equals ceil(num_samples / step)."""
    for num_samples in (1, 100, 131007, 131008, 131009, 30 * SAMPLE_RATE):
        for step in (1, 2, 131008, 794624):
            expected = _count_windows(num_samples, step)
            formula = math.ceil(num_samples / step)
            assert expected == formula, (num_samples, step, expected, formula)
