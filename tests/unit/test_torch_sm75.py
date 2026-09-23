"""Guardián de arquitectura sm_75 de PyTorch.

Tarjetas pequeñas como la GTX 1650 (4 GB) necesitan el binario sm_75 en la
rueda de torch. Si la rueda deja de incluirlo, el runtime no puede lanzar
kernels en esas GPUs y Onda caería silenciosamente a CPU o directamente a un
error de CUDA.
"""

import pytest


def test_torch_wheel_includes_sm75():
    """La rueda de torch instalada debe incluir soporte sm_75.

    En el contenedor de Onda el wheel CPU se sustituye en runtime por el
    backend CUDA completo; este test verifica que ese backend siga traendo
    sm_75. Si torch no está disponible (entorno de desarrollo sin dependencias
    de inferencia) se salta limpiamente.
    """
    try:
        import torch
    except ImportError:
        pytest.skip("torch no esta instalado en este entorno")

    # El conftest de los tests unitarios inyecta un mock de torch. Saltamos si
    # no estamos frente a una instalacion real de PyTorch.
    if not getattr(torch, "__file__", None) or not hasattr(torch, "__version__"):
        pytest.skip("torch no es una instalacion real en este entorno")
    if not hasattr(torch.cuda, "get_arch_list"):
        pytest.skip("torch.cuda.get_arch_list no disponible en esta version/build")

    archs = torch.cuda.get_arch_list()
    if not archs:
        pytest.fail(
            "torch.cuda.get_arch_list() devolvio una lista vacia: "
            "la rueda instalada no parece traer soporte CUDA. "
            "Sin sm_75 tarjetas como GTX 1650 (4 GB) no pueden ejecutar kernels; "
            "Onda degradaria a CPU o fallaria al lanzar el modelo."
        )

    if "sm_75" not in archs:
        pytest.fail(
            f"La rueda de torch instalada no incluye sm_75 (arquitecturas={archs}). "
            "Las tarjetas GTX 1650 / 1650 Super / 1660 (Turing, 4-6 GB) no podran "
            "ejecutar los modelos y Onda degradaria a CPU o fallaria. "
            "Para 4 GB el pipeline debe usar segmentacion agresiva o CPU; sin sm_75 "
            "ni siquiera la ruta segmentada funciona en GPU."
        )
