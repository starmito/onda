#!/usr/bin/env python3
"""tools/bench_compare.py — Compara dos benchmarks de Onda y emite veredicto.

Uso:
    python3 tools/bench_compare.py base.json nuevo.json [opciones]

Salida:
    0 = igual (dentro de umbrales)
    1 = cambio detectado
    2 = no comparable (faltan métricas esenciales)
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class Thresholds:
    time_pct: float
    vram_pct: float
    energy_db: float


@dataclass
class Metric:
    name: str
    base: float | None
    new: float | None
    unit: str
    threshold: float
    kind: str  # "pct" o "abs"


def load_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def get_versions(data: dict[str, Any]) -> dict[str, str]:
    return {k: str(v) for k, v in data.get("versions", {}).items()}


def get_metric(data: dict[str, Any], *keys: str) -> Any:
    node = data
    for key in keys:
        if not isinstance(node, dict):
            return None
        node = node.get(key)
    return node


def collect_metrics(base: dict[str, Any], new: dict[str, Any]) -> list[Metric]:
    metrics: list[Metric] = []

    # Tiempo total
    total_base = get_metric(base, "summary", "total_seconds")
    total_new = get_metric(new, "summary", "total_seconds")
    metrics.append(
        Metric(
            name="Tiempo total",
            base=_to_float(total_base),
            new=_to_float(total_new),
            unit="s",
            threshold=0.0,  # se asigna luego desde thresholds
            kind="pct",
        )
    )

    # VRAM
    for vram_key in ("vram_peak_mb", "vram_mean_mb"):
        v_base = get_metric(base, "summary", vram_key)
        v_new = get_metric(new, "summary", vram_key)
        label = "VRAM Peak (MiB)" if vram_key == "vram_peak_mb" else "VRAM Mean (MiB)"
        metrics.append(
            Metric(
                name=label,
                base=_to_float(v_base),
                new=_to_float(v_new),
                unit="MiB",
                threshold=0.0,
                kind="pct",
            )
        )

    # Tiempo por paso
    steps_base = get_metric(base, "summary", "step_seconds") or {}
    steps_new = get_metric(new, "summary", "step_seconds") or {}
    all_steps = sorted(set(steps_base.keys()) | set(steps_new.keys()))
    for step in all_steps:
        metrics.append(
            Metric(
                name=f"Paso: {step}",
                base=_to_float(steps_base.get(step)),
                new=_to_float(steps_new.get(step)),
                unit="s",
                threshold=0.0,
                kind="pct",
            )
        )

    # Energía por stem
    stems_base = _stem_energy_map(get_metric(base, "summary", "stems") or [])
    stems_new = _stem_energy_map(get_metric(new, "summary", "stems") or [])
    all_stems = sorted(set(stems_base.keys()) | set(stems_new.keys()))
    for stem in all_stems:
        metrics.append(
            Metric(
                name=f"Energía stem: {stem}",
                base=stems_base.get(stem),
                new=stems_new.get(stem),
                unit="dBFS",
                threshold=0.0,
                kind="abs",
            )
        )

    return metrics


def _to_float(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def _stem_energy_map(stems: list[dict[str, Any]]) -> dict[str, float]:
    result: dict[str, float] = {}
    for stem in stems:
        name = stem.get("name")
        energy = _to_float(stem.get("energy_rms_db"))
        if name and energy is not None:
            result[str(name)] = energy
    return result


def delta_pct(base: float, new: float) -> float:
    if base == 0:
        return float("inf") if new != 0 else 0.0
    return ((new - base) / base) * 100.0


def apply_thresholds(metrics: list[Metric], thresholds: Thresholds) -> list[Metric]:
    applied: list[Metric] = []
    for m in metrics:
        if m.kind == "pct":
            if m.unit == "MiB":
                t = thresholds.vram_pct
            else:
                t = thresholds.time_pct
        else:
            t = thresholds.energy_db
        applied.append(Metric(m.name, m.base, m.new, m.unit, t, m.kind))
    return applied


def evaluate(metrics: list[Metric]) -> tuple[str, int, list[str]]:
    reasons: list[str] = []
    missing = False

    for m in metrics:
        if m.base is None or m.new is None:
            missing = True
            continue
        if m.kind == "pct":
            d = delta_pct(m.base, m.new)
            if abs(d) > m.threshold:
                reasons.append(
                    f"{m.name}: {m.base:.2f} → {m.new:.2f} {m.unit} ({d:+.1f}%, umbral ±{m.threshold:.1f}%)"
                )
        else:
            d = m.new - m.base
            if abs(d) > m.threshold:
                reasons.append(
                    f"{m.name}: {m.base:.2f} → {m.new:.2f} {m.unit} ({d:+.2f} {m.unit}, umbral ±{m.threshold:.1f} {m.unit})"
                )

    if missing:
        return "no comparable", 2, reasons + ["Faltan métricas en uno de los ficheros."]
    if reasons:
        return "cambio", 1, reasons
    return "igual", 0, ["Todas las métricas dentro de los umbrales."]


def print_table(metrics: list[Metric]) -> None:
    print(f"{'Métrica':<30} {'Base':>14} {'Nuevo':>14} {'Δ':>12} {'Umbral':>12} {'Estado':>10}")
    print("-" * 100)
    for m in metrics:
        if m.base is None or m.new is None:
            base_s = _fmt(m.base, m.unit)
            new_s = _fmt(m.new, m.unit)
            print(f"{m.name:<30} {base_s:>14} {new_s:>14} {'N/A':>12} {'N/A':>12} {'N/A':>10}")
            continue
        if m.kind == "pct":
            d = delta_pct(m.base, m.new)
            d_s = f"{d:+.1f}%"
            u_s = f"±{m.threshold:.1f}%"
            ok = "OK" if abs(d) <= m.threshold else "CAMBIO"
        else:
            d = m.new - m.base
            d_s = f"{d:+.2f}"
            u_s = f"±{m.threshold:.1f}"
            ok = "OK" if abs(d) <= m.threshold else "CAMBIO"
        print(
            f"{m.name:<30} {_fmt(m.base, m.unit):>14} {_fmt(m.new, m.unit):>14} "
            f"{d_s:>12} {u_s:>12} {ok:>10}"
        )


def _fmt(value: float | None, unit: str) -> str:
    if value is None:
        return "N/A"
    return f"{value:.2f} {unit}"


def version_warnings(base: dict[str, Any], new: dict[str, Any]) -> list[str]:
    warnings: list[str] = []
    base_v = get_versions(base)
    new_v = get_versions(new)
    keys = sorted(set(base_v.keys()) | set(new_v.keys()))
    for key in keys:
        b = base_v.get(key, "N/A")
        n = new_v.get(key, "N/A")
        if b != n:
            warnings.append(f"{key}: base={b} ≠ nuevo={n}")
    return warnings


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Compara dos benchmarks de Onda.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Códigos de salida: 0 = igual, 1 = cambio, 2 = no comparable.",
    )
    parser.add_argument("base", type=Path, help="Fichero JSON de referencia.")
    parser.add_argument("new", type=Path, help="Fichero JSON a comparar.")
    parser.add_argument(
        "--time-threshold",
        type=float,
        default=5.0,
        help="Umbral para tiempos en %% (default: 5.0).",
    )
    parser.add_argument(
        "--vram-threshold",
        type=float,
        default=10.0,
        help="Umbral para VRAM en %% (default: 10.0).",
    )
    parser.add_argument(
        "--energy-threshold",
        type=float,
        default=2.0,
        help="Umbral para energía en dB (default: 2.0).",
    )

    args = parser.parse_args()

    if not args.base.is_file():
        print(f"Error: no existe {args.base}", file=sys.stderr)
        return 2
    if not args.new.is_file():
        print(f"Error: no existe {args.new}", file=sys.stderr)
        return 2

    base_data = load_json(args.base)
    new_data = load_json(args.new)

    thresholds = Thresholds(
        time_pct=args.time_threshold,
        vram_pct=args.vram_threshold,
        energy_db=args.energy_threshold,
    )

    metrics = collect_metrics(base_data, new_data)
    metrics = apply_thresholds(metrics, thresholds)

    warnings = version_warnings(base_data, new_data)
    if warnings:
        print("⚠️  Aviso: las versiones no coinciden entre ambos benchmarks:")
        for w in warnings:
            print(f"   - {w}")
        print()

    print_table(metrics)
    print()

    verdict, code, details = evaluate(metrics)
    print(f"Veredicto: {verdict.upper()}")
    for d in details:
        print(f"  • {d}")
    print(f"\nCódigo de salida: {code}")

    return code


if __name__ == "__main__":
    sys.exit(main())
