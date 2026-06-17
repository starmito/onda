#!/usr/bin/env python3
"""Benchmark all separation methods with same audio input."""
import subprocess
import time
import json
import os

CONTAINER = os.environ.get('CONTAINER_NAME', 'onda')
FIXTURE = os.environ.get('FIXTURE_DIR', '/app/tests/integration/fixtures') + '/chirp_5s.flac'
DURATION = 5.0
MDX_MODEL = os.environ.get('MDX_MODEL_PATH', '/app/models/MDX_Net_Models/Kim_Vocal_2.onnx')

BENCHMARKS = {
    "mdx-net": [
        "docker", "exec", CONTAINER, "python3",
        "inference/inference_mdx.py", MDX_MODEL, FIXTURE,
        "/tmp/bench-mdx/"
    ],
    "demucs-pytorch": [
        "docker", "exec", CONTAINER, "demucs",
        "-n", "htdemucs_ft", "--two-stems", "vocals",
        "-o", "/tmp/bench-demucs-pt/", FIXTURE
    ],
}

results = {}

for name, cmd in BENCHMARKS.items():
    print(f"Benchmarking {name}...")
    start = time.time()
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=180)
    elapsed = time.time() - start
    success = result.returncode == 0
    rtf = DURATION / elapsed if elapsed > 0 else float('inf')

    results[name] = {
        "rtf": round(rtf, 1),
        "elapsed_s": round(elapsed, 1),
        "success": success,
        "error": result.stderr[:200] if not success else None,
    }
    status = "✅" if success else "❌"
    print(f"  {status} {name}: {rtf:.1f}x realtime ({elapsed:.1f}s)")

out_path = os.path.join(os.path.dirname(__file__), "benchmark_results.json")
with open(out_path, "w") as f:
    json.dump(results, f, indent=2)

print(f"\n=== RESULTS ===\n{json.dumps(results, indent=2)}")
print(f"\nSaved to {out_path}")
