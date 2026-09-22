"""Tests for the chunk assembly logic in inference_universal.py.

These tests do not need a GPU or real model: conftest.py provides lightweight
torch/librosa/soundfile mocks, and we use an identity model that returns exactly
what it receives.  The goal is to guarantee that the assembled output has the
same length as the input, especially for odd lengths, overlaps and a partial
final chunk.
"""

import importlib.util
import sys
from pathlib import Path

import numpy as np
import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
INF_PATH = REPO_ROOT / "inference_universal.py"


def _load_inference_module():
    """Load inference_universal.py as a module without executing __main__."""
    spec = importlib.util.spec_from_file_location("inference_universal", INF_PATH)
    mod = importlib.util.module_from_spec(spec)
    sys.modules["inference_universal"] = mod
    spec.loader.exec_module(mod)
    return mod


@pytest.fixture
def inf_mod():
    return _load_inference_module()


class _IdentityModel:
    """Torch-compatible mock: returns the input as stem 0, shape [B,S,C,T]."""

    def __init__(self, S=1):
        self.S = S

    def __call__(self, arr):
        # arr is a _Tensor with shape [B, channels, T]
        B, C, T = arr.shape
        out = np.zeros((B, self.S, C, T), dtype=np.float32)
        for b in range(B):
            out[b, 0] = arr[b]._data if hasattr(arr[b], "_data") else np.array(arr[b])
        # Return a torch-compatible object through the module's torch mock.
        import torch
        return torch.tensor(out)

    def to(self, *args, **kwargs):
        return self

    def eval(self):
        return self


def _to_numpy(t):
    if hasattr(t, "_data"):
        return t._data
    if hasattr(t, "numpy"):
        return t.numpy()
    return np.array(t)


def test_chunked_process_preserves_odd_length_with_overlap(inf_mod):
    """Assembly must return a tensor with the exact input length."""
    np.random.seed(42)
    total_samples = 100_001  # odd length to stress integer rounding
    audio = np.random.randn(2, total_samples).astype(np.float32)

    C = 1000
    overlap = 4
    step = C // overlap
    model = _IdentityModel(S=1)

    result = inf_mod._chunked_process(
        model=model,
        audio=audio,
        C=C,
        step=step,
        batch_size=1,
        S=1,
        device="cpu",
        chunk_seconds=2.0,
        progress_file=None,
        pipeline_status=None,
    )

    result_np = _to_numpy(result)
    assert result_np.shape[-1] == total_samples, (
        f"output length {result_np.shape[-1]} != input length {total_samples}"
    )
    # Identity model: the assembled stem 0 should be the original audio.
    np.testing.assert_allclose(result_np[0], audio, atol=1e-5)


def test_chunked_process_last_partial_chunk(inf_mod):
    """A partial final chunk must not shorten the output."""
    np.random.seed(7)
    total_samples = 80_017
    audio = np.random.randn(2, total_samples).astype(np.float32)

    C = 2048
    overlap = 2
    step = C // overlap
    model = _IdentityModel(S=1)

    result = inf_mod._chunked_process(
        model=model,
        audio=audio,
        C=C,
        step=step,
        batch_size=2,
        S=1,
        device="cpu",
        chunk_seconds=1.5,
        progress_file=None,
        pipeline_status=None,
    )

    result_np = _to_numpy(result)
    assert result_np.shape[-1] == total_samples


def test_chunked_process_matches_real_bug_dimensions(inf_mod):
    """Regression shape for BS_Roformer_Viperx-like parameters (chunk > C)."""
    np.random.seed(13)
    sr = 44_100
    total_samples = 7_938_000  # 180 s
    audio = np.random.randn(2, total_samples).astype(np.float32)

    # dim_t = 3105, hop_length = 512 -> C = 512 * (3105 - 1) = 1_589_248
    C = 1_589_248
    overlap = 2
    step = C // overlap
    model = _IdentityModel(S=1)

    result = inf_mod._chunked_process(
        model=model,
        audio=audio,
        C=C,
        step=step,
        batch_size=2,
        S=1,
        device="cpu",
        chunk_seconds=35.0,
        progress_file=None,
        pipeline_status=None,
    )

    result_np = _to_numpy(result)
    assert result_np.shape[-1] == total_samples, (
        f"real-bug dimensions: output length {result_np.shape[-1]} != {total_samples}"
    )


def test_ensure_output_length_trims_and_pads(inf_mod):
    """_ensure_output_length fixes small length drifts defensively."""
    import torch
    expected = 1000

    # Too long -> trim.
    long_tensor = torch.tensor(np.ones((1, 2, expected + 30), dtype=np.float32))
    fixed = inf_mod._ensure_output_length(long_tensor, expected)
    assert fixed.shape[-1] == expected

    # Too short -> pad.
    short_tensor = torch.tensor(np.ones((1, 2, expected - 20), dtype=np.float32))
    fixed = inf_mod._ensure_output_length(short_tensor, expected)
    assert fixed.shape[-1] == expected


def test_ensure_output_length_rejects_large_drift(inf_mod):
    """A mismatch larger than a few samples is treated as a real assembly bug."""
    import torch
    tensor = torch.tensor(np.ones((1, 2, 500), dtype=np.float32))
    with pytest.raises(ValueError, match="output length 500 does not match input length 1000"):
        inf_mod._ensure_output_length(tensor, 1000)
