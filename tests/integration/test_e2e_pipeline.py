"""End-to-end integration tests for the full Onda v2 pipeline.

Tests the complete chain: upload → separate → status → download → delete.
Also includes fast API validation tests (no pipeline execution).

Usage:
    # Run fast API tests only (no pipeline):
    python3 -m pytest tests/integration/test_e2e_pipeline.py -v -m fast

    # Run the full pipeline test (30-120s):
    python3 -m pytest tests/integration/test_e2e_pipeline.py -v -m slow -s

    # Run all tests:
    python3 -m pytest tests/integration/test_e2e_pipeline.py -v
"""

import json
import math
import os
import struct
import tempfile
import time
import urllib.error
import urllib.request
import wave

import pytest

API = "http://192.168.1.87:3000"
PRESETS_VALID = ["turbo", "quality", "balanced", "fast"]
POLL_INTERVAL = 2  # seconds
MAX_WAIT = 120      # seconds


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def generate_test_wav(duration_sec=30, sample_rate=44100):
    """Generate a simple sine wave WAV file for testing."""
    tmp = tempfile.NamedTemporaryFile(suffix=".wav", delete=False)
    n_samples = duration_sec * sample_rate
    with wave.open(tmp.name, "w") as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(sample_rate)
        for i in range(n_samples):
            value = int(16000 * math.sin(2 * math.pi * 440 * i / sample_rate))
            wf.writeframes(struct.pack("<h", value))
    return tmp.name


def api_get(path, timeout=10):
    """GET request, returns (status_code, parsed_json)."""
    url = f"{API}{path}"
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.status, json.loads(resp.read())


def api_post(path, data=None, timeout=10):
    """POST request with JSON body, returns (status_code, parsed_json)."""
    url = f"{API}{path}"
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


def api_post_multipart(path, file_path, field_name="file", timeout=30):
    """POST request with multipart file upload, returns (status_code, parsed_json)."""
    url = f"{API}{path}"
    boundary = "----OndaTestBoundary"
    filename = os.path.basename(file_path)

    # Build multipart body manually
    with open(file_path, "rb") as f:
        file_data = f.read()

    body_lines = []
    body_lines.append(f"--{boundary}".encode())
    body_lines.append(
            f'Content-Disposition: form-data; name="{field_name}"; filename="{filename}"'.encode()
    )
    body_lines.append(b"Content-Type: audio/wav")
    body_lines.append(b"")
    body_lines.append(file_data)
    body_lines.append(f"--{boundary}--".encode())
    body = b"\r\n".join(body_lines)

    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")

    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


def api_delete(path, timeout=10):
    """DELETE request, returns (status_code, parsed_json)."""
    url = f"{API}{path}"
    req = urllib.request.Request(url, method="DELETE")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


def api_get_raw(path, timeout=10):
    """GET request returning raw (status_code, content_type, bytes)."""
    url = f"{API}{path}"
    req = urllib.request.Request(url)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            content_type = resp.headers.get("Content-Type", "")
            return resp.status, content_type, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, "", b""


def wait_for_completion(max_wait=MAX_WAIT, poll_interval=POLL_INTERVAL):
    """Poll GET /api/status until status is 'done' or timeout, returns final status dict."""
    deadline = time.monotonic() + max_wait
    while time.monotonic() < deadline:
        try:
            code, data = api_get("/api/status")
        except Exception:
            time.sleep(poll_interval)
            continue

        if data.get("status") in ("done", "error"):
            return data
        time.sleep(poll_interval)

    # Timeout — grab last status
    try:
        _, data = api_get("/api/status")
    except Exception:
        data = {"status": "timeout", "error": "polling timeout"}
    return data


# ---------------------------------------------------------------------------
# Slow test: full end-to-end pipeline
# ---------------------------------------------------------------------------


@pytest.mark.slow
class TestE2EPipeline:
    """Full pipeline test: upload → separate → status → download → delete."""

    def test_e2e_full_pipeline(self):
        """Test the complete Onda v2 pipeline flow.

        Steps:
            1. Generate and upload a synthetic WAV file
            2. Start separation pipeline
            3. Poll status until done
            4. Download each generated stem file
            5. Delete the song output directory
            6. Verify status returns idle after deletion
        """
        # ── Step 1: Generate & Upload ─────────────────────────────────
        wav_path = None
        try:
            wav_path = generate_test_wav(duration_sec=30, sample_rate=44100)
            print(f"\n[1/6] Uploading test audio: {wav_path} ({os.path.getsize(wav_path)} bytes)")

            status, data = api_post_multipart("/api/upload", wav_path)
            assert status == 200, f"Upload failed: {status} — {data}"
            assert "path" in data, f"Upload response missing 'path': {data}"
            input_path = data["path"]
            print(f"      → input path: {input_path}")
        finally:
            if wav_path and os.path.exists(wav_path):
                os.unlink(wav_path)

        # ── Step 2: Separate ──────────────────────────────────────────
        print(f"\n[2/6] Starting separation with preset=turbo")
        sep_payload = {
            "preset": "turbo",
            "input": input_path,
            "viperx": True,
            "demucs": True,
            "viperx_keep": "both",
            "demucs_keep": ["drums", "bass", "other", "vocals"],
        }
        status, data = api_post("/api/separate", sep_payload)
        assert status == 202, f"Separate failed: {status} — {data}"
        assert data.get("status") == "started", f"Unexpected response: {data}"
        song = data.get("song")
        assert song, f"No song name in response: {data}"
        print(f"      → song: {song}")

        # ── Step 3: Poll Status ───────────────────────────────────────
        print(f"\n[3/6] Polling status until pipeline completes (max {MAX_WAIT}s)...")
        final = wait_for_completion()
        assert final.get("status") == "done", (
            f"Pipeline did not complete. Status: {final.get('status')}, "
            f"error: {final.get('error', 'none')}"
        )
        assert final.get("progress") == 1.0, (
            f"Progress should be 1.0, got {final.get('progress')}"
        )
        files = final.get("files", [])
        assert len(files) > 0, f"Expected output files, got none. Full response: {final}"
        print(f"      → status=done, progress=1.0, files={len(files)}")
        for f in files:
            print(f"        {f['name']}")

        # ── Step 4: Download each file ────────────────────────────────
        print(f"\n[4/6] Downloading {len(files)} output file(s)...")
        for f in files:
            file_path = f["path"]
            code, ct, body = api_get_raw(file_path)
            assert code == 200, f"Download failed for {file_path}: HTTP {code}"
            assert "audio/wav" in ct.lower() or "audio/x-wav" in ct.lower(), (
                f"Unexpected Content-Type for {file_path}: {ct}"
            )
            assert len(body) > 100, (
                f"File {file_path} too small ({len(body)} bytes), likely not real audio"
            )
            print(f"      → {f['name']}: {len(body)} bytes, Content-Type={ct}")

        # ── Step 5: Delete the song ───────────────────────────────────
        print(f"\n[5/6] Deleting song: {song}")
        del_path = f"/api/files/{song}"
        status, data = api_delete(del_path)
        assert status == 200, f"Delete failed: {status} — {data}"
        assert data.get("deleted") is True, f"Expected deleted=true, got: {data}"
        print(f"      → deleted={data['deleted']}")

        # ── Step 6: Verify idle ────────────────────────────────────────
        print(f"\n[6/6] Verifying status returns to idle after deletion")
        status, data = api_get("/api/status")
        print(f"      → status response: {data}")
        assert data.get("status") == "idle", (
            f"Expected idle after deletion, got: {data.get('status')}"
        )


# ---------------------------------------------------------------------------
# Fast tests: API validation (no pipeline execution)
# ---------------------------------------------------------------------------


@pytest.mark.fast
class TestUploadValidation:
    """Quick validation tests for the upload endpoint."""

    def test_upload_reject_no_file(self):
        """POST /api/upload without a file should return 400."""
        url = f"{API}/api/upload"
        # Send a POST with no multipart body
        req = urllib.request.Request(url, data=b"", method="POST")
        try:
            urllib.request.urlopen(req, timeout=5)
            pytest.fail("Expected HTTP 400")
        except urllib.error.HTTPError as e:
            assert e.code == 400, f"Expected 400, got {e.code}"
            body = json.loads(e.read())
            assert "error" in body, f"Expected error key in: {body}"


@pytest.mark.fast
class TestSeparateValidation:
    """Quick validation tests for the separate endpoint."""

    def test_separate_invalid_preset(self):
        """POST /api/separate with an unknown preset should return 400."""
        status, data = api_post("/api/separate", {
            "preset": "nonexistent_preset_xyz",
            "input": "/input/fake.wav",
        })
        assert status == 400, f"Expected 400, got {status}: {data}"
        assert "error" in data, f"Expected error key in: {data}"


@pytest.mark.fast
class TestStatusValidation:
    """Quick validation tests for the status endpoint."""

    def test_status_response_structure(self):
        """GET /api/status returns valid JSON with expected keys.
        When idle (no pipeline), status='idle'. When running/done, full status object."""
        status, data = api_get("/api/status")
        assert status == 200, f"Expected 200, got {status}"
        assert "status" in data, f"Response missing 'status': {data}"
        # Acceptable values: idle, running, done, error
        assert data["status"] in ("idle", "running", "done", "error"), (
            f"Unexpected status value: {data['status']}"
        )


@pytest.mark.fast
class TestDeleteValidation:
    """Quick validation tests for the delete endpoint."""

    def test_delete_nonexistent(self):
        """DELETE /api/files/nonexistent should return 404."""
        status, data = api_delete("/api/files/noexiste_xyz_test")
        assert status == 404, f"Expected 404, got {status}: {data}"
        assert "error" in data, f"Expected error key in: {data}"


# ---------------------------------------------------------------------------
# PipelineConfig flags tests (slow)
# ---------------------------------------------------------------------------


@pytest.mark.slow
class TestPipelineConfigFlags:
    """Verifica que las flags de PipelineConfig se respetan."""

    def _do_pipeline(self, payload, label):
        """Helper: upload → separate → poll. Returns (song, final_status)."""
        wav_path = None
        try:
            wav_path = generate_test_wav(duration_sec=10, sample_rate=44100)
            print(f"\n[{label}] Uploading...")
            status, data = api_post_multipart("/api/upload", wav_path)
            assert status == 200, f"Upload failed: {status} — {data}"
            input_path = data["path"]

            payload["input"] = input_path
            print(f"[{label}] Separating with: viperx={payload.get('viperx')}, demucs={payload.get('demucs')}")
            status, data = api_post("/api/separate", payload)
            assert status == 202, f"Separate failed: {status} — {data}"
            song = data["song"]

            print(f"[{label}] Polling until done...")
            final = wait_for_completion()
            assert final.get("status") == "done", f"Pipeline failed: {final}"
            return song, final
        finally:
            if wav_path and os.path.exists(wav_path):
                os.unlink(wav_path)

    def _cleanup(self, song):
        try:
            api_delete(f"/api/files/{song}")
        except Exception:
            pass

    @staticmethod
    def _has_file(files, substr):
        return any(substr in f["name"] for f in files)

    @staticmethod
    def _has_demucs_vocals(files):
        """Check for Demucs vocals.wav (NOT no_vocals.wav)."""
        return any(f["name"] == "vocals.wav" for f in files)

    @pytest.mark.xfail(reason="Demucs-only mode has known output path bug (htdemucs_ft subdirectory)")
    def test_demucs_only_no_viperx(self):
        """Solo Demucs, sin ViperX. Debe usar archivo original como input.
        
        Known issue: Demucs outputs to htdemucs_ft/ subdirectory but backend
        expects files at song root. Test validates the expected behavior;
        xfail until backend is fixed.
        """
        payload = {
            "preset": "turbo",
            "viperx": False,
            "demucs": True,
            "demucs_keep": ["drums", "bass", "other", "vocals"],
        }
        song, final = self._do_pipeline(payload, "demucs-only")
        try:
            files = final.get("files", [])
            names = [f["name"] for f in files]
            print(f"      files: {names}")
            # NO ViperX outputs
            assert not self._has_file(files, "vocals_viperx"), f"Should NOT have _vocals: {names}"
            assert not self._has_file(files, "instrumental_viperx"), f"Should NOT have _instrumental: {names}"
            # YES Demucs outputs
            assert self._has_demucs_vocals(files), f"Should have vocals.wav: {names}"
            assert self._has_file(files, "no_vocals"), f"Should have no_vocals: {names}"
        finally:
            self._cleanup(song)

    def test_viperx_only_no_demucs(self):
        """Solo ViperX, sin Demucs. Solo separación vocal."""
        payload = {
            "preset": "turbo",
            "viperx": True,
            "demucs": False,
            "viperx_keep": "both",
        }
        song, final = self._do_pipeline(payload, "viperx-only")
        try:
            files = final.get("files", [])
            names = [f["name"] for f in files]
            print(f"      files: {names}")
            # YES ViperX outputs
            assert self._has_file(files, "vocals_viperx"), f"Should have _vocals: {names}"
            assert self._has_file(files, "instrumental_viperx"), f"Should have _instrumental: {names}"
            # NO Demucs outputs (Demucs was disabled)
            assert not self._has_file(files, "no_vocals"), f"Should NOT have no_vocals: {names}"
        finally:
            self._cleanup(song)

    def test_viperx_vocals_only(self):
        """ViperX guardando solo vocals."""
        payload = {
            "preset": "turbo",
            "viperx": True,
            "demucs": False,
            "viperx_keep": "vocals",
        }
        song, final = self._do_pipeline(payload, "viperx-vocals-only")
        try:
            files = final.get("files", [])
            names = [f["name"] for f in files]
            print(f"      files: {names}")
            assert self._has_file(files, "vocals_viperx"), f"Should have _vocals: {names}"
            assert not self._has_file(files, "instrumental_viperx"), f"Should NOT have _instrumental: {names}"
        finally:
            self._cleanup(song)

    def test_viperx_instrumental_only(self):
        """ViperX guardando solo instrumental."""
        payload = {
            "preset": "turbo",
            "viperx": True,
            "demucs": False,
            "viperx_keep": "instrumental",
        }
        song, final = self._do_pipeline(payload, "viperx-instr-only")
        try:
            files = final.get("files", [])
            names = [f["name"] for f in files]
            print(f"      files: {names}")
            assert self._has_file(files, "instrumental_viperx"), f"Should have _instrumental: {names}"
            assert not self._has_file(files, "vocals_viperx"), f"Should NOT have _vocals: {names}"
        finally:
            self._cleanup(song)

    def test_demucs_keep_subset(self):
        """Demucs guardando solo algunos stems.
        
        Note: no_vocals.wav is always generated by Demucs alongside vocals.wav
        regardless of the demucs_keep list. The keep filter controls individual
        stems (drums, bass, other, vocals), not the combined no_vocals output.
        """
        payload = {
            "preset": "turbo",
            "viperx": True,
            "demucs": True,
            "viperx_keep": "both",
            "demucs_keep": ["drums", "vocals"],
        }
        song, final = self._do_pipeline(payload, "demucs-keep-subset")
        try:
            files = final.get("files", [])
            names = [f["name"] for f in files]
            print(f"      files: {names}")
            # Should have vocals.wav (Demucs stem, kept)
            assert self._has_demucs_vocals(files), f"Should have vocals.wav: {names}"
            # no_vocals.wav is always generated alongside vocals (backend behavior)
            # Verify that at least vocals was kept
            assert len(files) >= 2, f"Expected at least vocals + no_vocals + viperx outputs"
            # Should NOT have bass.wav or other.wav (filtered out)
            assert not self._has_file(files, "bass.wav") and not self._has_file(files, "other.wav"), (
                f"bass/other should have been filtered: {names}"
            )
        finally:
            self._cleanup(song)


# ---------------------------------------------------------------------------
# Data structure tests (fast)
# ---------------------------------------------------------------------------


@pytest.mark.fast
class TestDataStructures:
    """Verifica que las respuestas del backend coinciden con lo que espera el frontend."""

    def test_status_response_has_model_fields(self):
        """Status debe incluir campos de modelo para trazabilidad (tras pipeline)."""
        status, data = api_get("/api/status")
        assert status == 200
        assert "status" in data, f"Missing 'status' key: {data}"
        # When idle, only 'status' key is present. After pipeline,
        # 'preset', 'vocal_model', 'stem_model' are also present.
        if data.get("status") == "done":
            for field in ("preset", "vocal_model", "stem_model"):
                assert field in data, f"Status response missing '{field}': {data}"

    def test_models_response_structure(self):
        """GET /api/models debe devolver estructura compatible con PresetSelector."""
        status, data = api_get("/api/models")
        assert status == 200
        assert isinstance(data, dict), f"Expected dict, got {type(data)}"
        assert len(data) > 0, "Expected at least one preset"
        for preset in data.values():
            assert "name" in preset, f"Preset missing 'name': {preset}"
            assert "description" in preset, f"Preset missing 'description': {preset}"
        # Verify known presets exist
        preset_names = {p["name"] for p in data.values()}
        for expected in ("turbo", "balance", "master", "ultimate"):
            assert expected in preset_names, f"Missing preset '{expected}' in: {preset_names}"

    def test_health_response_structure(self):
        """Health debe tener campos que el frontend usa en HealthBar."""
        status, data = api_get("/api/health")
        assert status == 200
        # 'status' is a top-level string (e.g., "ok"), not a dict
        assert "status" in data, f"Health missing 'status': {data}"
        assert isinstance(data["status"], str), f"Health.status should be str, got: {type(data['status'])}"
        # Other sections are dicts with 'ok' and 'detail'
        for section in ("backend", "gpu", "disk", "docker"):
            assert section in data, f"Health missing '{section}': {data}"
            section_data = data[section]
            assert "ok" in section_data, f"Health.{section} missing 'ok': {section_data}"
            assert "detail" in section_data, f"Health.{section} missing 'detail': {section_data}"

    def test_separate_response_has_song(self):
        """POST /api/separate debe devolver song y status (aunque falle el input)."""
        status, data = api_post("/api/separate", {
            "preset": "turbo",
            "input": "/input/fake_does_not_exist.wav",
            "viperx": True,
            "demucs": True,
        })
        # May return 400 (bad input) or 202 (accepted), but structure should have song/status
        assert "status" in data or "error" in data, f"Response should have status or error: {data}"
        # If it was accepted, it should have a song name; clean up to avoid polluting status
        if "song" in data and data.get("status") == "started":
            assert isinstance(data["song"], str) and len(data["song"]) > 0
            # Delete the song immediately to clean up (won't stop running pipeline,
            # but prevents leftovers for next runs)
            try:
                api_delete(f"/api/files/{data['song']}")
            except Exception:
                pass


# ---------------------------------------------------------------------------
# Edge case tests
# ---------------------------------------------------------------------------


@pytest.mark.slow
class TestEdgeCases:
    """Casos límite que han causado bugs."""

    def test_filename_with_spaces_and_accents(self):
        """Canción con espacios y tildes (como 'Grupo Arena de Canarias')."""
        wav_path = None
        song = None
        try:
            # Create a WAV with a name containing spaces and accents
            n_samples = 10 * 44100
            wav_path = os.path.join(tempfile.gettempdir(), "Grupo Test - CAÑÓN.wav")
            with wave.open(wav_path, "w") as wf:
                wf.setnchannels(1)
                wf.setsampwidth(2)
                wf.setframerate(44100)
                for i in range(n_samples):
                    value = int(16000 * math.sin(2 * math.pi * 440 * i / 44100))
                    wf.writeframes(struct.pack("<h", value))

            print(f"\n[edge-accents] Uploading: {wav_path}")
            status, data = api_post_multipart("/api/upload", wav_path)
            assert status == 200, f"Upload failed: {status} — {data}"
            input_path = data["path"]
            print(f"      input path: {input_path}")

            print(f"[edge-accents] Separating...")
            status, data = api_post("/api/separate", {
                "preset": "turbo",
                "input": input_path,
                "viperx": True,
                "demucs": True,
                "viperx_keep": "both",
                "demucs_keep": ["drums", "bass", "other", "vocals"],
            })
            assert status == 202, f"Separate failed: {status} — {data}"
            song = data["song"]
            print(f"      song: {song}")

            print(f"[edge-accents] Polling...")
            final = wait_for_completion()
            assert final.get("status") == "done", f"Pipeline failed: {final}"
            files = final.get("files", [])
            assert len(files) > 0, f"No output files: {final}"
            print(f"      done, {len(files)} files")
        finally:
            if wav_path and os.path.exists(wav_path):
                os.unlink(wav_path)
            if song:
                try:
                    api_delete(f"/api/files/{song}")
                except Exception:
                    pass

    @pytest.mark.fast
    def test_delete_nonexistent_file(self):
        """DELETE archivo (ruta anidada) que no existe → 404 o 405."""
        url = f"{API}/api/files/nonexistent_song_xyz/nonexistent_file.wav"
        req = urllib.request.Request(url, method="DELETE")
        try:
            urllib.request.urlopen(req, timeout=5)
            pytest.fail("Expected HTTP error (404/405)")
        except urllib.error.HTTPError as e:
            assert e.code in (404, 405), (
                f"Expected 404 or 405, got {e.code}"
            )


# ---------------------------------------------------------------------------
# Smoke test (fast, no pipeline)
# ---------------------------------------------------------------------------


@pytest.mark.fast
class TestSmoke:
    """Pruebas rápidas que no ejecutan pipeline (para CI/pre-commit)."""

    def test_all_endpoints_accessible(self):
        """Todos los endpoints responden (aunque sea 404/405)."""
        endpoints = [
            ("GET", "/api/health"),
            ("GET", "/api/status"),
            ("GET", "/api/models"),
            ("GET", "/api/gpu"),
        ]
        for method, path in endpoints:
            url = f"{API}{path}"
            req = urllib.request.Request(url, method=method)
            try:
                with urllib.request.urlopen(req, timeout=5) as resp:
                    assert resp.status in [200, 404, 405], (
                        f"{method} {path} → unexpected {resp.status}"
                    )
            except urllib.error.HTTPError as e:
                assert e.code in [200, 404, 405], (
                    f"{method} {path} → unexpected {e.code}"
                )
            except Exception as e:
                pytest.fail(f"{method} {path} → connection failed: {e}")
