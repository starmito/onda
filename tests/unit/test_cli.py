"""Tests for the onda CLI argument parsing and dispatch."""

import sys
from types import SimpleNamespace
from unittest import mock

import pytest

from onda.cli import main


class TestCLIParsing:
    """Unit tests for CLI argument parsing without invoking heavy inference."""

    def test_cli_requires_subcommand(self, capsys):
        """Calling onda without a subcommand must print usage and exit."""
        with pytest.raises(SystemExit) as exc:
            main()
        assert exc.value.code != 0
        captured = capsys.readouterr()
        assert "usage:" in captured.err.lower()

    def test_cli_unknown_subcommand(self, capsys):
        """An unknown subcommand must exit with an error."""
        with pytest.raises(SystemExit) as exc:
            with mock.patch.object(sys, "argv", ["onda", "nope"]):
                main()
        assert exc.value.code != 0

    def test_viperx_requires_model(self, capsys):
        """onda viperx requires --model."""
        with pytest.raises(SystemExit):
            with mock.patch.object(sys, "argv", ["onda", "viperx", "song.wav"]):
                main()

    def test_viperx_parses_defaults(self, capsys):
        """viperx defaults: cuda device, overlap 8, output_viperx output."""
        args = SimpleNamespace(
            command="viperx",
            input="song.wav",
            model="model.ckpt",
            config=None,
            overlap=8,
            output="output_viperx",
            device="cuda",
        )
        with mock.patch(
            "onda.cli.argparse.ArgumentParser.parse_args", return_value=args
        ), mock.patch("onda.viperx.run_viperx") as mock_run:
            main()
            mock_run.assert_called_once_with(args)

    def test_demucs_parses_defaults(self):
        """demucs defaults: htdemucs model, cuda device."""
        args = SimpleNamespace(
            command="demucs",
            input="inst.wav",
            model="htdemucs",
            output="output_demucs",
            device="cuda",
        )
        with mock.patch(
            "onda.cli.argparse.ArgumentParser.parse_args", return_value=args
        ), mock.patch("onda.demucs.run_demucs") as mock_run:
            main()
            mock_run.assert_called_once_with(args)

    def test_pitch_parses_defaults(self):
        """pitch defaults: 2 semitones, skip drums."""
        args = SimpleNamespace(
            command="pitch",
            input_dir="stems/",
            semitones=2.0,
            output="output_pitchshift",
            skip=["drums"],
        )
        with mock.patch(
            "onda.cli.argparse.ArgumentParser.parse_args", return_value=args
        ), mock.patch("onda.pitch.run_pitch") as mock_run:
            main()
            mock_run.assert_called_once_with(args)

    def test_viperx_dispatch(self):
        """main() imports and calls run_viperx for the viperx subcommand."""
        args = SimpleNamespace(
            command="viperx",
            input="x.wav",
            model="m.ckpt",
            config=None,
            overlap=4,
            output="out",
            device="cpu",
        )
        with mock.patch(
            "onda.cli.argparse.ArgumentParser.parse_args", return_value=args
        ), mock.patch("onda.viperx.run_viperx") as mock_run:
            main()
            mock_run.assert_called_once_with(args)

    def test_demucs_dispatch(self):
        """main() imports and calls run_demucs for the demucs subcommand."""
        args = SimpleNamespace(
            command="demucs",
            input="x.wav",
            model="htdemucs_ft",
            output="out",
            device="cpu",
        )
        with mock.patch(
            "onda.cli.argparse.ArgumentParser.parse_args", return_value=args
        ), mock.patch("onda.demucs.run_demucs") as mock_run:
            main()
            mock_run.assert_called_once_with(args)

    def test_pitch_dispatch(self):
        """main() imports and calls run_pitch for the pitch subcommand."""
        args = SimpleNamespace(
            command="pitch",
            input_dir="stems/",
            semitones=-3.0,
            output="out",
            skip=["drums"],
        )
        with mock.patch(
            "onda.cli.argparse.ArgumentParser.parse_args", return_value=args
        ), mock.patch("onda.pitch.run_pitch") as mock_run:
            main()
            mock_run.assert_called_once_with(args)
