"""onda_gui.py — Onda Pipeline tab (standalone, Sun Valley themed).
Integrates: model selection, params, execution queue, multi-stem player.

Run: python onda_gui.py
"""

import os
import sys
import json
import threading
import subprocess
import tkinter as tk
from tkinter import ttk, filedialog, messagebox
from pathlib import Path

# Player import
try:
    from onda.player import MultiStemPlayer
except ImportError:
    from gui_data.onda_player import MultiStemPlayer

# Sun Valley theme
try:
    from gui_data.sv_ttk import set_theme
    THEME_AVAILABLE = True
except ImportError:
    THEME_AVAILABLE = False


# ── GPU Profiles ────────────────────────────────────────

GPU_PROFILES = {
    "Turbo (4 GB)":    {"overlap": 2,  "batch": 1},
    "Balance (8 GB)":  {"overlap": 4,  "batch": 2},
    "Calidad (12 GB)": {"overlap": 8,  "batch": 4},
    "Master (16+ GB)": {"overlap": 16, "batch": 8},
}

# ── Saved settings ──────────────────────────────────────

SETTINGS_FILE = Path.home() / ".onda_settings.json"

def load_settings():
    if SETTINGS_FILE.exists():
        return json.loads(SETTINGS_FILE.read_text())
    return {}

def save_settings(data):
    SETTINGS_FILE.write_text(json.dumps(data, indent=2))


# ═════════════════════════════════════════════════════════
#  MAIN GUI
# ═════════════════════════════════════════════════════════

class OndaGUI:
    def __init__(self):
        self.root = tk.Tk()
        self.root.title("🌊 Onda — Pipeline")
        self.root.geometry("800x700")
        self.root.minsize(600, 500)

        if THEME_AVAILABLE:
            set_theme("dark")

        self.player = MultiStemPlayer()
        self.queue_running = False
        self.settings = load_settings()

        self._build_ui()
        self._load_saved()

    # ── UI Construction ─────────────────────────────────

    def _build_ui(self):
        # Main notebook
        nb = ttk.Notebook(self.root)
        nb.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        # Tab 1: Pipeline
        tab_pipe = ttk.Frame(nb)
        nb.add(tab_pipe, text="⚙ Pipeline")
        self._build_pipeline_tab(tab_pipe)

        # Tab 2: Player
        tab_player = ttk.Frame(nb)
        nb.add(tab_player, text="🎧 Player")
        self._build_player_tab(tab_player)

    # ── Pipeline Tab ────────────────────────────────────

    def _build_pipeline_tab(self, parent):
        pad = {"padx": 5, "pady": 3}

        # ── Model selection ──
        frm = ttk.LabelFrame(parent, text="🎯 Modelo Viperx", padding=8)
        frm.pack(fill=tk.X, padx=8, pady=4)

        ttk.Label(frm, text="Checkpoint (.ckpt):").grid(row=0, column=0, sticky="w", **pad)
        self.model_var = tk.StringVar(value=self.settings.get("last_model", ""))
        ttk.Entry(frm, textvariable=self.model_var, width=50).grid(row=0, column=1, sticky="ew", **pad)
        ttk.Button(frm, text="📂 Browse", command=self._browse_model).grid(row=0, column=2, **pad)

        ttk.Label(frm, text="Config (.yaml):").grid(row=1, column=0, sticky="w", **pad)
        self.config_var = tk.StringVar(value=self.settings.get("last_config", "auto"))
        ttk.Entry(frm, textvariable=self.config_var, width=50).grid(row=1, column=1, sticky="ew", **pad)
        ttk.Label(frm, text="(auto-detect si se deja vacío)", foreground="gray").grid(
            row=1, column=2, sticky="w", **pad
        )
        frm.columnconfigure(1, weight=1)

        # ── Parameters ──
        frm2 = ttk.LabelFrame(parent, text="📐 Parámetros", padding=8)
        frm2.pack(fill=tk.X, padx=8, pady=4)

        # Overlap
        ttk.Label(frm2, text="Overlap:").grid(row=0, column=0, sticky="w", **pad)
        self.overlap_var = tk.IntVar(value=8)
        ttk.Scale(frm2, from_=2, to=16, variable=self.overlap_var,
                  orient=tk.HORIZONTAL, command=lambda v: self._update_overlap_label()
        ).grid(row=0, column=1, sticky="ew", **pad)
        self.overlap_label = ttk.Label(frm2, text="8")
        self.overlap_label.grid(row=0, column=2, **pad)

        # Demucs model
        ttk.Label(frm2, text="Demucs:").grid(row=1, column=0, sticky="w", **pad)
        self.demucs_var = tk.StringVar(value="htdemucs")
        ttk.Combobox(frm2, textvariable=self.demucs_var, values=["htdemucs", "htdemucs_ft"],
                     state="readonly", width=15).grid(row=1, column=1, sticky="w", **pad)

        # Semitones
        ttk.Label(frm2, text="Semitonos:").grid(row=2, column=0, sticky="w", **pad)
        self.semi_var = tk.DoubleVar(value=2.0)
        ttk.Spinbox(frm2, textvariable=self.semi_var, from_=-12, to=12,
                    increment=0.5, width=8).grid(row=2, column=1, sticky="w", **pad)

        # GPU Profile
        ttk.Label(frm2, text="Perfil GPU:").grid(row=3, column=0, sticky="w", **pad)
        self.profile_var = tk.StringVar(value="Calidad (12 GB)")
        cb = ttk.Combobox(frm2, textvariable=self.profile_var,
                          values=list(GPU_PROFILES.keys()), state="readonly", width=18)
        cb.grid(row=3, column=1, sticky="w", **pad)
        cb.bind("<<ComboboxSelected>>", lambda e: self._apply_profile())

        frm2.columnconfigure(1, weight=1)

        # ── Mode ──
        frm3 = ttk.LabelFrame(parent, text="▶ Modo", padding=8)
        frm3.pack(fill=tk.X, padx=8, pady=4)

        self.step_mode = tk.BooleanVar(value=False)
        ttk.Checkbutton(frm3, text="Paso a paso (si no: pipeline completo)",
                        variable=self.step_mode).pack(anchor="w")

        # Buttons
        btn_frame = ttk.Frame(frm3)
        btn_frame.pack(fill=tk.X, pady=5)
        ttk.Button(btn_frame, text="▶ Ejecutar", command=self._run_pipeline).pack(
            side=tk.LEFT, padx=3
        )
        ttk.Button(btn_frame, text="⏹ Cancelar", command=self._cancel).pack(
            side=tk.LEFT, padx=3
        )

        # ── Console ──
        frm4 = ttk.LabelFrame(parent, text="📋 Consola", padding=5)
        frm4.pack(fill=tk.BOTH, expand=True, padx=8, pady=4)

        self.console = tk.Text(frm4, height=10, bg="#1a1a2e", fg="#e0e0e0",
                               insertbackground="white", font=("Consolas", 9),
                               state=tk.DISABLED, wrap=tk.WORD)
        self.console.pack(fill=tk.BOTH, expand=True)

        scroll = ttk.Scrollbar(self.console, command=self.console.yview)
        scroll.pack(side=tk.RIGHT, fill=tk.Y)
        self.console.configure(yscrollcommand=scroll.set)

    # ── Player Tab ──────────────────────────────────────

    def _build_player_tab(self, parent):
        pad = {"padx": 5, "pady": 3}

        # Top: load stems
        frm_top = ttk.Frame(parent)
        frm_top.pack(fill=tk.X, padx=8, pady=4)

        ttk.Button(frm_top, text="📂 Cargar stems", command=self._load_stems).pack(
            side=tk.LEFT, padx=3
        )
        ttk.Button(frm_top, text="🔄 Pitch mode",
                   command=lambda: self._load_stems(pitch=True)).pack(
            side=tk.LEFT, padx=3
        )

        self.player_status = tk.StringVar(value="No stems cargados")
        ttk.Label(frm_top, textvariable=self.player_status).pack(side=tk.LEFT, padx=10)

        # Stem checkboxes + volume
        frm_stems = ttk.LabelFrame(parent, text="Stems", padding=8)
        frm_stems.pack(fill=tk.X, padx=8, pady=4)

        self.stem_vars = {}
        self.volume_scales = {}
        row = 0
        for name in ["drums", "bass", "other", "vocals"]:
            var = tk.BooleanVar(value=True)
            self.stem_vars[name] = var
            ttk.Checkbutton(frm_stems, text=name, variable=var,
                            command=lambda n=name: self._on_toggle(n)
            ).grid(row=row, column=0, sticky="w", **pad)

            scale = ttk.Scale(frm_stems, from_=0, to=100, value=100,
                              orient=tk.HORIZONTAL,
                              command=lambda v, n=name: self._on_volume(n, float(v)))
            scale.grid(row=row, column=1, sticky="ew", **pad)
            self.volume_scales[name] = scale
            row += 1

        frm_stems.columnconfigure(1, weight=1)

        # Transport
        frm_trans = ttk.Frame(parent)
        frm_trans.pack(fill=tk.X, padx=8, pady=4)

        ttk.Button(frm_trans, text="▶", width=3, command=self.player.play).pack(
            side=tk.LEFT, padx=2
        )
        ttk.Button(frm_trans, text="⏸", width=3, command=self.player.pause).pack(
            side=tk.LEFT, padx=2
        )
        ttk.Button(frm_trans, text="⏹", width=3, command=self.player.stop).pack(
            side=tk.LEFT, padx=2
        )

        # Seek bar
        self.seek_var = tk.DoubleVar(value=0)
        self.seek_scale = ttk.Scale(frm_trans, from_=0, to=100, variable=self.seek_var,
                                    orient=tk.HORIZONTAL,
                                    command=self._on_seek)
        self.seek_scale.pack(side=tk.LEFT, fill=tk.X, expand=True, padx=8)
        ttk.Button(frm_trans, text="◀ -5s", command=lambda: self._seek_rel(-5)).pack(
            side=tk.LEFT, padx=1
        )
        ttk.Button(frm_trans, text="+5s ▶", command=lambda: self._seek_rel(5)).pack(
            side=tk.LEFT, padx=1
        )

        self.time_label = tk.StringVar(value="0:00 / 0:00")
        ttk.Label(frm_trans, textvariable=self.time_label).pack(side=tk.LEFT, padx=5)

        # Start the player update timer
        self._update_player_ui()

    # ── Actions ─────────────────────────────────────────

    def _browse_model(self):
        path = filedialog.askopenfilename(
            title="Selecciona checkpoint (.ckpt)",
            filetypes=[("Checkpoint", "*.ckpt"), ("All files", "*.*")]
        )
        if path:
            self.model_var.set(path)
            # Auto-detect yaml
            ckpt_dir = os.path.dirname(path)
            ckpt_name = os.path.splitext(os.path.basename(path))[0]
            yamls = [f for f in os.listdir(ckpt_dir)
                     if f.endswith(".yaml") and f.startswith(ckpt_name)]
            if yamls:
                self.config_var.set(os.path.join(ckpt_dir, yamls[0]))
            else:
                self.config_var.set("")
            self._save()

    def _update_overlap_label(self):
        self.overlap_label.configure(text=str(int(self.overlap_var.get())))

    def _apply_profile(self):
        profile = GPU_PROFILES.get(self.profile_var.get(), {})
        if "overlap" in profile:
            self.overlap_var.set(profile["overlap"])
            self._update_overlap_label()

    def _log(self, text):
        self.console.configure(state=tk.NORMAL)
        self.console.insert(tk.END, text + "\n")
        self.console.see(tk.END)
        self.console.configure(state=tk.DISABLED)

    def _save(self):
        self.settings["last_model"] = self.model_var.get()
        self.settings["last_config"] = self.config_var.get()
        save_settings(self.settings)

    def _load_saved(self):
        pass  # Already set during UI construction

    def _cancel(self):
        self.queue_running = False
        self._log("⏹ Cancelado por el usuario")

    # ── Execution ───────────────────────────────────────

    def _run_pipeline(self):
        if self.queue_running:
            self._log("⚠ Ya hay un pipeline en ejecución")
            return

        model = self.model_var.get()
        if not model:
            messagebox.showerror("Error", "Selecciona un modelo .ckpt")
            return

        config = self.config_var.get()
        input_file = filedialog.askopenfilename(
            title="Selecciona audio de entrada",
            filetypes=[("Audio", "*.mp3 *.wav *.flac *.ogg"), ("All files", "*.*")]
        )
        if not input_file:
            return

        self.queue_running = True
        self._save()

        base_name = os.path.splitext(os.path.basename(input_file))[0]
        out_dir = os.path.join(os.path.dirname(input_file) or ".", f"onda_{base_name}")

        steps = [
            {
                "name": "Viperx",
                "cmd": [
                    "onda", "viperx", input_file,
                    "-m", model,
                    "-o", out_dir,
                    "--overlap", str(self.overlap_var.get()),
                ] + (["-c", config] if config and config != "auto" else []),
            },
            {
                "name": "Demucs",
                "cmd": [
                    "onda", "demucs",
                    os.path.join(out_dir, f"{base_name}_instrumental.wav"),
                    "-m", self.demucs_var.get(),
                    "-o", out_dir,
                ],
            },
            {
                "name": "Pitch",
                "cmd": [
                    "onda", "pitch",
                    os.path.join(out_dir, "htdemucs" if "ft" not in self.demucs_var.get()
                                 else self.demucs_var.get().replace("_ft", ""),
                                 f"{base_name}_instrumental"),
                    "-s", str(self.semi_var.get()),
                    "-o", os.path.join(out_dir, "pitchshifted"),
                ],
            },
        ]

        if self.step_mode.get():
            self._run_steps(steps)
        else:
            threading.Thread(target=self._run_all, args=(steps, out_dir),
                             daemon=True).start()

    def _run_steps(self, steps):
        """Run one step at a time (user clicks Execute for each)."""
        # For step mode, just run the first pending step
        for step in steps:
            if not hasattr(step, "done"):
                self._log(f"\n{'='*50}")
                self._log(f"⚙ Ejecutando: {step['name']}")
                self._log(f"{'='*50}")
                self._run_cmd(step["cmd"])
                step["done"] = True
                self._log(f"✅ {step['name']} completado")
                self._try_load_player()
                break
        else:
            self._log("\n🎉 Pipeline completo!")
            self.queue_running = False

    def _run_all(self, steps, out_dir):
        for step in steps:
            if not self.queue_running:
                break
            self._log(f"\n{'='*50}")
            self._log(f"⚙ {step['name']}...")
            self._log(f"{'='*50}")
            self._run_cmd(step["cmd"])
            self._log(f"✅ {step['name']} completado")
            self.root.after(0, self._try_load_player)

        self._log(f"\n🎉 Pipeline completo! Output: {out_dir}")
        self.queue_running = False

    def _run_cmd(self, cmd):
        try:
            result = subprocess.run(
                cmd, capture_output=True, text=True, timeout=600
            )
            if result.stdout:
                for line in result.stdout.splitlines():
                    self.root.after(0, lambda l=line: self._log(f"  {l}"))
            if result.returncode != 0:
                self.root.after(0,
                    lambda: self._log(f"  ⚠ Error ({result.returncode}): {result.stderr[-300:]}")
                )
        except subprocess.TimeoutExpired:
            self._log("  ⏰ Timeout (10 min)")

    def _try_load_player(self):
        """Find stem dir and load into player."""
        # Try to find the latest output
        cwd = os.getcwd()
        for root, dirs, files in os.walk(cwd):
            if all(f"{s}.wav" in files for s in ["drums", "bass", "other", "vocals"]):
                self._load_stems_dir(root)
                return

    # ── Player wiring ───────────────────────────────────

    def _load_stems(self, pitch=False):
        dir_path = filedialog.askdirectory(title="Directorio con stems (.wav)")
        if dir_path:
            self._load_stems_dir(dir_path)

    def _load_stems_dir(self, dir_path):
        self.player.load(dir_path)
        names = self.player.stem_names
        self.player_status.set(f"{len(names)} stems — {self.player.duration():.0f}s")
        # Update checkboxes
        for name, var in self.stem_vars.items():
            if name in names:
                var.set(self.player.is_enabled(name))
            else:
                var.set(False)

    def _on_toggle(self, name):
        self.player.toggle(name, self.stem_vars[name].get())

    def _on_volume(self, name, value):
        self.player.set_volume(name, value / 100.0)

    def _on_seek(self, value):
        pct = float(value) / 100.0
        dur = self.player.duration()
        if dur > 0:
            self.player.seek(pct * dur)

    def _seek_rel(self, delta):
        self.player.seek(self.player.position() + delta)

    def _update_player_ui(self):
        """Periodic UI update for seek bar and time label."""
        pos = self.player.position()
        dur = self.player.duration()
        if dur > 0:
            self.seek_var.set((pos / dur) * 100)

        def fmt(t):
            m, s = divmod(int(t), 60)
            return f"{m}:{s:02d}"

        self.time_label.set(f"{fmt(pos)} / {fmt(dur)}")
        self.root.after(200, self._update_player_ui)

    # ── Run ─────────────────────────────────────────────

    def run(self):
        self.root.mainloop()


def main():
    gui = OndaGUI()
    gui.run()


if __name__ == "__main__":
    main()
