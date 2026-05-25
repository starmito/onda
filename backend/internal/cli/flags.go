package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Preset defines what models and parameters each preset uses.
type Preset struct {
	Name        string
	VocalModel  string
	VocalOverlap int
	StemModel   string
	DrumsModel  string
	BassModel   string
	OtherModel  string
	Pitch       int
	Description string
}

// Presets is the map of all available presets.
var Presets = map[string]Preset{
	"turbo": {
		Name:         "turbo",
		VocalModel:   "melband_kj",
		VocalOverlap: 2,
		StemModel:    "htdemucs_ft",
		Description:  "Rápido, ~8GB VRAM",
	},
	"balance": {
		Name:         "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		StemModel:    "htdemucs_ft",
		Description:  "Recomendado, ~12GB VRAM",
	},
	"master": {
		Name:         "master",
		VocalModel:   "polarformer",
		VocalOverlap: 8,
		StemModel:    "htdemucs_ft",
		BassModel:    "htdemucs_bass",
		Description:  "Máxima calidad vocal, ~12GB VRAM",
	},
	"ultimate": {
		Name:         "ultimate",
		VocalModel:   "polarformer",
		VocalOverlap: 8,
		StemModel:    "",
		DrumsModel:   "htdemucs_drums",
		BassModel:    "htdemucs_bass",
		OtherModel:   "viperx_other",
		Description:  "Mejor por stem, 4 pases dedicados, ~12GB VRAM",
	},
}

// PipelineFlags holds all parsed CLI flags for the pipeline subcommand.
type PipelineFlags struct {
	Preset       string
	VocalModel   string
	VocalOverlap int
	VocalKeep    string   // both, vocals, instrumental
	StemModel    string
	StemKeep     []string // drums, bass, other, vocals (o "all")
	DrumsModel   string
	BassModel    string
	OtherModel   string
	Pitch        int
	Input        string
	Output       string

	// Internal tracking for legacy flag overrides
	hasVocalOverlap bool
	hasPitch        bool
}

// ParsePipelineFlags parses args (typically os.Args[2:]) and returns PipelineFlags.
func ParsePipelineFlags(args []string) (*PipelineFlags, error) {
	fs := flag.NewFlagSet("pipeline", flag.ContinueOnError)

	// Modern flags
	preset := fs.String("preset", "balance", "Preset to use: turbo, balance, master, ultimate")
	vocalModel := fs.String("vocal-model", "", "Vocal separation model (overrides preset)")
	vocalOverlap := fs.Int("vocal-overlap", 0, "Vocal overlap (overrides preset)")
	vocalKeep := fs.String("vocal-keep", "both", "Keep: both, vocals, instrumental")
	stemModel := fs.String("stem-model", "", "Stem separation model")
	stemKeep := fs.String("stem-keep", "", "Stems to keep: drums,bass,other,vocals or all")
	drumsModel := fs.String("drums-model", "", "Drums model (dedicated pass)")
	bassModel := fs.String("bass-model", "", "Bass model (dedicated pass)")
	otherModel := fs.String("other-model", "", "Other model (dedicated pass)")
	pitch := fs.Int("pitch", 0, "Pitch shift in semitones (0 = disabled)")
	input := fs.String("input", "", "Input audio file path")
	output := fs.String("output", "", "Output directory path")

	// Legacy flags
	viperx := fs.Bool("viperx", false, "[legacy] Use ViperX for vocal separation (alias for --vocal-model viperx)")
	demucs := fs.Bool("demucs", false, "[legacy] Use Demucs for stem separation (alias for --stem-model htdemucs_ft)")
	viperxOverlap := fs.Int("viperx-overlap", 0, "[legacy] ViperX overlap (alias for --vocal-overlap)")
	demucsModel := fs.String("demucs-model", "", "[legacy] Demucs model (alias for --stem-model)")
	rubberband := fs.Bool("rubberband", false, "[legacy] Enable Rubberband pitch shift (alias for --pitch 0)")

	// Help flag
	help := fs.Bool("help", false, "Show help")

	// Suppress usage output on parse errors — we handle them ourselves
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("flag parsing error: %w", err)
	}

	if *help {
		fmt.Print(Help())
		os.Exit(0)
	}

	flags := &PipelineFlags{
		Preset:       *preset,
		VocalModel:   *vocalModel,
		VocalOverlap: *vocalOverlap,
		VocalKeep:    *vocalKeep,
		StemModel:    *stemModel,
		DrumsModel:   *drumsModel,
		BassModel:    *bassModel,
		OtherModel:   *otherModel,
		Pitch:        *pitch,
		Input:        *input,
		Output:       *output,
	}

	// Track whether explicit flags were set
	if *vocalOverlap != 0 {
		flags.hasVocalOverlap = true
	}
	if *pitch != 0 {
		flags.hasPitch = true
	}

	// Parse stem-keep into slice
	if *stemKeep != "" {
		parts := strings.Split(*stemKeep, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		flags.StemKeep = parts
	}

	// Apply legacy flag mappings
	if *viperx {
		if flags.VocalModel == "" {
			flags.VocalModel = "viperx"
		}
	}
	if *demucs {
		if flags.StemModel == "" {
			flags.StemModel = "htdemucs_ft"
		}
	}
	if *viperxOverlap > 0 {
		flags.VocalOverlap = *viperxOverlap
		flags.hasVocalOverlap = true
	}
	if *demucsModel != "" {
		flags.StemModel = *demucsModel
	}
	if *rubberband {
		if !flags.hasPitch {
			flags.Pitch = 0
		}
	}

	// Resolve preset (applies defaults, allows overrides)
	if err := flags.ResolvePreset(); err != nil {
		return nil, err
	}

	// Validate
	if err := flags.Validate(); err != nil {
		return nil, err
	}

	return flags, nil
}

// ResolvePreset applies a preset configuration, allowing individual flag overrides.
func (f *PipelineFlags) ResolvePreset() error {
	p, ok := Presets[f.Preset]
	if !ok {
		validPresets := make([]string, 0, len(Presets))
		for name := range Presets {
			validPresets = append(validPresets, name)
		}
		return fmt.Errorf("unknown preset %q: valid presets are %s", f.Preset, strings.Join(validPresets, ", "))
	}

	// Apply preset defaults if not overridden
	if f.VocalModel == "" {
		f.VocalModel = p.VocalModel
	}
	if !f.hasVocalOverlap {
		f.VocalOverlap = p.VocalOverlap
	}
	if f.StemModel == "" {
		f.StemModel = p.StemModel
	}
	if f.DrumsModel == "" {
		f.DrumsModel = p.DrumsModel
	}
	if f.BassModel == "" {
		f.BassModel = p.BassModel
	}
	if f.OtherModel == "" {
		f.OtherModel = p.OtherModel
	}
	if !f.hasPitch {
		f.Pitch = p.Pitch
	}

	return nil
}

// Validate checks that all flags contain valid values.
func (f *PipelineFlags) Validate() error {
	// Validate preset
	if _, ok := Presets[f.Preset]; !ok {
		validPresets := make([]string, 0, len(Presets))
		for name := range Presets {
			validPresets = append(validPresets, name)
		}
		return fmt.Errorf("unknown preset %q: valid presets are %s", f.Preset, strings.Join(validPresets, ", "))
	}

	// Validate vocal-keep
	validVocalKeep := map[string]bool{"both": true, "vocals": true, "instrumental": true}
	if !validVocalKeep[f.VocalKeep] {
		return fmt.Errorf("invalid --vocal-keep %q: must be one of both, vocals, instrumental", f.VocalKeep)
	}

	// Validate stem-keep values
	if len(f.StemKeep) > 0 {
		validStems := map[string]bool{"drums": true, "bass": true, "other": true, "vocals": true, "all": true}
		for _, s := range f.StemKeep {
			if !validStems[s] {
				return fmt.Errorf("invalid stem %q in --stem-keep: must be drums, bass, other, vocals, or all", s)
			}
		}
	}

	// Validate input
	if f.Input == "" {
		return fmt.Errorf("--input is required")
	}

	// Validate that vocal-model is not empty after resolution
	if f.VocalModel == "" {
		return fmt.Errorf("vocal model could not be resolved; specify --vocal-model or use a valid preset")
	}

	return nil
}

// Help returns the help text for the pipeline subcommand.
func Help() string {
	var b strings.Builder

	b.WriteString("Usage:\n")
	b.WriteString("  onda pipeline [flags]\n\n")
	b.WriteString("Run the audio separation pipeline.\n\n")
	b.WriteString("Flags (modern):\n")
	b.WriteString("  --preset string          Preset to use: turbo, balance, master, ultimate (default \"balance\")\n")
	b.WriteString("  --vocal-model string     Vocal separation model (overrides preset)\n")
	b.WriteString("  --vocal-overlap int      Vocal overlap size (overrides preset)\n")
	b.WriteString("  --vocal-keep string      What to keep: both, vocals, instrumental (default \"both\")\n")
	b.WriteString("  --stem-model string      Stem separation model (e.g. htdemucs_ft)\n")
	b.WriteString("  --stem-keep string       Stems to keep: drums,bass,other,vocals or all\n")
	b.WriteString("  --drums-model string     Drums model for dedicated pass\n")
	b.WriteString("  --bass-model string      Bass model for dedicated pass\n")
	b.WriteString("  --other-model string     Other model for dedicated pass\n")
	b.WriteString("  --pitch int              Pitch shift in semitones (0 = disabled) (default 0)\n")
	b.WriteString("  --input string           Input audio file path (required)\n")
	b.WriteString("  --output string          Output directory path\n")
	b.WriteString("  --help                   Show this help\n\n")
	b.WriteString("Flags (legacy — compatibility with pipeline.sh):\n")
	b.WriteString("  --viperx                 Use ViperX for vocal separation (alias for --vocal-model viperx)\n")
	b.WriteString("  --demucs                 Use Demucs for stem separation (alias for --stem-model htdemucs_ft)\n")
	b.WriteString("  --viperx-overlap int     ViperX overlap (alias for --vocal-overlap)\n")
	b.WriteString("  --demucs-model string    Demucs model (alias for --stem-model)\n")
	b.WriteString("  --rubberband             Enable Rubberband pitch shift (alias for --pitch 0)\n\n")
	b.WriteString("Presets:\n")
	b.WriteString("  turbo      Rápido, ~8GB VRAM — melband_kj + htdemucs_ft, overlap=2\n")
	b.WriteString("  balance    Recomendado, ~12GB VRAM — polarformer + htdemucs_ft, overlap=4\n")
	b.WriteString("  master     Máxima calidad vocal, ~12GB VRAM — polarformer, overlap=8, bass dedicated\n")
	b.WriteString("  ultimate   Mejor por stem, 4 pases dedicados, ~12GB VRAM — polarformer, all stem models\n")

	return b.String()
}
