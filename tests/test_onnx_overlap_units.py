"""Unit tests for the MDXNet ONNX overlap unit handling.

These tests exercise only the overlap normalisation and chunking-plan logic;
they rely on conftest.py mocks for torch/onnxruntime so they run without a GPU.
"""

from unittest import mock

import pytest


def _make_separator(tmp_path, overlap):
    """Build an OnnxMDX instance using the mocked backend.

    We override ``initialize_model_settings`` so the STFT helper (which needs
    real torch audio support) is never created; chunk_size is all we need for
    the overlap unit tests.  ``create_onnx_session`` is replaced by a fresh mock
    so these tests do not interfere with the shared conftest session mock.
    """
    import onda.onnx_mdx as omx

    class _TestableOnnxMDX(omx.OnnxMDX):
        def initialize_model_settings(self):
            self.n_bins = self.n_fft // 2 + 1
            self.trim = self.n_fft // 2
            self.chunk_size = self.hop_length * (self.dim_t - 1)
            self.gen_size = self.chunk_size - 2 * self.trim
            self.stft = None

    model_path = tmp_path / "model.onnx"
    model_path.write_bytes(b"onnx")
    config = {
        "dim_f": 100,
        "dim_t": 3,
        "n_fft": 200,
        "hop_length": 100,
    }
    fake_session = mock.MagicMock(
        get_inputs=mock.Mock(return_value=[mock.MagicMock(name="stft_features")]),
        get_providers=mock.Mock(return_value=["CPUExecutionProvider"]),
    )
    # The conftest mock makes torch.device return its argument unchanged.
    with mock.patch.object(omx, "create_onnx_session", return_value=fake_session):
        return _TestableOnnxMDX(config, str(model_path), "cpu", overlap=overlap)


def test_overlap_count_yields_positive_step_and_multiple_chunks(tmp_path):
    """With overlap=8 (count) the step must be positive and chunks > 1."""
    sep = _make_separator(tmp_path, overlap=8)
    assert sep._overlap_fraction == 0.125
    step, total = sep._chunking_plan(mixture_len=500)
    assert step > 0
    assert total > 1


def test_overlap_fraction_equivalent_to_count(tmp_path):
    """overlap=0.25 (fraction) and overlap=4 (count) must give the same plan."""
    sep_frac = _make_separator(tmp_path, overlap=0.25)
    sep_count = _make_separator(tmp_path, overlap=4)
    assert sep_frac._overlap_fraction == sep_count._overlap_fraction == 0.25

    step_f, total_f = sep_frac._chunking_plan(mixture_len=500)
    step_c, total_c = sep_count._chunking_plan(mixture_len=500)
    assert step_f == step_c
    assert total_f == total_c


def test_invalid_overlap_values_fall_back_to_default(tmp_path, capsys):
    """Non-numeric, None, zero or negative overlap values fall back to 0.25."""
    for bad in (0, -1, None, "foo"):
        sep = _make_separator(tmp_path, overlap=bad)
        assert sep._overlap_fraction == 0.25
        out = capsys.readouterr().out
        assert "inválido" in out.lower() or "invalid" in out.lower()


def test_chunking_plan_never_returns_zero_chunks(tmp_path):
    """Even with a tiny or zero-length mixture total_chunks must be >= 1."""
    sep = _make_separator(tmp_path, overlap=8)
    for length in (0, 1, 50, 500):
        step, total = sep._chunking_plan(length)
        assert step > 0
        assert total >= 1


def test_legacy_buggy_formula_reproduces_negative_step(tmp_path):
    """Document the old bug: overlap=8 used as a fraction gave a negative step."""
    sep = _make_separator(tmp_path, overlap=8)
    chunk_size = sep.chunk_size
    # This is the *old* calculation that treated the integer as a fraction.
    old_step = int((1 - 8) * chunk_size)
    assert old_step < 0
    old_total = (500 + old_step - 1) // old_step
    assert old_total == 0
