"""Tests for onda.viperx covering pure logic paths.

These tests do not need a GPU or the full PyTorch/librosa stack: conftest.py
injects lightweight import mocks, and each test monkey-patches the parts of the
module it exercises.
"""

import os
import sys
from types import SimpleNamespace
from unittest import mock

import pytest
import yaml


@pytest.fixture
def make_args(tmp_path):
    """Build a SimpleNamespace for run_viperx."""

    def _make(
        model=None,
        config=None,
        input_file=None,
        output="viperx_out",
        overlap=8,
        device="cpu",
    ):
        return SimpleNamespace(
            model=str(model) if model else str(tmp_path / "model.ckpt"),
            config=str(config) if config else None,
            input=str(input_file) if input_file else str(tmp_path / "input.wav"),
            output=str(tmp_path / output),
            overlap=overlap,
            device=device,
        )

    return _make


def write_config(path, model_type):
    """Write a minimal valid RoFormer config file."""
    cfg = {
        "model": model_type,
        "audio": {"hop_length": 441},
        "inference": {"dim_t": 801, "batch_size": 1},
        "training": {"instruments": ["vocals", "other"]},
    }
    with open(path, "w", encoding="utf-8") as f:
        yaml.safe_dump(cfg, f)


def test_run_viperx_exits_when_model_missing(make_args, capsys):
    """run_viperx must exit 1 when the model checkpoint is missing."""
    args = make_args(model="/nonexistent/model.ckpt")
    import onda.viperx as vx

    with pytest.raises(SystemExit) as exc:
        vx.run_viperx(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "model not found" in captured.out.lower()


def test_run_viperx_autodetects_config_same_prefix(make_args, tmp_path):
    """run_viperx auto-detects a .yaml with the same basename as the model."""
    model_path = tmp_path / "my_model.ckpt"
    model_path.write_bytes(b"ckpt")
    write_config(tmp_path / "my_model.yaml", {"num_bands": 4})
    # A different yaml should not be preferred when the prefix matches.
    write_config(tmp_path / "other.yaml", {"freqs_per_bands": [1, 2]})

    args = make_args(model=model_path)
    import onda.viperx as vx
    import lib_v5.mel_band_roformer as mb
    import lib_v5.bs_roformer as bs

    detected = None
    orig_open = open

    def tracking_open(path, *args, **kwargs):
        nonlocal detected
        if str(path).endswith(".yaml"):
            detected = path
        return orig_open(path, *args, **kwargs)

    def raise_after_config(*args, **kwargs):
        raise RuntimeError("config-detected")

    with mock.patch("builtins.open", side_effect=tracking_open), mock.patch.object(
        mb, "MelBandRoformer", side_effect=raise_after_config
    ), mock.patch.object(bs, "BSRoformer", side_effect=raise_after_config), mock.patch(
        "onda.viperx.sf.write"
    ):
        with pytest.raises(RuntimeError, match="config-detected"):
            vx.run_viperx(args)

    assert detected is not None
    assert os.path.basename(str(detected)) == "my_model.yaml"


def test_run_viperx_autodetects_any_yaml_when_prefix_missing(make_args, tmp_path):
    """When no prefix-matching yaml exists, the first yaml is selected."""
    model_path = tmp_path / "model.ckpt"
    model_path.write_bytes(b"ckpt")
    write_config(tmp_path / "only_config.yaml", {"num_bands": 4})

    args = make_args(model=model_path)
    import onda.viperx as vx
    import lib_v5.mel_band_roformer as mb
    import lib_v5.bs_roformer as bs

    detected = None
    orig_open = open

    def tracking_open(path, *args, **kwargs):
        nonlocal detected
        if str(path).endswith(".yaml"):
            detected = path
        return orig_open(path, *args, **kwargs)

    def raise_after_config(*args, **kwargs):
        raise RuntimeError("config-detected")

    with mock.patch("builtins.open", side_effect=tracking_open), mock.patch.object(
        mb, "MelBandRoformer", side_effect=raise_after_config
    ), mock.patch.object(bs, "BSRoformer", side_effect=raise_after_config), mock.patch(
        "onda.viperx.sf.write"
    ):
        with pytest.raises(RuntimeError, match="config-detected"):
            vx.run_viperx(args)

    assert detected is not None
    assert os.path.basename(str(detected)) == "only_config.yaml"


def test_run_viperx_exits_when_no_config_found(make_args, tmp_path, capsys):
    """run_viperx exits 1 when no .yaml config is found next to the model."""
    model_path = tmp_path / "model.ckpt"
    model_path.write_bytes(b"ckpt")
    args = make_args(model=model_path)
    import onda.viperx as vx

    with pytest.raises(SystemExit) as exc:
        vx.run_viperx(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "no .yaml config found" in captured.out.lower()


def test_run_viperx_exits_on_unknown_model_type(make_args, tmp_path, capsys):
    """run_viperx exits 1 when the config does not match a known model type."""
    model_path = tmp_path / "model.ckpt"
    model_path.write_bytes(b"ckpt")
    write_config(tmp_path / "model.yaml", {"unknown_key": True})

    args = make_args(model=model_path)
    import onda.viperx as vx

    with pytest.raises(SystemExit) as exc:
        vx.run_viperx(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "unknown model type" in captured.out.lower()
