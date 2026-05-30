#!/usr/bin/env python3
"""onda-api.py — Minimal HTTP API server for Onda GUI.
Replaces bash CGI scripts. Listens on localhost:3001, proxied by nginx.

Fixes from v1 (feature/mini-daw):
  - Delete: docker exec onda rm (permissions, root-owned files)
  - Output: capped + pre-filtered (avoid rglob on huge dirs)
  - Separate: start_new_session=True (survive API restart)
  - Upload: validate filename + extension
"""

import os
import json
import struct
import subprocess
import urllib.parse
import wave
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path

MODELS_DIR = Path("/models")
INPUT_DIR = Path("/input")
OUTPUT_DIR = Path("/output")
UPLOAD_DIR = Path("/input")

ALLOWED_EXTENSIONS = {".wav", ".mp3", ".flac", ".ogg", ".aac", ".m4a", ".aiff"}
AUDIO_EXTENSIONS = {".wav", ".mp3", ".flac"}

MODEL_DIRS = {
    "vocal": [MODELS_DIR / "VR_Models", MODELS_DIR / "MDX_Net_Models", MODELS_DIR / "RoFormer_Models"],
    "stems": [MODELS_DIR / "Demucs_Models", MODELS_DIR / "Demucs_ONNX"],
}


class OndaAPI(BaseHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split("?")[0]
        qs = urllib.parse.parse_qs(self.path.split("?")[1] if "?" in self.path else "")

        try:
            if path == "/api/status":
                self._json(self._status())
            elif path == "/api/models":
                self._json(self._models(qs))
            elif path == "/api/input":
                self._json(self._input_files())
            elif path == "/api/output":
                self._json(self._output_files(qs))
            elif path == "/api/health":
                self._json(self._health())
            elif path == "/api/peaks":
                self._json(self._peaks(qs))
            elif path == "/api/delete":
                self._json(self._delete(qs))
            else:
                self._error(404, "Not found")
        except Exception as e:
            self._error(500, str(e))

    def do_POST(self):
        path = self.path.split("?")[0]
        content_len = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_len) if content_len else b""

        try:
            if path == "/api/separate":
                self._json(self._separate(body))
            elif path == "/api/upload":
                self._json(self._upload(body))
            elif path == "/api/clear":
                self._json(self._clear())
            elif path == "/api/rubberband":
                self._json(self._rubberband(body))
            elif path == "/api/backend/start":
                self._json(self._backend_start())
            elif path == "/api/backend/stop":
                self._json(self._backend_stop())
            elif path == "/api/backend/restart":
                self._json(self._backend_restart())
            elif path == "/api/delete":
                qs = urllib.parse.parse_qs(self.path.split("?")[1] if "?" in self.path else "")
                self._json(self._delete(qs))
            else:
                self._error(404, "Not found")
        except Exception as e:
            self._error(500, str(e))

    # ── Helpers ──

    def _json(self, data):
        body = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _error(self, code, msg):
        body = json.dumps({"error": msg}).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


    def _health(self):
        """Check all system dependencies. Returns { component: {ok, code, detail} }."""
        import shutil as _shutil
        result = {}

        # ── Backend (onda container) ──
        rc, out, _ = self._run("docker", "ps", "-a", "--filter", "name=^onda$", "--format", "{{.Status}}")
        if rc != 0:
            result["backend"] = {"ok": False, "code": "E6", "detail": "Docker not accessible"}
        elif not out.strip():
            result["backend"] = {"ok": False, "code": "E1", "detail": "Onda container not found"}
        elif out.strip().startswith("Up"):
            result["backend"] = {"ok": True, "code": "", "detail": out.strip()}
        else:
            result["backend"] = {"ok": False, "code": "E2", "detail": out.strip()}

        # ── GPU ──
        rc, out, _ = self._run("docker", "run", "--rm", "--gpus", "all", "alpine", "echo", "GPU_OK")
        if rc == 0 and "GPU_OK" in out:
            result["gpu"] = {"ok": True, "code": "", "detail": "NVIDIA GPU available"}
        else:
            # Check if nvidia runtime exists at all
            rc2, info, _ = self._run("docker", "info", "--format", "{{json .Runtimes}}")
            if rc2 == 0 and "nvidia" in info:
                result["gpu"] = {"ok": False, "code": "E3", "detail": "NVIDIA runtime present but GPU check failed"}
            else:
                result["gpu"] = {"ok": False, "code": "E3", "detail": "NVIDIA runtime not available"}

        # ── Disk ──
        try:
            usage = _shutil.disk_usage("/output")
            free_mb = usage.free // (1024 * 1024)
            if free_mb < 500:
                result["disk"] = {"ok": False, "code": "E5", "detail": f"Only {free_mb} MB free on /output"}
            else:
                result["disk"] = {"ok": True, "code": "", "detail": f"{free_mb} MB free"}
        except Exception:
            result["disk"] = {"ok": True, "code": "", "detail": "Disk check unavailable"}

        # ── Docker socket ──
        rc, _, _ = self._run("docker", "ps")
        result["docker"] = {"ok": rc == 0, "code": "" if rc == 0 else "E6", "detail": "OK" if rc == 0 else "Docker socket not accessible"}

        return result

    def _backend_start(self):
        """Start the onda processing container."""
        rc, out, err = self._run("docker", "start", "onda")
        if rc == 0:
            return {"success": True, "detail": "Backend started"}
        return {"success": False, "detail": err.strip() or out.strip() or "Failed to start backend"}

    def _backend_stop(self):
        """Stop the onda processing container."""
        rc, out, err = self._run("docker", "stop", "onda")
        if rc == 0:
            return {"success": True, "detail": "Backend stopped"}
        return {"success": False, "detail": err.strip() or out.strip() or "Failed to stop backend"}

    def _backend_restart(self):
        """Restart the onda processing container."""
        rc, out, err = self._run("docker", "restart", "onda")
        if rc == 0:
            return {"success": True, "detail": "Backend restarted"}
        return {"success": False, "detail": err.strip() or out.strip() or "Failed to restart backend"}

    def _run(self, *args):
        """Run a command on the host (not inside onda container). Returns (rc, stdout, stderr)."""
        import subprocess as _sp
        r = _sp.run(list(args), capture_output=True, text=True, timeout=30)
        return r.returncode, r.stdout, r.stderr

    def _docker_exec(self, *args):
        """Run a command inside the onda container. Returns (returncode, stdout, stderr)."""
        cmd = ["docker", "exec", "onda"] + list(args)
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        return result.returncode, result.stdout, result.stderr

    # ── API Methods ──

    def _status(self):
        """Read pipeline status from onda container."""
        try:
            ret, stdout, _ = self._docker_exec("cat", "/tmp/onda_pipeline_status.json")
            if ret == 0 and stdout.strip():
                return json.loads(stdout.strip())
        except (subprocess.TimeoutExpired, json.JSONDecodeError, Exception):
            pass
        return {"status": "idle", "progress": 0, "step": "", "song": "", "elapsed": 0, "eta": 0}

    def _models(self, qs):
        model_type = qs.get("type", ["vocal"])[0]
        models = []
        for base_dir in MODEL_DIRS.get(model_type, []):
            if not base_dir.exists():
                continue
            for item in sorted(base_dir.iterdir()):
                if item.is_dir():
                    ckpts = (list(item.glob("*.ckpt")) + list(item.glob("*.pth")) +
                             list(item.glob("*.th")) + list(item.glob("*.onnx")))
                    if ckpts:
                        models.append({
                            "name": item.name,
                            "path": str(item),
                            "ckpts": [c.name for c in ckpts],
                        })
                elif item.is_file() and item.suffix.lower() in {".onnx", ".ckpt", ".pth", ".th"}:
                    models.append({
                        "name": item.stem,
                        "path": str(item),
                        "ckpts": [item.name],
                    })
        return {"models": models}

    def _input_files(self):
        files = []
        if INPUT_DIR.exists():
            for f in sorted(INPUT_DIR.iterdir()):
                if f.is_file() and f.suffix.lower() in ALLOWED_EXTENSIONS:
                    files.append({
                        "name": f.name,
                        "path": str(f),
                        "size": f.stat().st_size,
                    })
        return {"files": files}

    def _output_files(self, qs):
        song = qs.get("song", [None])[0]
        files = []
        if not OUTPUT_DIR.exists():
            return {"files": files}

        # Collect dirs first, then files. Capped at 100 total.
        dirs = sorted(
            [d for d in OUTPUT_DIR.iterdir() if d.is_dir() and not d.name.startswith("_")],
            key=lambda d: d.stat().st_mtime, reverse=True
        )
        count = 0
        for d in dirs:
            if song and d.name != song:
                continue
            for f in sorted(d.iterdir()):
                if f.is_file() and f.suffix.lower() in AUDIO_EXTENSIONS:
                    rel = str(f.relative_to(OUTPUT_DIR))
                    files.append({
                        "name": f.name,
                        "size": f.stat().st_size,
                        "url": f"/output/{rel}",
                    })
                    count += 1
                    if count >= 100:
                        return {"files": files}
        return {"files": files}

    def _separate(self, body):
        data = urllib.parse.parse_qs(body.decode())
        input_file = data.get("input_file", [""])[0]
        viperx = data.get("viperx", ["false"])[0] in ("true", "on")
        demucs = data.get("demucs", ["false"])[0] in ("true", "on")
        viperx_keep = data.get("viperx_keep", ["both"])[0]
        demucs_keep = data.get("demucs_keep", ["all"])[0]

        if not input_file:
            return {"success": False, "error": "Missing input_file"}

        if not input_file.startswith("/"):
            input_path = f"/input/{input_file}"
        else:
            input_path = input_file

        args = []
        if viperx:
            args += ["--viperx"]
            if viperx_keep:
                args += ["--viperx-keep", viperx_keep]
        if demucs:
            args += ["--demucs"]
            if demucs_keep:
                args += ["--demucs-keep", demucs_keep]
        args.append(input_path)

        cmd = ["docker", "exec", "onda", "/app/pipeline.sh"] + args

        try:
            subprocess.Popen(
                cmd,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                start_new_session=True,  # survive API server restart
            )
            return {"success": True, "message": "Pipeline started"}
        except FileNotFoundError:
            return {"success": False, "error": "docker not found"}
        except Exception as e:
            return {"success": False, "error": str(e)}

    def _rubberband(self, body):
        """Pitch-shift selected stems via rubberband. Returns new file URLs."""
        try:
            req = json.loads(body.decode())
        except (json.JSONDecodeError, UnicodeDecodeError):
            return {"success": False, "error": "Invalid JSON"}

        stems = req.get("stems", [])
        pitch = int(req.get("pitch", 0))
        if not stems:
            return {"success": False, "error": "No stems provided"}

        suffix = f" (+{pitch})" if pitch >= 0 else f" ({pitch})"
        new_files = []

        for item in stems:
            url = item.get("url", "")
            do_pitch = item.get("pitch", False)

            # Resolve path: /output/Song/stem.wav -> /output/Song/stem (+N).wav
            if url.startswith("/output/"):
                rel = url[len("/output/"):]
            elif url.startswith("output/"):
                rel = url[len("output/"):]
            else:
                rel = url

            rel_path = Path(rel)
            song_dir = rel_path.parent
            stem_name = rel_path.stem
            ext = rel_path.suffix
            new_name = f"{stem_name}{suffix}{ext}"
            new_rel = str(song_dir / new_name) if str(song_dir) != "." else new_name

            src_path = f"/output/{rel}"
            dst_path = f"/output/{new_rel}"

            try:
                if do_pitch:
                    ret, stdout, stderr = self._docker_exec(
                        "rubberband", "--pitch", str(pitch), "--quiet",
                        src_path, dst_path
                    )
                    if ret != 0:
                        return {"success": False, "error": f"Rubberband failed on {rel}: {stderr.strip()}"}
                else:
                    ret, stdout, stderr = self._docker_exec("cp", src_path, dst_path)
                    if ret != 0:
                        return {"success": False, "error": f"Copy failed on {rel}: {stderr.strip()}"}

                new_files.append({
                    "name": new_name,
                    "url": f"/output/{new_rel}",
                    "pitch": pitch if do_pitch else 0,
                })
            except Exception as e:
                return {"success": False, "error": f"Failed on {rel}: {str(e)}"}

        return {"success": True, "files": new_files}

    def _upload(self, body):
        filename = self.headers.get("X-Filename", "").strip()
        if not filename:
            return {"success": False, "error": "Missing X-Filename header"}

        # Sanitize: keep only safe chars
        import re
        safe = re.sub(r"[^\w\s.\-()\[\]]", "_", filename)
        ext = Path(safe).suffix.lower()
        if ext not in ALLOWED_EXTENSIONS:
            return {"success": False, "error": f"Unsupported format: {ext}"}

        dest = UPLOAD_DIR / safe
        dest.write_bytes(body)
        return {"success": True, "files": [{"name": safe}]}

    def _clear(self):
        if INPUT_DIR.exists():
            for f in INPUT_DIR.iterdir():
                if f.is_file():
                    f.unlink()
        return {"success": True}

    def _peaks(self, qs):
        """Generate waveform peak data for a WAV file. Returns JSON array of ~400 floats.
        Caches results as .peaks.json alongside the WAV for instant subsequent loads."""
        file_param = qs.get("file", [""])[0]
        if not file_param:
            return {"peaks": [], "error": "Missing file param"}

        # Resolve path — handle relative, absolute, and URLs with query params
        from urllib.parse import urlparse, unquote
        if "://" in file_param:
            # Full URL: http://host:port/output/Song/stem.wav?cb=...
            file_param = unquote(urlparse(file_param).path)
        elif file_param.startswith("/"):
            # Relative URL: /output/Song/stem.wav?cb=...
            file_param = unquote(urlparse("http://x" + file_param).path)
        else:
            file_param = unquote(file_param.split("?")[0])
        if "/output/" in file_param:
            rel = file_param[file_param.index("/output/") + len("/output/"):]
        elif file_param.startswith("output/"):
            rel = file_param[len("output/"):]
        else:
            rel = file_param
        target = OUTPUT_DIR / rel

        if not target.exists() or not target.is_file():
            return {"peaks": [], "error": "File not found"}

        num_peaks = int(qs.get("n", ["400"])[0])

        # Check cache
        cache_path = target.with_suffix(target.suffix + ".peaks.json")
        try:
            if cache_path.exists():
                with open(cache_path) as f:
                    cached = json.load(f)
                if cached.get("n") == num_peaks:
                    return {"peaks": cached["peaks"]}
        except Exception:
            pass  # Cache miss, regenerate

        try:
            with wave.open(str(target), 'rb') as wf:
                nframes = wf.getnframes()
                nchannels = wf.getnchannels()
                sampwidth = wf.getsampwidth()
                frames = wf.readframes(nframes)

            # Decode based on sample width
            if sampwidth == 2:
                fmt = f'{len(frames)//2}h'
                samples = struct.unpack(fmt, frames)
            elif sampwidth == 4:
                fmt = f'{len(frames)//4}i'
                samples = struct.unpack(fmt, frames)
            else:
                return {"peaks": [], "error": f"Unsupported sample width: {sampwidth}"}

            # If stereo, take left channel (every other sample)
            if nchannels == 2:
                samples = samples[::2]

            # Compute peaks
            step = max(1, len(samples) // num_peaks)
            peaks = []
            abs_max = 32768.0 if sampwidth == 2 else 2147483648.0
            for i in range(0, len(samples), step):
                chunk = samples[i:i+step]
                peak = max(abs(s) for s in chunk) / abs_max
                peaks.append(round(peak, 4))

            result = peaks[:num_peaks]

            # Save cache
            try:
                with open(cache_path, 'w') as f:
                    json.dump({"n": num_peaks, "peaks": result}, f)
            except Exception:
                pass  # Cache write failure is non-fatal

            return {"peaks": result}
        except Exception as e:
            return {"peaks": [], "error": str(e)}

    def _delete(self, qs):
        """Delete stem file or song directory via docker exec (permissions)."""
        file_param = qs.get("file", [None])[0]
        if not file_param:
            return {"success": False, "error": "Missing file param"}

        # Resolve: accept /output/... or output/... or just stem name
        if file_param.startswith("/output/"):
            rel = file_param[len("/output/"):]
        elif file_param.startswith("output/"):
            rel = file_param[len("output/"):]
        else:
            rel = file_param

        target = f"/output/{rel}"

        try:
            ret, stdout, stderr = self._docker_exec("rm", "-rf", target)
            if ret == 0:
                return {"success": True}
            else:
                return {"success": False, "error": stderr.strip() or "Delete failed"}
        except Exception as e:
            return {"success": False, "error": str(e)}

    def log_message(self, format, *args):
        pass  # Silent access log


if __name__ == "__main__":
    port = int(os.environ.get("API_PORT", 3001))
    server = HTTPServer(("127.0.0.1", port), OndaAPI)
    print(f"Onda API listening on 127.0.0.1:{port}")
    server.serve_forever()