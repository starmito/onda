"""onda onnx_utils — shared helpers for ONNX Runtime GPU loading.

Ensures CUDA/cuDNN libraries are visible to onnxruntime-gpu before creating an
inference session, and verifies the execution provider actually selected.
"""

import functools
import logging
import os
import warnings
from typing import Any, Dict, List, Optional, Tuple

logger = logging.getLogger(__name__)

CUDA_PROVIDER = "CUDAExecutionProvider"
CPU_PROVIDER = "CPUExecutionProvider"


def _gpu_backend_cache_dir() -> str:
    """Return the GPU backend cache directory used by entrypoint.sh."""
    return os.environ.get("ONDA_GPU_CACHE_DIR", "/opt/pytorch-backends/cuda")


def ensure_cuda_libs_ld_library_path(cache_dir: Optional[str] = None) -> None:
    """Prepend CUDA 13 / cuDNN lib dirs to LD_LIBRARY_PATH as a fallback.

    onnxruntime-gpu 1.27+ is built against CUDA 13. The PyTorch wheel installs
    the NVIDIA CUDA/cuDNN packages under ``$CACHE_DIR/nvidia/``. Adding those
    lib directories to LD_LIBRARY_PATH lets the dynamic loader resolve the
    libraries even when onnxruntime.preload_dlls() is unavailable or fails.
    """
    if cache_dir is None:
        cache_dir = _gpu_backend_cache_dir()

    paths = []
    cu13 = os.path.join(cache_dir, "nvidia", "cu13", "lib")
    cudnn = os.path.join(cache_dir, "nvidia", "cudnn", "lib")
    if os.path.isdir(cu13):
        paths.append(cu13)
    if os.path.isdir(cudnn):
        paths.append(cudnn)
    if not paths:
        return

    current = os.environ.get("LD_LIBRARY_PATH", "")
    current_parts = [p for p in current.split(os.pathsep) if p]
    missing = [p for p in paths if p not in current_parts]
    if missing:
        new_parts = missing + current_parts
        os.environ["LD_LIBRARY_PATH"] = os.pathsep.join(new_parts)
        logger.debug("Added CUDA/cuDNN lib dirs to LD_LIBRARY_PATH: %s", missing)


def _torch_cuda_available() -> bool:
    """Return True when PyTorch reports a CUDA device available."""
    try:
        import torch

        return bool(torch.cuda.is_available())
    except Exception:
        return False


def preload_onnx_dlls() -> bool:
    """Call onnxruntime.preload_dlls() before creating a session.

    Returns True when the call succeeds. Failures are logged but never raised,
    so that the module remains importable in environments without onnxruntime.
    """
    try:
        import onnxruntime as ort

        if not hasattr(ort, "preload_dlls"):
            return False

        # Try loading from NVIDIA site packages first (the standard location
        # for nvidia_* wheels installed alongside torch). Fall back to the
        # default search order if that fails.
        try:
            ort.preload_dlls(directory="")
        except Exception:
            ort.preload_dlls()
        return True
    except Exception as exc:
        logger.warning("onnxruntime.preload_dlls() failed: %s", exc)
    return False


def build_onnx_providers(
    prefer_cuda: bool = True,
    explicit: Optional[List[str]] = None,
) -> List[str]:
    """Return the provider list to pass to InferenceSession."""
    if explicit is not None:
        return list(explicit)
    if prefer_cuda and _torch_cuda_available():
        return [CUDA_PROVIDER, CPU_PROVIDER]
    return [CPU_PROVIDER]


def verify_onnx_providers(
    session: Any,
    requested_providers: Optional[List[str]] = None,
) -> List[str]:
    """Inspect a session's active providers and warn on silent CPU fallback.

    Args:
        session: An onnxruntime.InferenceSession instance.
        requested_providers: Providers originally requested; used to decide
            whether a CUDA request was silently ignored.

    Returns:
        The list of active providers from ``session.get_providers()``.
    """
    try:
        providers = session.get_providers()
    except Exception as exc:
        logger.warning("Could not read ONNX Runtime session providers: %s", exc)
        return []

    requested = requested_providers or []
    requested_cuda = CUDA_PROVIDER in requested
    has_cuda = CUDA_PROVIDER in providers

    if has_cuda:
        logger.info("ONNX Runtime active providers: %s", providers)
    else:
        logger.info("ONNX Runtime active providers (CPU): %s", providers)

    if requested_cuda and not has_cuda and _torch_cuda_available():
        warnings.warn(
            "CUDA was requested and a GPU is available, but ONNX Runtime is only "
            f"using {providers}. Check that CUDA/cuDNN libraries match the "
            "onnxruntime-gpu wheel and are visible in LD_LIBRARY_PATH.",
            RuntimeWarning,
            stacklevel=3,
        )
    return providers


def create_onnx_session(
    model_path: str,
    providers: Optional[List[str]] = None,
) -> Any:
    """Create an ONNX Runtime session with robust CUDA loading.

    This helper combines the recommended onnxruntime.preload_dlls() call, the
    LD_LIBRARY_PATH fallback for CUDA 13/cuDNN libraries, and post-creation
    verification so a silent CPU fallback is never hidden.
    """
    ensure_cuda_libs_ld_library_path()
    preload_onnx_dlls()

    import onnxruntime as ort

    providers = providers if providers is not None else build_onnx_providers()
    session = ort.InferenceSession(model_path, providers=providers)
    verify_onnx_providers(session, requested_providers=providers)
    return session


def _build_minimal_onnx_model_bytes() -> bytes:
    """Build a tiny ONNX Identity model in memory for provider probing.

    The model has a single float32 input and output of shape [1] and one
    Identity node. It is cheap to create (no disk, no download) and is only
    used to ask onnxruntime which execution providers it can actually load.
    """
    from onnx import helper, TensorProto

    input_tensor = helper.make_tensor_value_info("input", TensorProto.FLOAT, [1])
    output_tensor = helper.make_tensor_value_info("output", TensorProto.FLOAT, [1])
    node = helper.make_node("Identity", ["input"], ["output"])
    graph = helper.make_graph([node], "onda_onnx_probe", [input_tensor], [output_tensor])
    model = helper.make_model(graph, opset_imports=[helper.make_opsetid("", 13)])
    return model.SerializeToString()


@functools.lru_cache(maxsize=None)
def _probe_onnx_providers(cuda_requested: bool = True) -> Tuple[List[str], bool, Optional[str]]:
    """Create a real ONNX Runtime session and report the effective providers.

    Args:
        cuda_requested: Whether the caller expected CUDA to be used. When
            ``False``, falling back to CPU is not treated as an error.

    Returns:
        A tuple of (effective_providers, cuda_is_active, error_message).
        ``error_message`` is set when session creation fails or when CUDA was
        requested but the session silently fell back to CPU.
    """
    import onnxruntime as ort

    try:
        model_bytes = _build_minimal_onnx_model_bytes()
    except Exception as exc:
        return [CPU_PROVIDER], False, f"could not build probe model: {exc}"

    requested = [CUDA_PROVIDER, CPU_PROVIDER]
    try:
        session = ort.InferenceSession(model_bytes, providers=requested)
    except Exception as exc:
        return [CPU_PROVIDER], False, str(exc)

    try:
        providers = session.get_providers()
    except Exception as exc:
        return [CPU_PROVIDER], False, f"session created but could not read providers: {exc}"

    has_cuda = CUDA_PROVIDER in providers
    error: Optional[str] = None
    if not has_cuda and cuda_requested:
        error = (
            "CUDAExecutionProvider was requested but ONNX Runtime fell back to "
            f"{providers}"
        )
    return providers, has_cuda, error


def _clear_onnx_runtime_info_cache() -> None:
    """Invalidate the cached ONNX provider probe.

    Intended for tests; production code should not need to call this.
    """
    _probe_onnx_providers.cache_clear()


def get_onnx_runtime_info() -> Dict[str, Any]:
    """Return a JSON-serializable dict describing the ONNX Runtime environment.

    Used by the Go health endpoint to surface provider support without forcing
    operators to read Python logs. The ``cuda`` field reflects the *effective*
    provider used by a real session, not just the providers listed by
    ``get_available_providers()``.
    """
    info: Dict[str, Any] = {
        "available": False,
        "version": None,
        "providers": [],
        "cuda": False,
        "cuda_supported": False,
        "cuda_requested": False,
        "error": None,
    }
    try:
        import onnxruntime as ort

        info["available"] = True
        info["version"] = ort.__version__
        available_providers = ort.get_available_providers()
        info["cuda_supported"] = CUDA_PROVIDER in available_providers
        cuda_requested = _torch_cuda_available()
        info["cuda_requested"] = cuda_requested

        effective_providers, cuda_active, error = _probe_onnx_providers(cuda_requested=cuda_requested)
        info["providers"] = effective_providers
        info["cuda"] = cuda_active
        if error:
            info["error"] = error
    except Exception as exc:
        info["error"] = str(exc)
    return info
