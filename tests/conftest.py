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
        def __getitem__(self, key):
            return _Tensor(self._data[key])
        def __setitem__(self, key, value):
            if isinstance(value, _Tensor):
                value = value._data
            self._data[key] = value
        def unsqueeze(self, dim):
            return _Tensor(np.expand_dims(self._data, dim))
        def dim(self):
            return self._data.ndim
        def numel(self):
            return self._data.size

    torch.inference_mode = lambda *args, **kwargs: _InferenceMode()
    torch.tensor = lambda data, *args, **kwargs: _Tensor(data)
    torch.zeros = lambda shape, *args, **kwargs: _Tensor(np.zeros(shape, dtype=np.float32))
    torch.linspace = lambda start, end, steps, *args, **kwargs: _Tensor(np.linspace(start, end, steps))
    torch.stack = lambda tensors, dim=0: _Tensor(np.stack([t._data if isinstance(t, _Tensor) else t for t in tensors], axis=dim))
    torch.nn = _inject("torch.nn")
    torch.nn.Module = type("Module", (), {"eval": lambda self: self, "to": lambda self, *args: self, "parameters": lambda self: []})
    torch.nn.functional = ModuleType("torch.nn.functional")
    torch.nn.functional.pad = lambda tensor, pad, *args, **kwargs: np.pad(np.array(tensor), [(0, 0)] * (np.array(tensor).ndim - len(pad) // 2) + [(pad[0], pad[1])], mode="edge")

    # librosa / soundfile mocks
    librosa = _inject("librosa")
    librosa.load = mock.Mock(return_value=(np.zeros(44100, dtype=np.float32), 44100))
    sf = _inject("soundfile")
    sf.write = mock.Mock()

    # lib_v5 model modules (used by onda.viperx)
    # Pre-populate the whole namespace so the real files (which need torch) are
    # never read during pure-logic tests.
    lib_v5 = _inject("lib_v5")
    lib_v5.__path__ = []

    def _dummy_model(*args, **kwargs):
        """Return an object that satisfies viperx's model interactions."""
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
