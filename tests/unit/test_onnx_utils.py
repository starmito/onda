"""Tests for onda.onnx_utils.

These tests exercise provider selection, LD_LIBRARY_PATH setup, and the
post-session verification helper without requiring a real GPU or a real
onnxruntime installation (the session-level paths rely on the onnxruntime mock
injected by conftest.py).
"""

import os
import sys
import warnings
from unittest import mock

import pytest


def _set_cuda_available(value: bool):
    """Monkeypatch torch.cuda.is_available for the helper."""
    import torch

    torch.cuda.is_available = lambda: value


def test_ensure_cuda_libs_ld_library_path_adds_existing_dirs(tmp_path, monkeypatch):
    """Existing CUDA 13 / cuDNN lib dirs are prepended to LD_LIBRARY_PATH."""
    import onda.onnx_utils as ou

    cu13 = tmp_path / "nvidia" / "cu13" / "lib"
    cudnn = tmp_path / "nvidia" / "cudnn" / "lib"
    cu13.mkdir(parents=True)
    cudnn.mkdir(parents=True)

    monkeypatch.setenv("LD_LIBRARY_PATH", "/existing")
    monkeypatch.setenv("ONDA_GPU_CACHE_DIR", str(tmp_path))

    ou.ensure_cuda_libs_ld_library_path()
    ld = os.environ["LD_LIBRARY_PATH"]
    parts = ld.split(os.pathsep)
    assert str(cu13) in parts
    assert str(cudnn) in parts
    assert "/existing" in parts
    assert parts.index(str(cu13)) < parts.index("/existing")
    assert parts.index(str(cudnn)) < parts.index("/existing")


def test_ensure_cuda_libs_ld_library_path_idempotent(tmp_path, monkeypatch):
    """The helper does not duplicate paths already present in LD_LIBRARY_PATH."""
    import onda.onnx_utils as ou

    cu13 = tmp_path / "nvidia" / "cu13" / "lib"
    cu13.mkdir(parents=True)
    monkeypatch.setenv("LD_LIBRARY_PATH", str(cu13))
    monkeypatch.setenv("ONDA_GPU_CACHE_DIR", str(tmp_path))

    ou.ensure_cuda_libs_ld_library_path()
    parts = os.environ["LD_LIBRARY_PATH"].split(os.pathsep)
    assert parts.count(str(cu13)) == 1


def test_build_onnx_providers_prefers_cuda_when_available(monkeypatch):
    """When torch sees CUDA, CUDAExecutionProvider is first."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: True)
    providers = ou.build_onnx_providers()
    assert providers == [ou.CUDA_PROVIDER, ou.CPU_PROVIDER]


def test_build_onnx_providers_cpu_fallback(monkeypatch):
    """When torch does not see CUDA, only CPUExecutionProvider is returned."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: False)
    providers = ou.build_onnx_providers()
    assert providers == [ou.CPU_PROVIDER]


def test_build_onnx_providers_honours_explicit_list():
    """Explicit provider list overrides the CUDA heuristic."""
    import onda.onnx_utils as ou

    explicit = ["CPUExecutionProvider"]
    assert ou.build_onnx_providers(explicit=explicit) == explicit


def test_verify_onnx_providers_warns_when_cuda_requested_but_cpu_used(monkeypatch):
    """A silent CPU fallback with GPU available emits a RuntimeWarning."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: True)
    session = mock.MagicMock()
    session.get_providers.return_value = [ou.CPU_PROVIDER]

    with pytest.warns(RuntimeWarning, match="CUDA was requested"):
        providers = ou.verify_onnx_providers(
            session, requested_providers=[ou.CUDA_PROVIDER, ou.CPU_PROVIDER]
        )
    assert providers == [ou.CPU_PROVIDER]


def test_verify_onnx_providers_no_warning_when_cuda_present(monkeypatch):
    """No warning when CUDAExecutionProvider is active."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: True)
    session = mock.MagicMock()
    session.get_providers.return_value = [ou.CUDA_PROVIDER, ou.CPU_PROVIDER]

    with warnings.catch_warnings():
        warnings.simplefilter("error")
        providers = ou.verify_onnx_providers(session, requested_providers=[ou.CUDA_PROVIDER, ou.CPU_PROVIDER])
    assert ou.CUDA_PROVIDER in providers


def test_create_onnx_session_uses_helper(monkeypatch, tmp_path):
    """create_onnx_session calls preload, creates the session and verifies it."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: False)
    model_path = tmp_path / "model.onnx"
    model_path.write_bytes(b"onnx")

    session = ou.create_onnx_session(str(model_path))
    assert session is not None
    # The conftest mock returns a MagicMock; the helper must have called
    # get_providers on it.
    session.get_providers.assert_called_once()


def test_get_onnx_runtime_info_structure(monkeypatch):
    """get_onnx_runtime_info returns the expected JSON-serializable fields."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: False)
    info = ou.get_onnx_runtime_info()
    assert info["available"] is True
    assert "version" in info
    assert "providers" in info
    assert info["cuda"] is False
    assert info["cuda_requested"] is False
    assert info["error"] is None


def test_get_onnx_runtime_info_detects_cuda_provider(monkeypatch):
    """cuda=true when CUDAExecutionProvider is in available providers."""
    import onda.onnx_utils as ou

    monkeypatch.setattr("torch.cuda.is_available", lambda: True)
    # The conftest mock returns ["CPUExecutionProvider"]; patch it temporarily.
    import onnxruntime as ort

    original = ort.get_available_providers
    ort.get_available_providers = mock.Mock(return_value=[ou.CUDA_PROVIDER, ou.CPU_PROVIDER])
    try:
        info = ou.get_onnx_runtime_info()
        assert info["cuda"] is True
        assert info["cuda_requested"] is True
    finally:
        ort.get_available_providers = original
