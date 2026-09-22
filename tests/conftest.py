"""Shared pytest fixtures and import mocks.

The host test runner does not have GPU libraries installed (torch, librosa,
soundfile). We provide lightweight mocks in ``sys.modules`` so that tests can
import the inference modules purely to exercise their CLI/pure logic paths.
"""

import sys
from types import ModuleType
from unittest import mock

import numpy as np
import pytest


@pytest.fixture(scope="session", autouse=True)
def _mock_gpu_audio_deps():
    """Make torch/librosa/soundfile and lib_v5 modules importable as dummies."""
    # Track what we inject so we can clean it up after the session.
    injected = {}

    def _inject(name, module=None):
        if name not in sys.modules:
            mod = module or ModuleType(name)
            sys.modules[name] = mod
            injected[name] = mod
        return sys.modules[name]

    # torch + torch.nn mocks
    torch = _inject("torch")
    torch.device = lambda x, *args, **kwargs: x
    torch.cuda = ModuleType("torch.cuda")
    torch.cuda.is_available = lambda: False
    torch.load = lambda *args, **kwargs: {}
    torch.float32 = "float32"

    class _InferenceMode:
        def __enter__(self):
            return self
        def __exit__(self, *args):
            return False

    def _to_array(value):
        if isinstance(value, _Tensor):
            return value._data
        return np.array(value, dtype=np.float32)

    class _Tensor:
        def __init__(self, data):
            self._data = np.array(data, dtype=np.float32)
        def to(self, *args, **kwargs):
            return self
        def cpu(self):
            return self
        def numpy(self):
            return self._data
        @property
        def shape(self):
            return self._data.shape
        @property
        def dtype(self):
            return self._data.dtype
        @property
        def device(self):
            return "cpu"
        def __getitem__(self, key):
            return _Tensor(self._data[key])
        def __setitem__(self, key, value):
            if isinstance(value, _Tensor):
                value = value._data
            self._data[key] = value
        def unsqueeze(self, dim):
            return _Tensor(np.expand_dims(self._data, dim))
        def squeeze(self, dim=None):
            return _Tensor(np.squeeze(self._data, axis=dim))
        def dim(self):
            return self._data.ndim
        def numel(self):
            return self._data.size
        def __array__(self, dtype=None):
            return np.asarray(self._data, dtype=dtype)
        def __add__(self, other):
            return _Tensor(self._data + _to_array(other))
        def __radd__(self, other):
            return _Tensor(_to_array(other) + self._data)
        def __sub__(self, other):
            return _Tensor(self._data - _to_array(other))
        def __rsub__(self, other):
            return _Tensor(_to_array(other) - self._data)
        def __mul__(self, other):
            return _Tensor(self._data * _to_array(other))
        def __rmul__(self, other):
            return _Tensor(_to_array(other) * self._data)
        def __truediv__(self, other):
            return _Tensor(self._data / _to_array(other))
        def __rtruediv__(self, other):
            return _Tensor(_to_array(other) / self._data)
        def __iadd__(self, other):
            self._data = self._data + _to_array(other)
            return self
        def __isub__(self, other):
            self._data = self._data - _to_array(other)
            return self
        def __imul__(self, other):
            self._data = self._data * _to_array(other)
            return self
        def __itruediv__(self, other):
            self._data = self._data / _to_array(other)
            return self

    torch.inference_mode = lambda *args, **kwargs: _InferenceMode()
    torch.tensor = lambda data, *args, **kwargs: _Tensor(data)
    torch.zeros = lambda shape, *args, **kwargs: _Tensor(np.zeros(shape, dtype=np.float32))
    torch.ones = lambda shape, *args, **kwargs: _Tensor(np.ones(shape, dtype=np.float32))
    torch.linspace = lambda start, end, steps, *args, **kwargs: _Tensor(np.linspace(start, end, steps))
    torch.stack = lambda tensors, dim=0: _Tensor(np.stack([t._data if isinstance(t, _Tensor) else t for t in tensors], axis=dim))
    torch.cat = lambda tensors, dim=0: _Tensor(np.concatenate([t._data if isinstance(t, _Tensor) else np.array(t) for t in tensors], axis=dim))
    torch.hann_window = lambda size, *args, **kwargs: _Tensor(np.hanning(size).astype(np.float32))
    torch.from_numpy = lambda arr, *args, **kwargs: _Tensor(np.array(arr))
    torch.stft = lambda *args, **kwargs: _Tensor(np.zeros((2, 1025, 10), dtype=np.float32))
    torch.istft = lambda *args, **kwargs: _Tensor(np.zeros((2, 512), dtype=np.float32))
    torch.view_as_real = lambda x: _Tensor(np.stack([x._data, x._data], axis=-1))
    torch.view_as_complex = lambda x: _Tensor(x._data[..., 0] + 1j * x._data[..., 1])
    torch.nn = _inject("torch.nn")
    torch.nn.Module = type("Module", (), {"eval": lambda self: self, "to": lambda self, *args: self, "parameters": lambda self: []})
    torch.nn.functional = ModuleType("torch.nn.functional")
    def _pad(tensor, pad, mode="constant", value=0):
        arr = np.array(tensor)
        ndim = arr.ndim
        n_padded_dims = len(pad) // 2
        padding = [(0, 0)] * (ndim - n_padded_dims)
        for i in range(n_padded_dims):
            padding.append((pad[-2 * (i + 1)], pad[-2 * (i + 1) + 1]))
        if mode == "reflect":
            # numpy reflect is slightly different from torch reflect; edge is enough for tests.
            mode = "edge"
        kwargs = {}
        if mode == "constant":
            kwargs["constant_values"] = value
        return np.pad(arr, padding, mode=mode, **kwargs)
    torch.nn.functional.pad = _pad

    # onnxruntime mock (so onda.polarformer imports cleanly)
    ort = _inject("onnxruntime")
    ort.InferenceSession = mock.Mock(
        return_value=mock.MagicMock(
            get_inputs=mock.Mock(return_value=[mock.MagicMock(name="stft_features")])
        )
    )
    ort.CUDAExecutionProvider = "CUDAExecutionProvider"
    ort.CPUExecutionProvider = "CPUExecutionProvider"

    # librosa / soundfile mocks
    librosa = _inject("librosa")
    librosa.load = mock.Mock(return_value=(np.zeros(44100, dtype=np.float32), 44100))
    sf = _inject("soundfile")
    sf.write = mock.Mock()

    # lib_v5 model modules (used by onda.vocal)
    # Pre-populate the whole namespace so the real files (which need torch) are
    # never read during pure-logic tests.
    lib_v5 = _inject("lib_v5")
    lib_v5.__path__ = []

    def _dummy_model(*args, **kwargs):
        """Return an object that satisfies vocal's model interactions."""
        m = mock.MagicMock()
        m.parameters.return_value = []
        m.to.return_value = m
        m.eval.return_value = m
        return m

    mel_band = _inject("lib_v5.mel_band_roformer")
    mel_band.MelBandRoformer = _dummy_model
    bs = _inject("lib_v5.bs_roformer")
    bs.BSRoformer = _dummy_model

    yield

    for name in injected:
        sys.modules.pop(name, None)
