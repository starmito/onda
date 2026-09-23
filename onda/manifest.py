"""Model manifest generator.

Reads model YAML/JSON configs (tolerating ``!!python/tuple`` tags safely) and
writes a ``model.manifest.json`` next to each model with extracted metadata.

The manifest is meant to become the single source of truth for:

* model architecture type
* produced stems, target stem and stem count
* editable inference/demucs flags with defaults
* the source YAML and generation timestamp
"""

from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import yaml


class TolerantSafeLoader(yaml.SafeLoader):
    """Safe YAML loader that accepts ``!!python/tuple`` as a plain list."""


def _python_tuple_constructor(loader: yaml.SafeLoader, node: yaml.Node) -> list[Any]:
    """Construct a python/tuple tag as a normal list (no code execution)."""
    return loader.construct_sequence(node)


TolerantSafeLoader.add_constructor(
    "tag:yaml.org,2002:python/tuple", _python_tuple_constructor
)


def load_yaml_tolerant(path: str | Path) -> dict[str, Any]:
    """Load a YAML file safely, tolerating ``!!python/tuple`` tags.

    Returns an empty dict for an empty file and never executes arbitrary code.
    """
    with open(path, "r", encoding="utf-8") as f:
        doc = yaml.load(f, Loader=TolerantSafeLoader)
    if doc is None:
        return {}
    if not isinstance(doc, dict):
        raise ValueError(f"YAML root is {type(doc).__name__}, expected mapping")
    return doc


def repo_root() -> Path:
    """Return the repository root (parent of the ``onda`` package)."""
    return Path(__file__).resolve().parent.parent


def default_models_dir() -> Path:
    """Return the default on-disk models directory."""
    return repo_root() / "data" / "models"


def _find_config_files(model_dir: Path) -> tuple[Path | None, Path | None]:
    """Find YAML and optional JSON config files in a model directory.

    ``*.orig`` files and generated manifests are ignored.
    """
    yaml_path: Path | None = None
    json_path: Path | None = None
    for entry in model_dir.iterdir():
        if entry.is_dir():
            continue
        lower = entry.name.lower()
        if lower == "model.manifest.json":
            continue
        if lower.endswith((".yaml", ".yml")) and not lower.endswith(".orig"):
            yaml_path = entry
        elif lower.endswith(".json"):
            json_path = entry
    return yaml_path, json_path


def _load_json(path: Path) -> dict[str, Any]:
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict):
        raise ValueError(f"JSON root is {type(data).__name__}, expected object")
    return data


def _find_checkpoint(model_dir: Path) -> Path | None:
    """Return the first checkpoint file found in the model directory."""
    for ext in (".ckpt", ".onnx", ".pth", ".th"):
        for ckpt in sorted(model_dir.glob(f"*{ext}")):
            return ckpt
    return None


def detect_model_type(
    cfg: dict[str, Any], checkpoint_path: Path | None = None
) -> str:
    """Deduce the real architecture type from YAML keys or checkpoint name.

    Returns ``"unknown"`` when no signal is available instead of inventing a
    value.
    """
    model_cfg = cfg.get("model", {}) or {}
    if isinstance(model_cfg, dict):
        explicit = model_cfg.get("type")
        if explicit:
            return str(explicit).strip().lower()

        if any(
            k in model_cfg
            for k in ("band_SR", "band_stride", "band_kernel", "conv_depths")
        ):
            return "scnet"
        if "freqs_per_bands" in model_cfg:
            return "bs_roformer"
        if any(
            k in model_cfg
            for k in ("num_scales", "num_subbands", "num_blocks_per_scale")
        ):
            return "mdx23c"

    if checkpoint_path is not None:
        lower = str(checkpoint_path).lower()
        if "scnet" in lower:
            return "scnet"
        if "mdx23c" in lower:
            return "mdx23c"
        if "roformer" in lower:
            return "bs_roformer"
        if lower.endswith(".onnx"):
            return "mdx_net"
        if "demucs" in lower or "htdemucs" in lower:
            return "demucs"

    return "unknown"


def extract_stems(
    cfg: dict[str, Any], json_cfg: dict[str, Any] | None = None, model_type: str = "unknown"
) -> dict[str, Any]:
    """Extract the stems Onda will actually produce.

    The manifest must describe what Onda generates, not only what the YAML
    declares. When a model has a ``target_instrument``, Onda derives the
    complementary ``instrumental`` stem by subtraction, so the produced stems
    are ``[target, "instrumental"]`` and ``num_stems`` is 2.

    The originally declared instruments/count are preserved as
    ``declared_instruments`` / ``declared_num_stems`` so the drift between
    "what the model predicts" and "what Onda writes" is always visible.
    """
    training = cfg.get("training", {}) or {}
    model_cfg = cfg.get("model", {}) or {}

    # 1. What the YAML/JSON declares.
    declared_instruments: list[str] = []
    if training.get("instruments"):
        declared_instruments = [str(s).strip().lower() for s in training["instruments"]]
    elif model_cfg.get("sources"):
        declared_instruments = [str(s).strip().lower() for s in model_cfg["sources"]]
    elif json_cfg is not None and json_cfg.get("target_instrument"):
        declared_instruments = [
            str(json_cfg["target_instrument"]).strip().lower()
        ]

    if "target_instrument" in training:
        target = training["target_instrument"]
    elif json_cfg is not None and "target_instrument" in json_cfg:
        target = json_cfg["target_instrument"]
    else:
        target = None

    if target is not None:
        target = str(target).strip().lower()

    # 2. What Onda will generate.
    if target is not None:
        stems = [target, "instrumental"]
    else:
        stems = declared_instruments[:]

    # Demucs models downloaded as a bare checkpoint do not declare their
    # instruments, but the app always produces the canonical 4-stem set.
    if not stems and model_type == "demucs":
        declared_instruments = ["drums", "bass", "other", "vocals"]
        stems = declared_instruments[:]

    return {
        "stems": stems,
        "target": target,
        "num_stems": len(stems),
        "declared_instruments": declared_instruments,
        "declared_num_stems": len(declared_instruments),
    }


def _flag(
    default: Any,
    *,
    editable: bool = True,
    min: Any | None = None,
    max: Any | None = None,
    step: Any | None = None,
    choices: list[Any] | None = None,
    description: str = "",
    affects: list[str] | None = None,
    better_side: str = "",
) -> dict[str, Any]:
    entry: dict[str, Any] = {"default": default, "editable": editable}
    if min is not None:
        entry["min"] = min
    if max is not None:
        entry["max"] = max
    if step is not None:
        entry["step"] = step
    if choices is not None:
        entry["choices"] = choices
    if description:
        entry["description"] = description
    if affects:
        entry["affects"] = affects
    if better_side:
        entry["better_side"] = better_side
    return entry


def _int(value: Any, fallback: int) -> int:
    """Coerce *value* to int, returning *fallback* if impossible."""
    try:
        return int(value)
    except (TypeError, ValueError):
        return fallback


def _float(value: Any, fallback: float) -> float:
    """Coerce *value* to float, returning *fallback* if impossible."""
    try:
        return float(value)
    except (TypeError, ValueError):
        return fallback


# Metadata shared by every manifest.  ``affects`` and ``better_side`` use the
# vocabulary expected by the frontend: quality, vram, speed.
_FLAG_META: dict[str, dict[str, Any]] = {
    "segment_size": {
        "description": (
            "Número de muestras de audio que el modelo procesa en cada ventana "
            "de análisis. Valores mayores suelen mejorar la calidad hasta el "
            "óptimo del modelo, pero consumen más VRAM."
        ),
        "affects": ["quality", "vram"],
        "better_side": "quality",
    },
    "num_overlap": {
        "description": (
            "Número de ventanas solapadas entre segmentos consecutivos. "
            "Más solapamiento reduce artefactos de costura y mejora la calidad, "
            "a costa de más VRAM y tiempo de proceso."
        ),
        "affects": ["quality", "vram"],
        "better_side": "quality",
    },
    "batch_size": {
        "description": (
            "Número de segmentos procesados a la vez. Valores mayores aceleran "
            "la separación y usan más VRAM, sin cambiar la calidad del resultado."
        ),
        "affects": ["vram", "speed"],
        "better_side": "vram",
    },
    "chunk_size": {
        "description": (
            "Duración máxima de cada trozo procesado, en segundos. "
            "0 procesa la canción entera de una vez (máxima calidad, más VRAM)."
        ),
        "affects": ["quality", "vram"],
        "better_side": "quality",
    },
    "device": {
        "description": "Dispositivo de cálculo: CUDA (GPU) o CPU.",
        "affects": [],
        "better_side": "",
    },
    "shifts": {
        "description": (
            "Número de predicciones con pequeños desplazamientos temporales que "
            "se promedian. Aumentar mejora la calidad a costa de mucho más tiempo."
        ),
        "affects": ["quality", "speed"],
        "better_side": "quality",
    },
    "segment": {
        "description": (
            "Longitud de los segmentos analizados por Demucs, en segundos. "
            "0 deja que el modelo elija automáticamente. Valores mayores suelen "
            "dar mejor calidad hasta el óptimo del modelo."
        ),
        "affects": ["quality"],
        "better_side": "quality",
    },
    "jobs": {
        "description": (
            "Número de trabajos paralelos durante la separación. Más trabajos "
            "aceleran el proceso pero no afectan la calidad."
        ),
        "affects": ["speed"],
        "better_side": "speed",
    },
}

# Which flags are exposed for each canonical architecture.  This must stay in
# sync with the backend so the UI only sees controls that really apply.
_FLAGS_BY_TYPE: dict[str, set[str]] = {
    "bs_roformer": {"segment_size", "num_overlap", "chunk_size", "batch_size", "device"},
    "mel_band_roformer": {"segment_size", "num_overlap", "chunk_size", "batch_size", "device"},
    "mdx23c": {"segment_size", "num_overlap", "batch_size", "device"},
    "mdx_net": {"segment_size", "num_overlap", "batch_size", "device"},
    "scnet": {"segment_size", "num_overlap", "chunk_size", "batch_size", "device"},
    "demucs": {"shifts", "segment", "jobs", "device"},
    "htdemucs": {"shifts", "segment", "jobs", "device"},
}


def extract_flags(cfg: dict[str, Any], model_type: str = "unknown") -> dict[str, Any]:
    """Extract editable inference flags using the app's vocabulary.

    The returned keys match the names used by the backend and pipeline
    (``segment_size``, ``num_overlap``, ``batch_size``, ``chunk_size``,
    ``device``, ``shifts``, ``segment``, ``jobs``).  Raw architecture keys such
    as ``dim_t`` are kept as metadata, not as editable flags.

    Defaults come from the model config when available; otherwise the app-wide
    fallback is used and documented explicitly.  Declared values that fall
    outside the supported range are clamped to the nearest valid bound so the
    manifest never advertises an unusable default.
    """
    inference = cfg.get("inference", {}) or {}
    demucs = cfg.get("demucs", {}) or {}

    # Values declared by the model.
    segment_size = _int(inference.get("dim_t"), 0)
    num_overlap = _int(inference.get("num_overlap"), 0)
    batch_size = _int(inference.get("batch_size"), 0)
    chunk_size = _int(inference.get("chunk_size"), 0) if "chunk_size" in inference else 0

    shifts = _int(demucs["shifts"], 0) if "shifts" in demucs else 1
    segment = _float(demucs["segment"], 0.0) if "segment" in demucs else 0.0
    jobs = _int(demucs["jobs"], 0) if "jobs" in demucs else 0

    # Effective defaults matching the backend pipeline.  Keys present in the
    # model config keep their declared value even when it is 0; missing keys
    # fall back to the app-wide default.
    effective_segment_size = segment_size if segment_size > 0 else 512
    effective_num_overlap = num_overlap if num_overlap > 0 else 4
    effective_batch_size = batch_size if batch_size > 0 else 1
    effective_chunk_size = chunk_size
    effective_segment = segment

    # Clamp declared defaults to the ranges the UI and pipeline actually support.
    effective_segment_size = max(128, min(effective_segment_size, 2048))
    effective_num_overlap = max(1, min(effective_num_overlap, 8))
    effective_batch_size = max(1, min(effective_batch_size, 8))
    effective_shifts = max(1, min(shifts, 10)) if shifts > 0 else 1
    effective_jobs = max(1, min(jobs, 8)) if jobs > 0 else 1

    all_flags: dict[str, Any] = {
        "segment_size": _flag(
            effective_segment_size,
            min=128,
            max=2048,
            step=1,
            **_FLAG_META["segment_size"],
        ),
        "num_overlap": _flag(
            effective_num_overlap,
            min=1,
            max=8,
            step=1,
            **_FLAG_META["num_overlap"],
        ),
        "batch_size": _flag(
            effective_batch_size,
            min=1,
            max=8,
            step=1,
            **_FLAG_META["batch_size"],
        ),
        "chunk_size": _flag(
            effective_chunk_size,
            min=0,
            max=600,
            step=1,
            **_FLAG_META["chunk_size"],
        ),
        "device": _flag(
            "cuda",
            choices=["cuda", "cpu"],
            **_FLAG_META["device"],
        ),
        "shifts": _flag(
            effective_shifts,
            min=1,
            max=10,
            step=1,
            **_FLAG_META["shifts"],
        ),
        "segment": _flag(
            effective_segment,
            min=0,
            max=7,
            step=1,
            **_FLAG_META["segment"],
        ),
        "jobs": _flag(
            effective_jobs,
            min=1,
            max=8,
            step=1,
            **_FLAG_META["jobs"],
        ),
    }

    selected = _FLAGS_BY_TYPE.get(model_type, set(all_flags.keys()))
    return {name: all_flags[name] for name in sorted(selected)}


def extract_metadata(
    cfg: dict[str, Any], json_cfg: dict[str, Any] | None = None
) -> dict[str, Any]:
    """Preserve raw architecture/inference keys as read-only metadata.

    Values such as ``dim_t`` or ``normalize`` describe the trained model; they
    are not user-editable inference flags, but they are useful for traceability
    and for mapping between the YAML vocabulary and the app vocabulary.
    """
    metadata: dict[str, Any] = {}
    inference = cfg.get("inference", {}) or {}
    if inference:
        metadata["inference"] = dict(inference)
    model_cfg = cfg.get("model", {}) or {}
    model_meta = {
        k: v
        for k, v in model_cfg.items()
        if k in ("num_stems", "type")
    }
    if model_meta:
        metadata["model"] = model_meta
    if json_cfg:
        metadata["sidecar_json"] = dict(json_cfg)
    return metadata


def generate_manifest(
    model_dir: str | Path,
    *,
    now: datetime | None = None,
) -> dict[str, Any]:
    """Build a manifest dict for a single model directory.

    The function never raises: missing or broken configs are recorded as
    ``warnings`` and the manifest is generated with whatever can be deduced.
    """
    model_dir = Path(model_dir)
    name = model_dir.name
    yaml_path, json_path = _find_config_files(model_dir)
    checkpoint = _find_checkpoint(model_dir)

    cfg: dict[str, Any] = {}
    json_cfg: dict[str, Any] | None = None
    source_yaml: str | None = None
    warnings: list[str] = []

    if yaml_path is not None:
        source_yaml = yaml_path.name
        try:
            cfg = load_yaml_tolerant(yaml_path)
        except Exception as exc:  # pragma: no cover - defensive
            warnings.append(f"Failed to parse {yaml_path.name}: {exc}")
            cfg = {}
    else:
        warnings.append("No YAML config found")

    if json_path is not None:
        try:
            json_cfg = _load_json(json_path)
        except Exception as exc:  # pragma: no cover - defensive
            warnings.append(f"Failed to parse {json_path.name}: {exc}")

    model_type = detect_model_type(cfg, checkpoint)
    stems_info = extract_stems(cfg, json_cfg, model_type=model_type)
    flags = extract_flags(cfg, model_type=model_type)
    metadata = extract_metadata(cfg, json_cfg)

    manifest: dict[str, Any] = {
        "name": name,
        "type": model_type,
        "stems": stems_info,
        "flags": flags,
        "metadata": metadata,
        "source_yaml": source_yaml,
        "checkpoint": checkpoint.name if checkpoint else None,
        "generated_at": (now or datetime.now(timezone.utc)).isoformat(),
    }
    if warnings:
        manifest["warnings"] = warnings

    return manifest


def write_manifest(
    manifest: dict[str, Any],
    model_dir: str | Path,
) -> Path:
    """Write ``model.manifest.json`` inside *model_dir*."""
    model_dir = Path(model_dir)
    path = model_dir / "model.manifest.json"
    with open(path, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2, ensure_ascii=False)
        f.write("\n")
    return path


def regenerate_manifests(
    models_dir: str | Path,
    *,
    write: bool = True,
    now: datetime | None = None,
) -> list[dict[str, Any]]:
    """Walk *models_dir* and (re)generate a manifest for every model folder.

    Each entry in the returned list contains ``category``, ``model``,
    ``manifest`` and, when *write* is ``True``, the written ``path``.
    """
    models_dir = Path(models_dir)
    results: list[dict[str, Any]] = []
    if not models_dir.is_dir():
        return results

    for category_dir in sorted(models_dir.iterdir()):
        if not category_dir.is_dir():
            continue
        for model_dir in sorted(category_dir.iterdir()):
            if not model_dir.is_dir():
                continue
            if _find_checkpoint(model_dir) is None:
                continue
            manifest = generate_manifest(model_dir, now=now)
            result: dict[str, Any] = {
                "category": category_dir.name,
                "model": model_dir.name,
                "manifest": manifest,
            }
            if write:
                path = write_manifest(manifest, model_dir)
                result["path"] = str(path)
            results.append(result)

    return results
