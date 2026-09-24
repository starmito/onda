import importlib
import pytest

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
