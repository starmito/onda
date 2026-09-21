"""Tests for the tolerant YAML reader and model manifest generator."""

import json
from pathlib import Path

import pytest

from onda.manifest import (
    detect_model_type,
    extract_flags,
    extract_stems,
    generate_manifest,
    load_yaml_tolerant,
    regenerate_manifests,
)


REPO_ROOT = Path(__file__).resolve().parents[2]
MODELS_DIR = REPO_ROOT / "data" / "models"


def test_load_yaml_tolerant_reads_python_tuple(tmp_path: Path) -> None:
    yaml_path = tmp_path / "tuple.yaml"
    yaml_path.write_text(
        "model:\n"
        "  freqs_per_bands: !!python/tuple [2, 2, 4]\n"
        "  multi_stft_resolutions_window_sizes: !!python/tuple\n"
        "    - 4096\n"
        "    - 2048\n",
        encoding="utf-8",
    )
    data = load_yaml_tolerant(yaml_path)
    assert data["model"]["freqs_per_bands"] == [2, 2, 4]
    assert data["model"]["multi_stft_resolutions_window_sizes"] == [4096, 2048]


def test_load_yaml_tolerant_does_not_execute_code(tmp_path: Path) -> None:
    yaml_path = tmp_path / "safe.yaml"
    yaml_path.write_text(
        "inference:\n"
        "  num_overlap: 4\n"
        "  batch_size: 1\n",
        encoding="utf-8",
    )
    data = load_yaml_tolerant(yaml_path)
    assert data["inference"]["num_overlap"] == 4
    assert data["inference"]["batch_size"] == 1


@pytest.mark.parametrize(
    "cfg, checkpoint, expected",
    [
        ({"model": {"type": "bs_roformer"}}, None, "bs_roformer"),
        ({"model": {"freqs_per_bands": [2, 4]}}, None, "bs_roformer"),
        ({"model": {"band_SR": [0.1]}}, None, "scnet"),
        ({"model": {"num_scales": 5}}, None, "mdx23c"),
        ({}, Path("/models/SCNet_MUSDB18/foo.ckpt"), "scnet"),
        ({}, Path("/models/MDX23C_D1581/foo.ckpt"), "mdx23c"),
        ({}, Path("/models/Kim_Vocal_1/Kim_Vocal_1.onnx"), "mdx_net"),
        ({}, None, "unknown"),
    ],
)
def test_detect_model_type(cfg, checkpoint, expected) -> None:
    assert detect_model_type(cfg, checkpoint) == expected


@pytest.mark.parametrize(
    "cfg, json_cfg, expected_stems, expected_target, expected_declared",
    [
        (
            {"training": {"instruments": ["bass", "drums", "vocals"]}},
            None,
            ["bass", "drums", "vocals"],
            None,
            ["bass", "drums", "vocals"],
        ),
        (
            {"training": {"instruments": ["bass", "drums"], "target_instrument": "drums"}},
            None,
            ["drums", "instrumental"],
            "drums",
            ["bass", "drums"],
        ),
        (
            {"model": {"sources": ["drums", "bass"]}},
            None,
            ["drums", "bass"],
            None,
            ["drums", "bass"],
        ),
        (
            {},
            {"target_instrument": "Vocals"},
            ["vocals", "instrumental"],
            "vocals",
            ["vocals"],
        ),
        ({}, None, [], None, []),
    ],
)
def test_extract_stems(cfg, json_cfg, expected_stems, expected_target, expected_declared) -> None:
    result = extract_stems(cfg, json_cfg)
    assert result["stems"] == expected_stems
    assert result["target"] == expected_target
    assert result["num_stems"] == len(expected_stems)
    assert result["declared_instruments"] == expected_declared
    assert result["declared_num_stems"] == len(expected_declared)


def test_extract_flags_extracts_inference_and_demucs() -> None:
    cfg = {
        "inference": {
            "dim_t": 256,
            "num_overlap": 4,
            "batch_size": 1,
            "chunk_size": 0,
            "normalize": False,
        },
        "demucs": {"shifts": 0, "segment": 0, "jobs": 0},
    }
    flags = extract_flags(cfg)
    assert flags["segment_size"]["default"] == 256
    assert flags["num_overlap"]["default"] == 4
    assert flags["overlap"]["default"] == 0.25
    assert flags["batch_size"]["default"] == 1
    assert flags["chunk_size"]["default"] == 0
    assert flags["device"]["default"] == "cuda"
    assert flags["device"]["choices"] == ["cuda", "cpu"]
    assert flags["shifts"]["default"] == 0
    assert flags["segment"]["default"] == 0.0
    assert flags["jobs"]["default"] == 0
    # Raw architecture keys live in metadata, not in editable flags.
    assert "dim_t" not in flags
    assert "normalize" not in flags


def test_generate_manifest_with_valid_config(tmp_path: Path) -> None:
    model_dir = tmp_path / "MyModel"
    model_dir.mkdir()
    (model_dir / "MyModel.ckpt").write_text("dummy")
    (model_dir / "MyModel.yaml").write_text(
        "model:\n"
        "  type: bs_roformer\n"
        "  num_stems: 2\n"
        "training:\n"
        "  instruments: [vocals, other]\n"
        "  target_instrument: vocals\n"
        "inference:\n"
        "  num_overlap: 2\n"
        "  batch_size: 1\n",
        encoding="utf-8",
    )
    manifest = generate_manifest(model_dir)
    assert manifest["name"] == "MyModel"
    assert manifest["type"] == "bs_roformer"
    assert manifest["stems"]["stems"] == ["vocals", "instrumental"]
    assert manifest["stems"]["target"] == "vocals"
    assert manifest["stems"]["num_stems"] == 2
    assert manifest["stems"]["declared_instruments"] == ["vocals", "other"]
    assert manifest["stems"]["declared_num_stems"] == 2
    assert manifest["flags"]["segment_size"]["default"] == 512
    assert manifest["flags"]["num_overlap"]["default"] == 2
    assert manifest["flags"]["batch_size"]["default"] == 1
    # dim_t was not declared in this synthetic YAML, so it is not invented in metadata.
    assert "dim_t" not in manifest["metadata"].get("inference", {})
    assert manifest["source_yaml"] == "MyModel.yaml"
    assert manifest["checkpoint"] == "MyModel.ckpt"
    assert "warnings" not in manifest


def test_generate_manifest_with_broken_yaml(tmp_path: Path) -> None:
    model_dir = tmp_path / "Broken"
    model_dir.mkdir()
    (model_dir / "Broken.ckpt").write_text("dummy")
    (model_dir / "Broken.yaml").write_text(
        "inference: [not a mapping",
        encoding="utf-8",
    )
    manifest = generate_manifest(model_dir)
    assert manifest["name"] == "Broken"
    assert manifest["type"] == "unknown"
    assert manifest["stems"]["stems"] == []
    assert manifest["source_yaml"] == "Broken.yaml"
    assert "warnings" in manifest


def test_generate_manifest_without_yaml(tmp_path: Path) -> None:
    model_dir = tmp_path / "OnnxOnly"
    model_dir.mkdir()
    (model_dir / "OnnxOnly.onnx").write_text("dummy")
    (model_dir / "OnnxOnly.json").write_text(
        '{"target_instrument": "Vocals"}',
        encoding="utf-8",
    )
    manifest = generate_manifest(model_dir)
    assert manifest["name"] == "OnnxOnly"
    assert manifest["type"] == "mdx_net"
    assert manifest["stems"]["stems"] == ["vocals", "instrumental"]
    assert manifest["stems"]["declared_instruments"] == ["vocals"]
    assert manifest["stems"]["declared_num_stems"] == 1
    assert manifest["source_yaml"] is None
    assert "No YAML config found" in manifest["warnings"]


def test_regenerate_manifests_walks_models_root(tmp_path: Path) -> None:
    models_dir = tmp_path / "models"
    cat_dir = models_dir / "VR_Models"
    model_dir = cat_dir / "Test"
    model_dir.mkdir(parents=True)
    (model_dir / "Test.ckpt").write_text("dummy", encoding="utf-8")
    (model_dir / "Test.yaml").write_text(
        "training:\n  instruments: [vocals]\n"
        "inference:\n  num_overlap: 2\n",
        encoding="utf-8",
    )
    results = regenerate_manifests(models_dir, write=True)
    assert len(results) == 1
    assert results[0]["category"] == "VR_Models"
    assert results[0]["model"] == "Test"
    assert (model_dir / "model.manifest.json").exists()


class TestRealModels:
    """Read-only assertions against the real models shipped in the repo."""

    @pytest.mark.parametrize(
        "category, model, expected_type, expected_stems, expected_target, expected_declared",
        [
            (
                "VR_Models",
                "BS_Roformer_SW_6stem",
                "bs_roformer",
                ["bass", "drums", "other", "vocals", "guitar", "piano"],
                None,
                ["bass", "drums", "other", "vocals", "guitar", "piano"],
            ),
            (
                "VR_Models",
                "BS_Roformer_Viperx",
                "bs_roformer",
                ["vocals", "instrumental"],
                "vocals",
                ["vocals"],
            ),
            (
                "VR_Models",
                "MDX23C_D1581",
                "mdx23c",
                ["vocals", "instrumental"],
                None,
                ["vocals", "instrumental"],
            ),
            (
                "VR_Models",
                "SCNet_MUSDB18",
                "scnet",
                ["drums", "bass", "other", "vocals"],
                None,
                ["drums", "bass", "other", "vocals"],
            ),
            (
                "MDX_Net_Models",
                "Kim_Vocal_1",
                "mdx_net",
                ["vocals", "instrumental"],
                "vocals",
                ["vocals"],
            ),
        ],
    )
    def test_real_model_manifest(
        self,
        category: str,
        model: str,
        expected_type: str,
        expected_stems: list[str],
        expected_target: str | None,
        expected_declared: list[str],
    ) -> None:
        model_dir = MODELS_DIR / category / model
        if not model_dir.exists():
            pytest.skip(f"Real model not present: {model_dir}")
        manifest = generate_manifest(model_dir)
        assert manifest["name"] == model
        assert manifest["type"] == expected_type
        assert manifest["stems"]["stems"] == expected_stems
        assert manifest["stems"]["target"] == expected_target
        assert manifest["stems"]["num_stems"] == len(expected_stems)
        assert manifest["stems"]["declared_instruments"] == expected_declared
        assert manifest["stems"]["declared_num_stems"] == len(expected_declared)
        assert manifest["source_yaml"] is not None
        assert "generated_at" in manifest

    def test_python_tuple_fields_are_readable(self) -> None:
        """The SW 6-stem YAML used to fail on !!python/tuple tags."""
        yaml_path = (
            MODELS_DIR
            / "VR_Models"
            / "BS_Roformer_SW_6stem"
            / "BS-Rofo-SW-Fixed.yaml"
        )
        if not yaml_path.exists():
            pytest.skip("SW 6-stem YAML not present")
        data = load_yaml_tolerant(yaml_path)
        assert isinstance(data["model"]["freqs_per_bands"], list)
        assert isinstance(data["model"]["multi_stft_resolutions_window_sizes"], list)
        assert isinstance(data["augmentations"]["mixup_probs"], list)
        assert data["model"]["num_stems"] == 6
        assert data["training"]["instruments"] == [
            "bass",
            "drums",
            "other",
            "vocals",
            "guitar",
            "piano",
        ]
