#!/usr/bin/env python3
"""
Importa defaults de inferencia desde los YAML de UVR en .87 y genera
archivos JSON de configuración por modelo en model_configs/.

Cada modelo Roformer/MelBand tiene un archivo YAML con sus parámetros reales
en la sección "inference": dim_t, num_overlap, batch_size.

Usage:
    python3 backend/scripts/import_uvr_defaults.py [--dry-run]

Requiere acceso SSH a .87 para leer los YAML en /mnt/almacen/onda/models/VR_Models/.
"""
import json
import os
import subprocess
import sys
import yaml

# ── Paths ──────────────────────────────────────────────────────────
MODELS_ROOT = "/mnt/almacen/onda/models"
VR_MODELS = os.path.join(MODELS_ROOT, "VR_Models")
PROJECT_ROOT = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "..")
)
OUTPUT_DIR = os.path.join(PROJECT_ROOT, "model_configs")

# ── YAML → JSON mapping ────────────────────────────────────────────
# Maps model subdirectory names to (ckpt_name, yaml_name) pairs.
# If no YAML found, falls back to manual defaults below.
VR_MODEL_MAP = {
    "BS_Roformer_Viperx": {
        "ckpt": "model_bs_roformer_ep_317_sdr_12.9755.ckpt",
        "yaml": "model_bs_roformer_ep_317_sdr_12.9755.yaml",
    },
    "BS_PolarFormer": {
        "ckpt": "model_bs_polarformer_float16.ckpt",
        "yaml": "model_bs_polarformer_float16.yaml",
    },
    "Viperx_Other": {
        "ckpt": "model_bs_roformer_ep_937_sdr_10.5309.ckpt",
        "yaml": "model_bs_roformer_ep_937_sdr_10.5309.yaml",
    },
    "MelBand_Roformer_KJ": {
        "ckpt": "MelBandRoformer.ckpt",
        # YAML has a different name than the ckpt
        "yaml": "config_vocals_mel_band_roformer_kj.yaml",
    },
    "MelBand_Karaoke": {
        # Multiple ckpt/yaml pairs in this directory
        "pairs": [
            ("big_beta5e.ckpt", "big_beta5e.yaml"),
            ("big_beta6x.ckpt", "big_beta6x.yaml"),
            ("big_beta7.ckpt", "big_beta7.yaml"),
            ("melband_roformer_big_beta1.ckpt", "config_melbandroformer_big.yaml"),
            ("melband_roformer_big_beta4.ckpt", "config_melbandroformer_big_beta4.yaml"),
            # melband_roformer_big_beta3.ckpt has no dedicated YAML;
            # uses same defaults as beta1 (config_melbandroformer_big.yaml)
            ("melband_roformer_big_beta3.ckpt", None),
        ],
    },
}

# ── Manual overrides for models without YAML ───────────────────────
MANUAL_DEFAULTS = {
    # MelBand models without explicit YAML
    "melband_roformer_big_beta3": {
        "segment_size": 1101,   # same as beta1 (from config_melbandroformer_big.yaml)
        "overlap": 2,
        "batch_size": 1,
    },
    # MDX models — no YAML, use conservative defaults
    "Kim_Vocal_1": {
        "segment_size": 256,
        "overlap": 0.25,
        "batch_size": 0,
    },
    "Kim_Vocal_2": {
        "segment_size": 256,
        "overlap": 0.25,
        "batch_size": 0,
    },
    "UVR_MDXNET_Main": {
        "segment_size": 256,
        "overlap": 0.25,
        "batch_size": 0,
    },
    # Demucs — htdemucs_ft unified model (not per-stem ONNX)
    "htdemucs_ft": {
        "segment_size": 0,      # usa su propio default
        "overlap": 0.25,
        "batch_size": 0,
    },
}

# ── SSH helpers ────────────────────────────────────────────────────
def ssh_cat(host: str, path: str) -> str:
    """Read a remote file via SSH."""
    cmd = ["ssh", host, "cat", path]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    if result.returncode != 0:
        raise RuntimeError(
            f"SSH cat {path} on {host} failed: {result.stderr.strip()}"
        )
    return result.stdout


def read_yaml_remote(host: str, path: str) -> dict:
    """Read a remote YAML file and parse it."""
    raw = ssh_cat(host, path)
    return yaml.safe_load(raw)


# ── Config generation ──────────────────────────────────────────────
def build_config(
    segment_size: int,
    overlap: float,
    batch_size: int,
    chunk_size: int = 0,
    device: str = "cuda",
) -> dict:
    """Build a ModelConfig dict matching the Go ModelConfig struct."""
    return {
        "segment_size": segment_size,
        "overlap": overlap,
        "chunk_size": chunk_size,
        "batch_size": batch_size,
        "device": device,
    }


def extract_inference(yaml_data: dict) -> dict | None:
    """Extract inference params from a parsed YAML."""
    inference = yaml_data.get("inference")
    if not inference:
        return None
    return {
        "dim_t": inference.get("dim_t"),
        "num_overlap": inference.get("num_overlap"),
        "batch_size": inference.get("batch_size"),
    }


def model_name_from_ckpt(ckpt_filename: str) -> str:
    """Strip extension from a ckpt filename to get the model name."""
    return os.path.splitext(ckpt_filename)[0]


# ── Main ────────────────────────────────────────────────────────────
def main():
    dry_run = "--dry-run" in sys.argv
    host = ".87"
    generated = []

    os.makedirs(OUTPUT_DIR, exist_ok=True)

    # Process VR_Models with explicit YAML mappings
    for subdir, info in sorted(VR_MODEL_MAP.items()):
        pairs = info.get("pairs")
        if pairs is None:
            pairs = [(info["ckpt"], info["yaml"])]

        for ckpt_file, yaml_file in pairs:
            model_name = model_name_from_ckpt(ckpt_file)

            if yaml_file is None:
                # No YAML — use manual defaults
                manual = MANUAL_DEFAULTS.get(model_name)
                if manual is None:
                    print(f"  SKIP {model_name}: no YAML and no manual defaults")
                    continue
                config = build_config(
                    segment_size=manual["segment_size"],
                    overlap=manual["overlap"],
                    batch_size=manual["batch_size"],
                )
                source = "manual defaults"
            else:
                yaml_path = os.path.join(VR_MODELS, subdir, yaml_file)
                try:
                    data = read_yaml_remote(host, yaml_path)
                    params = extract_inference(data)
                    if params is None:
                        print(f"  SKIP {model_name}: no 'inference' section in {yaml_path}")
                        continue
                    config = build_config(
                        segment_size=params["dim_t"],
                        overlap=float(params["num_overlap"]),
                        batch_size=params["batch_size"],
                    )
                    source = f"YAML: {yaml_path}"
                except Exception as e:
                    print(f"  WARN {model_name}: failed to read YAML — {e}")
                    continue

            json_path = os.path.join(OUTPUT_DIR, f"{model_name}.json")
            if dry_run:
                print(f"  [DRY-RUN] {model_name} ← {source}")
                print(f"    → {json.dumps(config)}")
            else:
                with open(json_path, "w") as f:
                    json.dump(config, f, indent=2)
                    f.write("\n")
                print(f"  WRITE {model_name}.json ← {source}")

            generated.append(model_name)

    # Process models from manual defaults that weren't already handled
    for model_name, manual in sorted(MANUAL_DEFAULTS.items()):
        if model_name in generated:
            continue
        config = build_config(
            segment_size=manual["segment_size"],
            overlap=manual["overlap"],
            batch_size=manual["batch_size"],
        )
        json_path = os.path.join(OUTPUT_DIR, f"{model_name}.json")
        if dry_run:
            print(f"  [DRY-RUN] {model_name} ← manual defaults")
            print(f"    → {json.dumps(config)}")
        else:
            with open(json_path, "w") as f:
                json.dump(config, f, indent=2)
                f.write("\n")
            print(f"  WRITE {model_name}.json ← manual defaults")
        generated.append(model_name)

    print(f"\nTotal: {len(generated)} model configs")
    if dry_run:
        print("(dry-run — no files written)")
    else:
        print(f"Output: {OUTPUT_DIR}/")


if __name__ == "__main__":
    main()
