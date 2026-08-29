"""Tests for onda.pitch without invoking the rubberband binary."""

import os
import sys
from types import SimpleNamespace
from unittest import mock

import pytest

from onda.pitch import SUPPORTED_STEMS, run_pitch


@pytest.fixture
def make_args(tmp_path):
    """Build a SimpleNamespace that points run_pitch at tmp_path."""

    def _make(input_dir=None, output="pitched", semitones=2.0, skip=("drums",)):
        return SimpleNamespace(
            input_dir=str(input_dir or tmp_path / "in"),
            output=str(tmp_path / output),
            semitones=semitones,
            skip=list(skip),
        )

    return _make


def test_run_pitch_exits_when_input_dir_missing(make_args, capsys):
    """run_pitch must exit 1 when the input directory does not exist."""
    args = make_args(input_dir="/nonexistent/stems")
    with pytest.raises(SystemExit) as exc:
        run_pitch(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "not found" in captured.out.lower()


def test_run_pitch_skips_missing_stems(make_args, tmp_path, capsys):
    """Missing stem files are skipped without error."""
    input_dir = tmp_path / "in"
    input_dir.mkdir()
    # Only create bass.wav
    (input_dir / "bass.wav").write_bytes(b"audio")

    args = make_args(input_dir=input_dir, skip=[])
    with mock.patch("onda.pitch.subprocess.run", return_value=mock.Mock(returncode=0)), mock.patch(
        "onda.pitch.os.path.getsize", return_value=1_000_000
    ):
        run_pitch(args)

    captured = capsys.readouterr()
    assert "skipping drums.wav" in captured.out.lower()
    assert "skipping other.wav" in captured.out.lower()
    assert "skipping vocals.wav" in captured.out.lower()


def test_run_pitch_calls_rubberband_for_non_skipped_stems(make_args, tmp_path):
    """Non-skipped stems trigger rubberband subprocess calls."""
    input_dir = tmp_path / "in"
    input_dir.mkdir()
    for stem in SUPPORTED_STEMS:
        if stem != "drums":
            (input_dir / f"{stem}.wav").write_bytes(b"audio")

    args = make_args(input_dir=input_dir, skip=("drums",))
    with mock.patch("onda.pitch.subprocess.run", return_value=mock.Mock(returncode=0)) as mock_run, mock.patch(
        "onda.pitch.os.path.getsize", return_value=1_000_000
    ):
        run_pitch(args)

        called_stems = set()
        for call in mock_run.call_args_list:
            cmd = call.args[0]
            assert cmd[0] == "rubberband"
            assert cmd[1] == "-p"
            assert float(cmd[2]) == pytest.approx(args.semitones)
            called_stems.add(os.path.basename(cmd[3]))

        assert called_stems == {"bass.wav", "other.wav", "vocals.wav"}


def test_run_pitch_copies_skipped_stems(make_args, tmp_path):
    """Skipped stems are copied as-is."""
    input_dir = tmp_path / "in"
    input_dir.mkdir()
    drums = input_dir / "drums.wav"
    drums.write_bytes(b"drums-data")

    args = make_args(input_dir=input_dir, skip=("drums",))
    with mock.patch("onda.pitch.subprocess.run", return_value=mock.Mock(returncode=0)):
        run_pitch(args)

    output_file = tmp_path / "pitched" / "drums.wav"
    assert output_file.exists()
    assert output_file.read_bytes() == b"drums-data"


def test_run_pitch_exits_on_rubberband_failure(make_args, tmp_path, capsys):
    """A failing rubberband call must exit 1 and print stderr."""
    input_dir = tmp_path / "in"
    input_dir.mkdir()
    (input_dir / "bass.wav").write_bytes(b"audio")

    args = make_args(input_dir=input_dir, skip=[])
    fake_result = mock.Mock(returncode=1)
    fake_result.stderr = "rubberband failed badly"
    with mock.patch("onda.pitch.subprocess.run", return_value=fake_result):
        with pytest.raises(SystemExit) as exc:
            run_pitch(args)
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "rubberband failed" in captured.out.lower()


def test_run_pitch_creates_output_dir(make_args, tmp_path):
    """run_pitch must create the output directory."""
    input_dir = tmp_path / "in"
    input_dir.mkdir()
    (input_dir / "drums.wav").write_bytes(b"audio")
    output_dir = tmp_path / "new_output"

    args = make_args(input_dir=input_dir, output=str(output_dir), skip=("drums",))
    with mock.patch("onda.pitch.subprocess.run", return_value=mock.Mock(returncode=0)):
        run_pitch(args)

    assert output_dir.is_dir()
