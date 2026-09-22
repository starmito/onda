"""Tests for deploy.sh release-invariant logic.

These tests use temporary git repositories *inside* the project root so they
never touch the real repo or its tags.  deploy.sh is sourced to reuse its
functions without running the actual deploy.
"""

import os
import shutil
import subprocess
import tempfile

import pytest


@pytest.fixture
def repo_root():
    """Return the absolute project root."""
    return os.path.dirname(os.path.dirname(os.path.dirname(__file__)))


def _write_minimal_repo(path, version):
    """Create the minimal versioned files required by build.sh/deploy.sh."""
    os.makedirs(os.path.join(path, "onda"), exist_ok=True)
    os.makedirs(os.path.join(path, "frontend"), exist_ok=True)

    with open(os.path.join(path, "VERSION"), "w", encoding="utf-8") as f:
        f.write(version + "\n")

    with open(os.path.join(path, "onda", "_version.py"), "w", encoding="utf-8") as f:
        f.write(f'__version__ = "{version}"\n')

    with open(os.path.join(path, "frontend", "package.json"), "w", encoding="utf-8") as f:
        f.write(f'{{"version": "{version}"}}\n')

    py_version = version.lstrip("v")
    with open(os.path.join(path, "pyproject.toml"), "w", encoding="utf-8") as f:
        f.write(f"[project]\nversion = \"{py_version}\"\n")


def _git_config(repo):
    """Set a dummy identity so git commits work."""
    subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=repo, check=True)
    subprocess.run(["git", "config", "user.name", "Test User"], cwd=repo, check=True)


def _create_temp_repo(repo_root, version="v1.2.3"):
    """Create a temporary git repo with the given VERSION and return its path."""
    tmpdir = tempfile.mkdtemp(dir=repo_root, prefix="tmp-test-repo-")
    try:
        _write_minimal_repo(tmpdir, version)
        subprocess.run(["git", "init"], cwd=tmpdir, check=True, capture_output=True)
        _git_config(tmpdir)
        subprocess.run(["git", "add", "."], cwd=tmpdir, check=True, capture_output=True)
        subprocess.run(["git", "commit", "-m", "initial"], cwd=tmpdir, check=True, capture_output=True)
        subprocess.run(["git", "branch", "-M", "main"], cwd=tmpdir, check=True, capture_output=True)
        return tmpdir
    except Exception:
        shutil.rmtree(tmpdir, ignore_errors=True)
        raise


def _run_bash(repo_root, tmpdir, script, env=None):
    """Source build.sh and deploy.sh with ONDA_ROOT pointing at the temp repo."""
    env_vars = {"ONDA_ROOT": tmpdir}
    if env:
        env_vars.update(env)

    full_script = f"""
set -euo pipefail
# Source with --version so we only load functions; the empty case would
# otherwise trigger the native build, which needs the real repo structure.
source "{repo_root}/build.sh" --version
source "{repo_root}/deploy.sh"
{script}
"""
    result = subprocess.run(
        ["bash", "-c", full_script],
        cwd=repo_root,
        capture_output=True,
        text=True,
        env={**os.environ, **env_vars},
    )
    return result


@pytest.fixture
def temp_repo(repo_root):
    """Yield a temporary repo and clean it up afterwards."""
    path = _create_temp_repo(repo_root)
    try:
        yield path
    finally:
        shutil.rmtree(path, ignore_errors=True)


class TestVersionFromFile:
    """The canonical version must come from VERSION, never from git tags."""

    def test_version_ignores_newer_tag(self, repo_root, temp_repo):
        """Even if a newer onda-* tag exists, VERSION wins."""
        # Tag the initial commit with both the matching version and a newer one.
        subprocess.run(["git", "tag", "onda-v1.2.3"], cwd=temp_repo, check=True)
        subprocess.run(["git", "tag", "onda-v9.9.9"], cwd=temp_repo, check=True)

        result = _run_bash(repo_root, temp_repo, "read_version && echo ONDAP_VERSION=$ONDAP_VERSION")
        assert result.returncode == 0, result.stderr
        assert "ONDAP_VERSION=v1.2.3" in result.stdout


class TestReleaseTagChecks:
    """deploy.sh must refuse to deploy without a proper release tag."""

    def test_refuses_missing_tag(self, repo_root, temp_repo):
        """No onda-<VERSION> tag exists -> deploy is denied with a clear message."""
        result = _run_bash(repo_root, temp_repo, "check_release_invariants")
        assert result.returncode != 0
        assert "release tag 'onda-v1.2.3' does not exist" in result.stderr

    def test_refuses_tag_not_ancestor(self, repo_root, temp_repo):
        """A tag that is not reachable from HEAD must be rejected."""
        # Create tag on a side branch.
        subprocess.run(["git", "checkout", "-b", "side"], cwd=temp_repo, check=True, capture_output=True)
        with open(os.path.join(temp_repo, "side.txt"), "w", encoding="utf-8") as f:
            f.write("side\n")
        subprocess.run(["git", "add", "side.txt"], cwd=temp_repo, check=True)
        subprocess.run(["git", "commit", "-m", "side commit"], cwd=temp_repo, check=True, capture_output=True)
        subprocess.run(["git", "tag", "onda-v1.2.3"], cwd=temp_repo, check=True)

        # Back on main with an extra commit that does not contain the tag.
        subprocess.run(["git", "checkout", "main"], cwd=temp_repo, check=True, capture_output=True)
        with open(os.path.join(temp_repo, "main.txt"), "w", encoding="utf-8") as f:
            f.write("main\n")
        subprocess.run(["git", "add", "main.txt"], cwd=temp_repo, check=True)
        subprocess.run(["git", "commit", "-m", "main commit"], cwd=temp_repo, check=True, capture_output=True)

        result = _run_bash(repo_root, temp_repo, "check_release_invariants")
        assert result.returncode != 0
        assert "is not an ancestor of HEAD" in result.stderr

    def test_accepts_existing_ancestor_tag(self, repo_root, temp_repo):
        """A valid release tag that is ancestor of HEAD lets the deploy proceed."""
        subprocess.run(["git", "tag", "onda-v1.2.3"], cwd=temp_repo, check=True)
        # Add another commit on top; the tag is still an ancestor.
        with open(os.path.join(temp_repo, "extra.txt"), "w", encoding="utf-8") as f:
            f.write("extra\n")
        subprocess.run(["git", "add", "extra.txt"], cwd=temp_repo, check=True)
        subprocess.run(["git", "commit", "-m", "extra commit"], cwd=temp_repo, check=True, capture_output=True)

        result = _run_bash(repo_root, temp_repo, "check_release_invariants && echo IMAGE_TAG=$IMAGE_TAG")
        assert result.returncode == 0, result.stderr
        assert "IMAGE_TAG=v1.2.3" in result.stdout


class TestModifiedVersionFiles:
    """Deploy must never rewrite tracked version files."""

    def test_refuses_and_does_not_touch_modified_file(self, repo_root, temp_repo):
        """If a versioned file is out of sync, deploy fails and leaves it untouched."""
        subprocess.run(["git", "tag", "onda-v1.2.3"], cwd=temp_repo, check=True)

        package_json = os.path.join(temp_repo, "frontend", "package.json")
        with open(package_json, "w", encoding="utf-8") as f:
            f.write('{"version": "v1.2.4"}\n')

        result = _run_bash(repo_root, temp_repo, "check_release_invariants")
        assert result.returncode != 0
        assert "frontend/package.json version (v1.2.4) does not match VERSION (v1.2.3)" in result.stderr

        # File must remain exactly as the test left it.
        with open(package_json, "r", encoding="utf-8") as f:
            assert f.read() == '{"version": "v1.2.4"}\n'


class TestUntaggedEscape:
    """ONDA_ALLOW_UNTAGGED=1 is the explicit escape hatch for dev builds."""

    def test_allow_untagged_dev_tag(self, repo_root, temp_repo):
        """Without a tag but with the flag, deploy is allowed and image is -dev."""
        result = _run_bash(
            repo_root,
            temp_repo,
            "check_release_invariants && echo IMAGE_TAG=$IMAGE_TAG",
            env={"ONDA_ALLOW_UNTAGGED": "1"},
        )
        assert result.returncode == 0, result.stderr
        assert "IMAGE_TAG=v1.2.3-dev" in result.stdout
        assert "ONDA_ALLOW_UNTAGGED=1" in result.stderr
