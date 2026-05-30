#!/usr/bin/env bash
# Onda — Descarga de modelos desde HuggingFace
# ============================================================================
# Uso:
#   bash scripts/download-models.sh              # todos los modelos
#   bash scripts/download-models.sh viperx        # solo ViperX
#   bash scripts/download-models.sh demucs        # solo Demucs
#   bash scripts/download-models.sh onnx          # solo ONNX stems
# ============================================================================
set -euo pipefail

MODEL_DIR="${MODEL_DIR:-./models}"
HF_BASE="https://huggingface.co"

# ── ViperX: BS-Roformer (~3.2 GB) ──────────────────────────────────
download_viperx() {
    local dst="$MODEL_DIR/VR_Models/BS_Roformer_Viperx"
    if [ -f "$dst/model_bs_roformer_ep_317_sdr_12.9755.ckpt" ]; then
        echo "✅ ViperX ya existe en $dst"
        return 0
    fi
    echo "⬇️  Descargando BS_Roformer_Viperx (~3.2 GB)..."
    mkdir -p "$dst"

    # ViperX model — disponible en:
    # https://huggingface.co/viperx/BS-Roformer-ViperX
    echo "   ℹ️  Descarga manual requerida:"
    echo "   /viperx/BS-Roformer-ViperX/resolve/main/model_bs_roformer_ep_317_sdr_12.9755.ckpt"
    echo "   Guarda el archivo en: $dst/"
    echo ""
    echo "   O usa huggingface-cli:"
    echo "   pip install huggingface_hub"
    echo "   huggingface-cli download viperx/BS-Roformer-ViperX model_bs_roformer_ep_317_sdr_12.9755.ckpt --local-dir $dst"
}

# ── Demucs: htdemucs_ft (~320 MB) ──────────────────────────────────
download_demucs() {
    local dst="$MODEL_DIR/htdemucs_ft"
    if [ -f "$dst/htdemucs_ft.yaml" ]; then
        echo "✅ Demucs ya existe en $dst"
        return 0
    fi
    echo "⬇️  Descargando htdemucs_ft (~320 MB)..."
    echo "   ℹ️  El modelo se descarga automáticamente al ejecutar demucs por primera vez."
    echo "   Para pre-descargarlo:"
    echo "   python3 -c \"import demucs; demucs.api.Separator(model='htdemucs_ft')\""
}

# ── ONNX stems (~50-200 MB cada uno) ───────────────────────────────
download_onnx() {
    echo "⬇️  Modelos ONNX para stems individuales:"
    echo "   ℹ️  Estos modelos van en $MODEL_DIR/ y se detectan automáticamente."
    echo "   Extensiones soportadas: .ckpt, .pth, .th, .onnx"
    echo ""
    echo "   Fuentes comunes:"
    echo "   - Kim_Vocal:  https://huggingface.co/KimberleyJSN/Kim_Vocal_2_ONNX"
    echo "   - UVR-MDX-NET: https://huggingface.co/aufr33/uvr-mdx-net"
    echo "   - Demucs ONNX: https://huggingface.co/aufr33/demucs-onnx"
}

# ── Main ──────────────────────────────────────────────────────────
case "${1:-all}" in
    viperx) download_viperx ;;
    demucs) download_demucs ;;
    onnx)   download_onnx ;;
    all)
        download_viperx
        echo ""
        download_demucs
        echo ""
        download_onnx
        ;;
    *)
        echo "Uso: $0 [viperx|demucs|onnx|all]"
        exit 1
        ;;
esac

echo ""
echo "✅ Hecho. Modelos en: $MODEL_DIR/"
echo "   Estructura esperada:"
echo "   $MODEL_DIR/VR_Models/BS_Roformer_Viperx/model_bs_roformer_ep_317_sdr_12.9755.ckpt"
echo "   $MODEL_DIR/htdemucs_ft/htdemucs_ft.yaml"
