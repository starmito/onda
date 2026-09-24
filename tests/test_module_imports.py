"""Smoke tests that every separation module imports cleanly.

These tests run in the inference environment where ``torch`` (and the rest
of the heavy ML stack) is available; they are skipped on the host runner
where ``torch`` is not installed.
"""

import importlib
import pytest

pytest.importorskip("torch")

MODULOS = [
    "onda.mdx",
    "onda.onnx_mdx",
    "onda.scnet",
    "onda.polarformer",
    "onda.vocal",
    "onda.demucs",
]


@pytest.mark.parametrize("mod", MODULOS)
def test_modulo_importa(mod):
    """Todos los módulos de separación deben importarse sin error."""
    importlib.import_module(mod)
