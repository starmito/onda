"""Tests for onda.demucs without invoking the demucs CLI."""

import sys
from types import SimpleNamespace
from unittest import mock

import pytest

from onda.demucs import run_demucs


@pytest.fixture
def make_args(tmp_path):
    """Build a SimpleNamespace for run_demucs."""

    def _make(input_file=None, output="demucs_out", model="htdemucs", device="cpu"):
        return SimpleNamespace(
            input=str(input_file) if input_file else str(tmp_path / "input.wav"),
            output=str(tmp_path / output),
            model=model,
            device=device,
        )

    return _make


def test_run_demucs_exits_when_input_missing(make_args, capsys):
    """run_demucs must exit 1 when the input file does not exist."""
    args = make_args(input_file="/nonexistent/song.wav")
    with pytest.raises(SystemExit) as exc:
        run_demucs(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "not found" in captured.out.lower()


def test_run_demucs_invokes_demucs_module(make_args, tmp_path):
    """run_demucs must call 'python -m demucs' with the expected arguments."""
    input_file = tmp_path / "song.wav"
    input_file.write_bytes(b"audio")
    args = make_args(input_file=input_file, model="htdemucs_ft", device="cuda")

    fake_result = mock.Mock(returncode=0)
    fake_result.stderr = ""
    with mock.patch("onda.demucs.subprocess.run", return_value=fake_result) as mock_run:
        run_demucs(args)

        mock_run.assert_called_once()
        call_args = mock_run.call_args
        cmd = call_args.args[0]
        assert cmd[0] == sys.executable
        assert cmd[1] == "-m"
        assert cmd[2] == "demucs"
        assert "-o" in cmd
        assert args.output in cmd
        assert "-d" in cmd
        assert args.device in cmd
        assert args.model in cmd
        assert args.input in cmd


def test_run_demucs_exits_on_failure(make_args, tmp_path, capsys):
    """A failing demucs subprocess must exit 1 and print stderr."""
    input_file = tmp_path / "song.wav"
    input_file.write_bytes(b"audio")
    args = make_args(input_file=input_file)

    fake_result = mock.Mock(returncode=1)
    fake_result.stderr = "demucs crashed"
    with mock.patch("onda.demucs.subprocess.run", return_value=fake_result):
        with pytest.raises(SystemExit) as exc:
            run_demucs(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "demucs failed" in captured.out.lower()


def test_run_demucs_creates_output_dir(make_args, tmp_path):
    """run_demucs must create the output directory."""
    input_file = tmp_path / "song.wav"
    input_file.write_bytes(b"audio")
    output_dir = tmp_path / "fresh_output"
    args = make_args(input_file=input_file, output=str(output_dir))

    fake_result = mock.Mock(returncode=0)
    fake_result.stderr = ""
    with mock.patch("onda.demucs.subprocess.run", return_value=fake_result):
        run_demucs(args)

    assert output_dir.is_dir()
