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

    ``*.orig`` files are ignored.
    """
    yaml_path: Path | None = None
    json_path: Path | None = None
    for entry in model_dir.iterdir():
        if entry.is_dir():
            continue
        lower = entry.name.lower()
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

    return "unknown"


def extract_stems(
    cfg: dict[str, Any], json_cfg: dict[str, Any] | None = None
) -> dict[str, Any]:
    """Extract the ordered stem list, target stem and stem count.

    Sources, in order of preference:

    1. ``training.instruments`` from the YAML.
    2. ``model.sources`` from the YAML.
    3. ``target_instrument`` from a sidecar JSON (MDX-Net ONNX style), with an
       inferred residual stem.
    """
    training = cfg.get("training", {}) or {}
    model_cfg = cfg.get("model", {}) or {}

    instruments = training.get("instruments")
    if instruments:
        stems = [str(s).strip().lower() for s in instruments]
    else:
        sources = model_cfg.get("sources")
        if sources:
            stems = [str(s).strip().lower() for s in sources]
        elif json_cfg is not None and json_cfg.get("target_instrument"):
            target = str(json_cfg["target_instrument"]).strip().lower()
            stems = [target, "other"]
        else:
            stems = []

    if "target_instrument" in training:
        target = training["target_instrument"]
    elif json_cfg is not None and "target_instrument" in json_cfg:
        target = json_cfg["target_instrument"]
    else:
        target = None

    if target is not None:
        target = str(target).strip().lower()

    return {
        "stems": stems,
        "target": target,
        "num_stems": len(stems),
    }


def _flag(
    default: Any,
    *,
    editable: bool = True,
    min: Any | None = None,
    max: Any | None = None,
    step: Any | None = None,
) -> dict[str, Any]:
    entry: dict[str, Any] = {"default": default, "editable": editable}
    if min is not None:
        entry["min"] = min
    if max is not None:
        entry["max"] = max
    if step is not None:
        entry["step"] = step
    return entry


def extract_flags(cfg: dict[str, Any]) -> dict[str, Any]:
    """Extract editable inference/demucs flags with safe default ranges.

    Ranges are only included when they can be deduced from the semantics of the
    parameter; the YAML is the source of truth for the default value.
    """
    flags: dict[str, Any] = {}
    inference = cfg.get("inference", {}) or {}
    demucs = cfg.get("demucs", {}) or {}

    if "dim_t" in inference:
        flags["dim_t"] = _flag(inference["dim_t"], min=1, step=1)
    if "num_overlap" in inference:
        flags["num_overlap"] = _flag(inference["num_overlap"], min=1, max=16, step=1)
    if "batch_size" in inference:
        flags["batch_size"] = _flag(inference["batch_size"], min=1, max=16, step=1)
    if "chunk_size" in inference:
        flags["chunk_size"] = _flag(inference["chunk_size"], min=0, step=1)
    if "normalize" in inference:
        flags["normalize"] = _flag(inference["normalize"], editable=True)

    if "shifts" in demucs:
        flags["shifts"] = _flag(demucs["shifts"], min=0, max=10, step=1)
    if "segment" in demucs:
        flags["segment"] = _flag(demucs["segment"], min=0, step=1)
    if "jobs" in demucs:
        flags["jobs"] = _flag(demucs["jobs"], min=0, max=16, step=1)

    return flags


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
    stems_info = extract_stems(cfg, json_cfg)
    flags = extract_flags(cfg)

    manifest: dict[str, Any] = {
        "name": name,
        "type": model_type,
        "stems": stems_info,
        "flags": flags,
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
