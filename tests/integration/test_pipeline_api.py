"""End-to-end tests for the Go pipeline via HTTP API."""
import subprocess
import pytest
import urllib.request
import json

API = "http://192.168.1.87:3000"

def api_get(path):
    url = f"{API}{path}"
    with urllib.request.urlopen(url, timeout=10) as resp:
        return resp.status, json.loads(resp.read())

def test_api_health():
    status, data = api_get("/api/health")
    assert status == 200
    assert data.get("docker") == "running" or data.get("container") == "running"

def test_api_models():
    status, data = api_get("/api/models")
    assert status == 200
    # Response should contain presets
    assert len(str(data)) > 10, "Empty models response"

def test_api_gpu():
    status, data = api_get("/api/gpu")
    assert status == 200
    assert "cuda" in str(data).lower() or "nvidia" in str(data).lower()

def test_health_method_not_allowed():
    """POST to /api/health should return 405."""
    import urllib.request
    req = urllib.request.Request(f"{API}/api/health", data=b"{}", method="POST")
    try:
        urllib.request.urlopen(req, timeout=5)
        assert False, "Expected HTTP error"
    except urllib.error.HTTPError as e:
        assert e.code == 405, f"Expected 405, got {e.code}"
