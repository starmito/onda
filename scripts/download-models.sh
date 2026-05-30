#!/usr/bin/env bash
# Onda — Descarga de modelos desde HuggingFace
# ============================================================================
# Uso:
#   bash scripts/download-models.sh              # todos los modelos
#   bash scripts/download-models.sh viperx        # solo ViperX (~3.2 GB)
#   bash scripts/download-models.sh onnx          # solo ONNX stems (~200 MB c/u)
# ============================================================================
set -euo pipefail

MODEL_DIR="${MODEL_DIR:-./models}"
HF_BASE="https://huggingface.co"

# ── Helpers ──────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
ok() { echo -e "  ${GREEN}✅${NC} $1"; }

download_file() {
    local url="$1" dst="$2" name="$3"
    if [ -f "$dst" ]; then
        ok "$name ya existe"
        return 0
    fi
    echo -e "  ${CYAN}⬇️  Descargando $name...${NC}"
    if command -v huggingface-cli &>/dev/null; then
        huggingface-cli download "$url" --local-dir "$(dirname "$dst")" --local-dir-use-symlinks=False
    elif command -v wget &>/dev/null; then
        wget -q --show-progress -O "$dst" "$url"
    elif command -v curl &>/dev/null; then
        curl -L --progress-bar -o "$dst" "$url"
    else
        echo -e "  ${YELLOW}⚠${NC}  Ni wget ni curl disponibles. Descarga manual:"
        echo "      $url"
        echo "      → $dst"
        return 1
    fi
    ok "$name descargado"
}

# ── ViperX: BS-Roformer (~3.2 GB) ──────────────────────────────────
download_viperx() {
    local dir="$MODEL_DIR/VR_Models/BS_Roformer_Viperx"
    local repo="viperx/BS-Roformer-Viperx"
    local ckpt="model_bs_roformer_ep_317_sdr_12.9755.ckpt"
    local yaml="model_bs_roformer_ep_317_sdr_12.9755.yaml"

    echo ""
    echo "🎤 ViperX — BS Roformer (~3.2 GB)"

    mkdir -p "$dir"

    # HuggingFace CLI: repo/file → auto
    if command -v huggingface-cli &>/dev/null; then
        echo "   usando huggingface-cli..."
        huggingface-cli download "$repo" --local-dir "$dir" --local-dir-use-symlinks=False
        ok "ViperX completo"
        return 0
    fi

    # Fallback: wget/curl individual files
    download_file "${HF_BASE}/${repo}/resolve/main/${ckpt}" "${dir}/${ckpt}" "${ckpt}"
    download_file "${HF_BASE}/${repo}/resolve/main/${yaml}" "${dir}/${yaml}" "${yaml} (config)"
}

# ── ONNX stems (~50-200 MB cada uno) ───────────────────────────────
download_onnx() {
    echo ""
    echo "🥁 ONNX Stems (~50-200 MB cada uno)"

    local dir="$MODEL_DIR/Demucs_ONNX"
    mkdir -p "$dir"

    local stems="vocals drums bass other"
    local repo="aufr33/demucs-onnx"
    local any_missing=false

    for stem in $stems; do
        local f="htdemucs_ft_${stem}.onnx"
        if [ -f "$dir/$f" ]; then
            ok "$f"
        else
            any_missing=true
        fi
    done

    if $any_missing; then
        echo ""
        echo "   🌐 Fuente: ${HF_BASE}/${repo}"
        echo ""
        if command -v huggingface-cli &>/dev/null; then
            echo "   Descargando todos los stems ONNX..."
            huggingface-cli download "$repo" --local-dir "$dir" --local-dir-use-symlinks=False
        else
            echo "   Para descargar:"
            echo "     pip install huggingface_hub"
            echo "     huggingface-cli download ${repo} --local-dir ${dir}"
        fi
    fi
}

# ── Main ──────────────────────────────────────────────────────────
echo "═══════════════════════════════════════"
echo "📦 Onda — Descarga de modelos"
echo "   Destino: $MODEL_DIR"
echo "═══════════════════════════════════════"

case "${1:-all}" in
    viperx)
        download_viperx
        ;;
    onnx)
        download_onnx
        ;;
    all)
        download_viperx
        download_onnx
        ;;
    *)
        echo "Uso: $0 [viperx|onnx|all]"
        exit 1
        ;;
esac

echo ""
echo "───────────────────────────────────────"
echo -e "${GREEN}✅ Hecho.${NC} Modelos en: $MODEL_DIR/"
echo ""
echo "   Estructura:"
echo "   $MODEL_DIR/"
echo "   ├── VR_Models/"
echo "   │   └── BS_Roformer_Viperx/"
echo "   │       ├── model_bs_roformer_ep_317_sdr_12.9755.ckpt"
echo "   │       └── model_bs_roformer_ep_317_sdr_12.9755.yaml"
echo "   └── Demucs_ONNX/          (opcional)"
echo "       ├── htdemucs_ft_vocals.onnx"
echo "       ├── htdemucs_ft_drums.onnx"
echo "       ├── htdemucs_ft_bass.onnx"
echo "       └── htdemucs_ft_other.onnx"
