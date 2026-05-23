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
import subprocess
import urllib.parse
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
    "stems": [MODELS_DIR / "Demucs_Models"],
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
                self._json({"ok": True})
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

    def _docker_exec(self, *args):
        """Run a command inside the onda container. Returns (returncode, stdout, stderr)."""
        cmd = ["docker", "exec", "onda"] + list(args)
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        return result.returncode, result.stdout, result.stderr

    # ── API Methods ──

    def _status(self):
        """Read pipeline status from onda container."""
        try:
            ret, stdout, _ = self._docker_exec("cat", "/tmp/pipeline_status.json")
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
                    ckpts = list(item.glob("*.ckpt")) + list(item.glob("*.pth"))
                    if ckpts:
                        models.append({
                            "name": item.name,
                            "path": str(item),
                            "ckpts": [c.name for c in ckpts],
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
            [d for d in OUTPUT_DIR.iterdir() if d.is_dir() and not d.name.startswith("_")),
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
        rubberband = data.get("rubberband", ["false"])[0] in ("true", "on")
        pitch = data.get("pitch", ["0"])[0]
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
        if rubberband:
            args += ["--rubberband"]
            if pitch and pitch != "0":
                args += ["--pitch", pitch]
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
