"""onda_player — Multi-track stem player with real-time muting.
Lightweight, no Qt dependency. Uses sounddevice for low-latency playback.

Usage:
    from onda_player import MultiStemPlayer
    player = MultiStemPlayer()
    player.load("/path/to/stems/")       # loads drums, bass, other, vocals
    player.toggle("drums", True)
    player.toggle("bass", False)
    player.play()
    player.seek(30.0)                    # jump to 30s
    player.pitch_mode("/path/to/pitch/") # switch to pitch-shifted stems
"""

import threading
import time
from pathlib import Path

import numpy as np
import soundfile as sf
import sounddevice as sd

STEMS = ["drums", "bass", "other", "vocals"]


class MultiStemPlayer:
    """Plays multiple WAV stems simultaneously with per-track mute/volume."""

    def __init__(self, sample_rate=44100):
        self.sr = sample_rate
        self.stems = {}           # name → numpy array (channels, samples)
        self.enabled = {}         # name → bool
        self.volumes = {}         # name → float (0.0 – 1.0)
        self._stream = None
        self._pos = 0             # current position in samples
        self._playing = False
        self._paused = False
        self._lock = threading.Lock()
        self._duration = 0.0      # seconds
        self._on_time = None      # callback for GUI timer updates

    # ── Loading ──────────────────────────────────────────

    def load(self, directory: str, stems=None):
        """Load stems from a directory. Expects {stem}.wav files."""
        directory = Path(directory)
        stems = stems or STEMS
        loaded = {}
        for name in stems:
            path = directory / f"{name}.wav"
            if path.exists():
                data, sr = sf.read(str(path), dtype="float32", always_2d=True)
                data = data.T  # (channels, samples)
                # Resample if needed
                if sr != self.sr:
                    import librosa
                    data = librosa.resample(data, orig_sr=sr, target_sr=self.sr, axis=1)
                loaded[name] = data
                self.enabled.setdefault(name, True)
                self.volumes.setdefault(name, 1.0)
            else:
                print(f"   ⚠ {name}.wav not found in {directory}")

        with self._lock:
            self.stems = loaded
            self._pos = 0
            if loaded:
                self._duration = max(d.shape[1] for d in loaded.values()) / self.sr

        print(f"   ✓ Loaded {len(loaded)} stems ({self._duration:.1f}s)")

    def pitch_mode(self, pitch_dir: str):
        """Switch to pitch-shifted stems (or any alternate directory)."""
        self.load(pitch_dir)

    # ── Controls ─────────────────────────────────────────

    def play(self):
        """Start or resume playback."""
        with self._lock:
            if self._stream and self._paused:
                self._paused = False
                self._playing = True
                return

            self._stop_stream()
            if not self.stems:
                return

            # Ensure position is within bounds
            max_samples = max(d.shape[1] for d in self.stems.values())
            self._pos = min(self._pos, max_samples - 1)
            self._on_time = time.time()

            self._stream = sd.OutputStream(
                samplerate=self.sr,
                channels=2,
                dtype="float32",
                callback=self._callback,
                finished_callback=self._on_stream_done,
            )
            self._stream.start()
            self._playing = True
            self._paused = False

    def pause(self):
        """Pause playback (position preserved)."""
        with self._lock:
            if self._stream and self._playing:
                self._paused = True
                self._playing = False
                self._stream.stop()

    def stop(self):
        """Stop playback and reset to beginning."""
        with self._lock:
            self._stop_stream()
            self._pos = 0
            self._playing = False
            self._paused = False

    def seek(self, seconds: float):
        """Jump to a position in seconds."""
        with self._lock:
            self._pos = int(seconds * self.sr)
            max_samples = max(d.shape[1] for d in self.stems.values()) if self.stems else 0
            self._pos = min(max(0, self._pos), max_samples - 1)
            if self._playing:
                was_playing = True
                self._stop_stream()
                self.play()
            self._on_time = time.time()

    # ── Track control ────────────────────────────────────

    def toggle(self, name: str, enabled: bool):
        """Enable/disable a stem. Changes take effect immediately."""
        with self._lock:
            if name in self.enabled:
                self.enabled[name] = enabled

    def set_volume(self, name: str, volume: float):
        """Set volume 0.0–1.0 for a stem."""
        with self._lock:
            if name in self.volumes:
                self.volumes[name] = max(0.0, min(1.0, volume))

    def is_playing(self) -> bool:
        return self._playing

    def is_paused(self) -> bool:
        return self._paused

    def position(self) -> float:
        """Current position in seconds (estimated)."""
        with self._lock:
            if self._playing and self._on_time:
                elapsed = time.time() - self._on_time
                return (self._pos / self.sr) + elapsed
            return self._pos / self.sr

    def duration(self) -> float:
        return self._duration

    def loaded_stems(self) -> list:
        return sorted(self.stems.keys())

    @property
    def stem_names(self) -> list:
        return self.loaded_stems()

    def is_enabled(self, name: str) -> bool:
        return self.enabled.get(name, False)

    def volume(self, name: str) -> float:
        return self.volumes.get(name, 1.0)

    # ── Cleanup ──────────────────────────────────────────

    def close(self):
        self._stop_stream()

    def __del__(self):
        self.close()

    # ── Internal ─────────────────────────────────────────

    def _stop_stream(self):
        if self._stream:
            try:
                self._stream.stop()
                self._stream.close()
            except Exception:
                pass
            self._stream = None

    def _on_stream_done(self):
        with self._lock:
            self._playing = False
            self._paused = False
            self._on_time = None

    def _callback(self, outdata, frames, timer_info, status):
        """sounddevice callback — mixes stems in real time."""
        if status:
            print(f"   ⚠ stream status: {status}")

        with self._lock:
            if not self._playing:
                outdata.fill(0)
                raise sd.CallbackStop

            # Determine output slice
            start = self._pos
            end = start + frames

            # Mix enabled stems
            mixed = np.zeros((frames, 2), dtype=np.float32)
            for name, data in self.stems.items():
                if not self.enabled.get(name, True):
                    continue
                vol = self.volumes.get(name, 1.0)
                n_samples = data.shape[1]
                if start < n_samples:
                    chunk_end = min(end, n_samples)
                    chunk = data[:, start:chunk_end].T  # (samples, channels)
                    if chunk.shape[0] < frames:
                        padded = np.zeros((frames, 2), dtype=np.float32)
                        padded[: chunk.shape[0]] = chunk
                        chunk = padded
                    mixed += chunk * vol

            # Clamp
            mixed = np.clip(mixed, -1.0, 1.0)
            outdata[:] = mixed

            # Advance position
            self._pos = end

            # Stop if past end
            max_samples = max(d.shape[1] for d in self.stems.values())
            if self._pos >= max_samples:
                self._pos = 0
                self._playing = False
                raise sd.CallbackStop


# ── Standalone test ──────────────────────────────────────
if __name__ == "__main__":
    import sys

    dir_path = sys.argv[1] if len(sys.argv) > 1 else "/tmp/onda_test/htdemucs/prueba_onda2_instrumental"
    print(f"🎧 MultiStemPlayer test — loading {dir_path}")

    player = MultiStemPlayer()
    player.load(dir_path)
    player.toggle("vocals", True)
    player.toggle("drums", True)
    player.toggle("bass", True)
    player.toggle("other", True)

    print("\nControls: p=play, space=pause, s=stop, q=quit")
    print("         1-4 toggle drums/bass/other/vocals")
    print("         ← → seek -5/+5s")

    def print_status():
        pos = player.position()
        dur = player.duration()
        bar_len = 30
        pct = min(pos / dur, 1.0) if dur > 0 else 0
        filled = int(bar_len * pct)
        bar = "█" * filled + "░" * (bar_len - filled)
        stems_status = " ".join(
            f"{'☑' if player.is_enabled(s) else '☐'}{s[:3]}"
            for s in player.stem_names
        )
        print(f"\r  {bar} {pos:.1f}s/{dur:.1f}s  {stems_status}  ", end="", flush=True)

    try:
        import tty, termios, select

        fd = sys.stdin.fileno()
        old = termios.tcgetattr(fd)
        tty.setcbreak(fd)

        player.play()

        while True:
            if select.select([sys.stdin], [], [], 0.1)[0]:
                key = sys.stdin.read(1)
                if key == "q":
                    break
                elif key == "p":
                    player.play()
                elif key == " ":
                    if player.is_playing():
                        player.pause()
                    else:
                        player.play()
                elif key == "s":
                    player.stop()
                elif key == "1":
                    player.toggle("drums", not player.is_enabled("drums"))
                elif key == "2":
                    player.toggle("bass", not player.is_enabled("bass"))
                elif key == "3":
                    player.toggle("other", not player.is_enabled("other"))
                elif key == "4":
                    player.toggle("vocals", not player.is_enabled("vocals"))
                elif key == "\x1b":
                    # Arrow keys
                    seq = sys.stdin.read(2)
                    if seq == "[C":  # right
                        player.seek(player.position() + 5)
                    elif seq == "[D":  # left
                        player.seek(player.position() - 5)
            print_status()
            if not player.is_playing() and not player.is_paused():
                break

    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)
        player.stop()
        player.close()
        print("\n👋 Done!")
