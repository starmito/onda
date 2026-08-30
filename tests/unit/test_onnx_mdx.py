"""Tests for onda.onnx_mdx covering pure logic paths.

These tests do not need a GPU or onnxruntime: conftest.py injects lightweight
import mocks for torch/librosa/soundfile, and onda.onnx_mdx tolerates a missing
onnxruntime import so config resolution and CLI paths can be exercised.
"""

import json
import os
from types import SimpleNamespace

import pytest


def write_json_config(path, **kwargs):
    """Write a minimal valid MDXNet ONNX JSON config."""
    cfg = {
        "dim_f": 3072,
        "dim_t": 8,
        "n_fft": 6144,
        "hop_length": 1024,
        "overlap": 0.25,
    }
    cfg.update(kwargs)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(cfg, f)


def make_args(tmp_path, **kwargs):
    """Build a SimpleNamespace for run_onnx_mdx."""
    defaults = {
        "model": str(tmp_path / "model.onnx"),
        "config": None,
        "input": str(tmp_path / "input.wav"),
        "output": str(tmp_path / "output"),
        "overlap": 4,
        "device": "cpu",
        "progress_file": None,
        "pipeline_status": None,
    }
    defaults.update(kwargs)
    return SimpleNamespace(**defaults)


def test_has_onnx_config_fields():
    """_has_onnx_config_fields recognizes flat and nested configs."""
    import onda.onnx_mdx as omx

    assert omx._has_onnx_config_fields(
        {"dim_f": 1, "dim_t": 2, "n_fft": 3, "hop_length": 4}
    )
    assert omx._has_onnx_config_fields(
        {"audio": {"dim_f": 1, "n_fft": 3}, "inference": {"dim_t": 2, "hop_length": 4}}
    )
    assert not omx._has_onnx_config_fields({"dim_f": 1})
    assert not omx._has_onnx_config_fields({})


def test_resolve_onnx_config_prefers_same_prefix(tmp_path):
    """Config resolution prefers a JSON with the same base name as the model."""
    import onda.onnx_mdx as omx

    onnx_path = tmp_path / "MyModel.onnx"
    onnx_path.write_bytes(b"onnx")
    write_json_config(tmp_path / "MyModel.json")
    write_json_config(tmp_path / "other.json", dim_f=9999)

    detected = omx._resolve_onnx_config(str(tmp_path), "MyModel.onnx")
    assert detected is not None
    assert os.path.basename(detected) == "MyModel.json"
    cfg = omx._load_config(detected)
    assert cfg["dim_f"] == 3072


def test_resolve_onnx_config_falls_back_to_any_valid(tmp_path):
    """When no prefix-matching config exists, any valid config is selected."""
    import onda.onnx_mdx as omx

    onnx_path = tmp_path / "MyModel.onnx"
    onnx_path.write_bytes(b"onnx")
    write_json_config(tmp_path / "only_config.json")

    detected = omx._resolve_onnx_config(str(tmp_path), "MyModel.onnx")
    assert detected is not None
    assert os.path.basename(detected) == "only_config.json"


def test_resolve_onnx_config_returns_none_when_missing(tmp_path):
    """Resolution returns None when no valid config is present."""
    import onda.onnx_mdx as omx

    onnx_path = tmp_path / "MyModel.onnx"
    onnx_path.write_bytes(b"onnx")
    assert omx._resolve_onnx_config(str(tmp_path), "MyModel.onnx") is None


def test_run_onnx_mdx_exits_when_model_missing(tmp_path, capsys):
    """run_onnx_mdx exits 1 when the ONNX model is missing."""
    import onda.onnx_mdx as omx

    args = make_args(tmp_path, model="/nonexistent/model.onnx")
    with pytest.raises(SystemExit) as exc:
        omx.run_onnx_mdx(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "model not found" in captured.out.lower()


def test_run_onnx_mdx_exits_when_no_config_found(tmp_path, capsys):
    """run_onnx_mdx exits 1 when no MDXNet config is found."""
    import onda.onnx_mdx as omx

    model_path = tmp_path / "model.onnx"
    model_path.write_bytes(b"onnx")
    args = make_args(tmp_path, model=str(model_path))
    with pytest.raises(SystemExit) as exc:
        omx.run_onnx_mdx(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "no mdxnet config found" in captured.out.lower()


def test_run_onnx_mdx_exits_when_config_missing_fields(tmp_path, capsys):
    """run_onnx_mdx exits 1 when the config lacks required fields."""
    import onda.onnx_mdx as omx

    model_path = tmp_path / "model.onnx"
    model_path.write_bytes(b"onnx")
    bad_config = tmp_path / "model.json"
    bad_config.write_text(json.dumps({"dim_f": 1}), encoding="utf-8")
    args = make_args(tmp_path, model=str(model_path), config=str(bad_config))
    with pytest.raises(SystemExit) as exc:
        omx.run_onnx_mdx(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "missing mdxnet fields" in captured.out.lower()


def test_load_config_supports_yaml_and_json(tmp_path):
    """_load_config reads both JSON and YAML configs."""
    import onda.onnx_mdx as omx

    json_path = tmp_path / "cfg.json"
    write_json_config(json_path)
    assert omx._load_config(str(json_path))["dim_f"] == 3072

    yaml_path = tmp_path / "cfg.yaml"
    yaml_path.write_text(
        "audio:\n  dim_f: 2048\n  n_fft: 4096\n  hop_length: 1024\n"
        "inference:\n  dim_t: 8\n",
        encoding="utf-8",
    )
    cfg = omx._load_config(str(yaml_path))
    assert cfg["audio"]["dim_f"] == 2048
    assert omx._has_onnx_config_fields(cfg)
