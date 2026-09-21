"""Tests for onda.polarformer covering pure logic paths.

These tests do not need a GPU or real onnxruntime: conftest.py injects
lightweight mocks for torch/librosa/soundfile/onnxruntime so config resolution
and CLI paths can be exercised.
"""

import json
import os
from types import SimpleNamespace

import pytest


def write_yaml_config(path, use_pope=True, **kwargs):
    """Write a minimal valid PolarFormer YAML config."""
    cfg = {
        "audio": {
            "sample_rate": 44100,
            "n_fft": 2048,
            "hop_length": 512,
            "num_channels": 2,
        },
        "model": {
            "stft_n_fft": 2048,
            "stft_hop_length": 512,
            "stft_win_length": 2048,
            "stereo": True,
            "use_pope": use_pope,
        },
        "inference": {
            "chunk_size": 882000,
            "num_overlap": 2,
            "batch_size": 4,
        },
    }
    cfg["model"].update(kwargs.get("model", {}))
    cfg["inference"].update(kwargs.get("inference", {}))
    import yaml

    with open(path, "w", encoding="utf-8") as f:
        yaml.dump(cfg, f, default_flow_style=False)


def make_args(tmp_path, **kwargs):
    """Build a SimpleNamespace for run_polarformer."""
    defaults = {
        "model": str(tmp_path / "model.onnx"),
        "config": None,
        "input": str(tmp_path / "input.wav"),
        "output": str(tmp_path / "output"),
        "overlap": 2,
        "device": "cpu",
        "progress_file": None,
        "pipeline_status": None,
        "chunk_size": None,
        "batch_size": None,
    }
    defaults.update(kwargs)
    return SimpleNamespace(**defaults)


def test_is_polarformer_config():
    """_is_polarformer_config recognises the canonical use_pope flag."""
    import onda.polarformer as pf

    assert pf._is_polarformer_config({"model": {"use_pope": True}})
    assert not pf._is_polarformer_config({"model": {"use_pope": False}})
    assert not pf._is_polarformer_config({"model": {"freqs_per_bands": [2, 4]}})
    assert not pf._is_polarformer_config({})


def test_resolve_polarformer_config_prefers_same_prefix(tmp_path):
    """Config resolution prefers a YAML with the same base name as the model."""
    import onda.polarformer as pf

    onnx_path = tmp_path / "MyModel.onnx"
    onnx_path.write_bytes(b"onnx")
    write_yaml_config(tmp_path / "MyModel.yaml")
    write_yaml_config(tmp_path / "other.yaml", use_pope=False)

    detected = pf._resolve_polarformer_config(str(tmp_path), "MyModel.onnx")
    assert detected is not None
    assert os.path.basename(detected) == "MyModel.yaml"


def test_resolve_polarformer_config_by_use_pope(tmp_path):
    """A YAML with use_pope True is detected even with a different base name."""
    import onda.polarformer as pf

    onnx_path = tmp_path / "model.onnx"
    onnx_path.write_bytes(b"onnx")
    write_yaml_config(tmp_path / "config.yaml")

    detected = pf._resolve_polarformer_config(str(tmp_path), "model.onnx")
    assert detected is not None
    assert os.path.basename(detected) == "config.yaml"


def test_resolve_polarformer_config_returns_none_when_missing(tmp_path):
    """Resolution returns None when no valid config is present."""
    import onda.polarformer as pf

    onnx_path = tmp_path / "model.onnx"
    onnx_path.write_bytes(b"onnx")
    assert pf._resolve_polarformer_config(str(tmp_path), "model.onnx") is None


def test_run_polarformer_exits_when_model_missing(tmp_path, capsys):
    """run_polarformer exits 1 when the ONNX model is missing."""
    import onda.polarformer as pf

    args = make_args(tmp_path, model="/nonexistent/model.onnx")
    with pytest.raises(SystemExit) as exc:
        pf.run_polarformer(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "model not found" in captured.out.lower()


def test_run_polarformer_exits_when_no_config_found(tmp_path, capsys):
    """run_polarformer exits 1 when no PolarFormer config is found."""
    import onda.polarformer as pf

    model_path = tmp_path / "model.onnx"
    model_path.write_bytes(b"onnx")
    args = make_args(tmp_path, model=str(model_path))
    with pytest.raises(SystemExit) as exc:
        pf.run_polarformer(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "no polarformer config found" in captured.out.lower()


def test_load_config_supports_yaml_and_json(tmp_path):
    """_load_config reads both JSON and YAML configs."""
    import onda.polarformer as pf

    json_path = tmp_path / "cfg.json"
    json_path.write_text(json.dumps({"model": {"use_pope": True}}), encoding="utf-8")
    assert pf._load_config(str(json_path))["model"]["use_pope"] is True

    yaml_path = tmp_path / "cfg.yaml"
    write_yaml_config(yaml_path)
    cfg = pf._load_config(str(yaml_path))
    assert cfg["model"]["use_pope"] is True
    assert cfg["audio"]["sample_rate"] == 44100


def test_cli_parses_args():
    """inference_polarformer.py parses positional and optional arguments."""
    import inference_polarformer as cli

    args = cli._parse_args(
        [
            "models/BS_PolarFormer",
            "input.wav",
            "output",
            "4",
            "--device",
            "cpu",
            "--chunk-size",
            "441000",
            "--batch-size",
            "2",
            "--progress-file",
            "/tmp/progress.json",
            "--pipeline-status",
            "/tmp/status.json",
        ]
    )
    assert args.model == "models/BS_PolarFormer"
    assert args.input == "input.wav"
    assert args.output == "output"
    assert args.overlap == 4
    assert args.device == "cpu"
    assert args.chunk_size == 441000
    assert args.batch_size == 2
    assert args.progress_file == "/tmp/progress.json"
    assert args.pipeline_status == "/tmp/status.json"
