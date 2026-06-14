package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// StemAction defines what happens to a stem after a pipeline step.
type StemAction string

const (
	StemSave    StemAction = "save"    // Guardar en resultado final
	ActionRoute StemAction = "route"   // Enviar al siguiente paso
	StemDiscard StemAction = "discard" // No guardar ni procesar
)

// StemRoute describes the routing of a single stem from a pipeline step.
type StemRoute struct {
	Action StemAction `json:"action"`
	Target string     `json:"target,omitempty"` // Paso destino: "result", "step:1", etc.
}

// PipelineStep defines a single step in a multi-step pipeline preset.
type PipelineStep struct {
	ID      string                `json:"id"`                // "viperx-1", "demucs-2", etc.
	Model   string                `json:"model"`             // nombre del modelo o ruta
	Type    string                `json:"type"`              // "viperx" | "demucs"
	Enabled bool                  `json:"enabled"`
	Stems   map[string]StemRoute  `json:"stems"`             // stem_name → routing
}

// Preset defines a complete audio processing preset with routing and pipeline chaining.
type Preset struct {
	Name        string         `json:"name"`
	Steps       []PipelineStep `json:"steps"`
	Pitch       int            `json:"pitch"`
	Description string         `json:"description"`
	Locked      bool           `json:"locked"` // true para los 4 predeterminados
}

// Presets is the map of all built-in presets (empty — seeded in presets.go).
var Presets = map[string]Preset{}

// PipelineFlags holds all parsed CLI flags for the pipeline subcommand.
type PipelineFlags struct {
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
}

// ParsePipelineFlags parses args (typically os.Args[2:]) and returns PipelineFlags.
func ParsePipelineFlags(args []string) (*PipelineFlags, error) {
	fs := flag.NewFlagSet("pipeline", flag.ContinueOnError)

	// Modern flags
	vocalModel := fs.String("vocal-model", "", "Vocal separation model")
	vocalOverlap := fs.Int("vocal-overlap", 0, "Vocal overlap")
	vocalKeep := fs.String("vocal-keep", "both", "Keep: both, vocals, instrumental")
	stemModel := fs.String("stem-model", "", "Stem separation model")
	stemKeep := fs.String("stem-keep", "", "Stems to keep: drums,bass,other,vocals or all")
	drumsModel := fs.String("drums-model", "", "Drums model (dedicated pass)")
	bassModel := fs.String("bass-model", "", "Bass model (dedicated pass)")
	otherModel := fs.String("other-model", "", "Other model (dedicated pass)")
	pitch := fs.Int("pitch", 0, "Pitch shift in semitones (0 = disabled)")
	input := fs.String("input", "", "Input audio file path")
	output := fs.String("output", "", "Output directory path")

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

	// Parse stem-keep into slice
	if *stemKeep != "" {
		parts := strings.Split(*stemKeep, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		flags.StemKeep = parts
	}

	// Validate
	if err := flags.Validate(); err != nil {
		return nil, err
	}

	return flags, nil
}

// Validate checks that all flags contain valid values.
func (f *PipelineFlags) Validate() error {
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
	b.WriteString("Flags:\n")
	b.WriteString("  --preset string          Preset to use (default: none, use flags directly)\n")
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
	b.WriteString("  --no-clean               Don't clean output directory (for pipeline chaining)\n")
	b.WriteString("  --input-from-step string Use this existing stem file as input (for chaining)\n")
	b.WriteString("  --help                   Show this help\n\n")
	b.WriteString("Presets (v2.8.0):\n")
	b.WriteString("  Voces Total       1 paso: ViperX separa voces + instrumental\n")
	b.WriteString("  Eliminador de Voz 1 paso: ViperX elimina voces, solo instrumental\n")
	b.WriteString("  Separador Completo 2 pasos: ViperX → Demucs (drums, bass, other, vocals)\n")
	b.WriteString("  Solo Instrumentos 1 paso: Demucs stems sin voces\n")

	return b.String()
}
