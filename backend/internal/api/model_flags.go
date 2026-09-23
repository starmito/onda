package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModelFlagValue is a single flag as returned by the config API.
// It contains the effective value plus the declaration (default, range,
// editable, description and quality/VRAM/speed metadata) so the frontend can
// render controls without hardcoded tables.
type ModelFlagValue struct {
	Name        string      `json:"name"`
	Value       interface{} `json:"value"`
	Default     interface{} `json:"default"`
	Min         interface{} `json:"min,omitempty"`
	Max         interface{} `json:"max,omitempty"`
	Step        interface{} `json:"step,omitempty"`
	Editable    bool        `json:"editable"`
	Type        string      `json:"type,omitempty"`
	Choices     []string    `json:"choices,omitempty"`
	Description string      `json:"description,omitempty"`
	Affects     []string    `json:"affects,omitempty"`
	BetterSide  string      `json:"better_side,omitempty"`
}

// ModelFlagsResponse is returned by GET /api/models/{name}/config.
type ModelFlagsResponse struct {
	Model    string           `json:"model"`
	Flags    []ModelFlagValue `json:"flags"`
	Stems    []string         `json:"stems,omitempty"`
	NumStems int              `json:"num_stems,omitempty"`
	Target   *string          `json:"target,omitempty"`
}

// modelFlagDef is the internal representation of a flag declaration.
type modelFlagDef struct {
	Default     interface{} `json:"default"`
	Min         interface{} `json:"min,omitempty"`
	Max         interface{} `json:"max,omitempty"`
	Step        interface{} `json:"step,omitempty"`
	Editable    bool        `json:"editable"`
	Type        string      `json:"type,omitempty"`
	Choices     []string    `json:"choices,omitempty"`
	Description string      `json:"description,omitempty"`
	Affects     []string    `json:"affects,omitempty"`
	BetterSide  string      `json:"better_side,omitempty"`
}

// knownFlags holds the generic ranges, defaults and user-facing metadata for
// every supported flag.  These are used both as fallback and as range metadata
// when the manifest does not declare its own bounds.
var knownFlags = map[string]modelFlagDef{
	"segment_size": {
		Default:     512,
		Min:         128,
		Max:         2048,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Número de muestras de audio que el modelo procesa en cada ventana de análisis. Valores mayores suelen mejorar la calidad hasta el óptimo del modelo, pero consumen más VRAM.",
		Affects:     []string{"quality", "vram"},
		BetterSide:  "quality",
	},
	"num_overlap": {
		Default:     4,
		Min:         1,
		Max:         8,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Número de ventanas solapadas entre segmentos consecutivos. Más solapamiento reduce artefactos de costura y mejora la calidad, a costa de más VRAM y tiempo de proceso.",
		Affects:     []string{"quality", "vram"},
		BetterSide:  "quality",
	},
	"chunk_size": {
		Default:     0,
		Min:         0,
		Max:         600,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Duración máxima de cada trozo procesado, en segundos. 0 procesa la canción entera de una vez (máxima calidad, más VRAM).",
		Affects:     []string{"quality", "vram"},
		BetterSide:  "quality",
	},
	"batch_size": {
		Default:     1,
		Min:         1,
		Max:         8,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Número de segmentos procesados a la vez. Valores mayores aceleran la separación y usan más VRAM, sin cambiar la calidad del resultado.",
		Affects:     []string{"vram", "speed"},
		BetterSide:  "vram",
	},
	"device": {
		Default:     "cuda",
		Editable:    true,
		Type:        "choice",
		Choices:     []string{"cuda", "cpu"},
		Description: "Dispositivo de cálculo: CUDA (GPU) o CPU.",
	},
	"shifts": {
		Default:     1,
		Min:         1,
		Max:         10,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Número de predicciones con pequeños desplazamientos temporales que se promedian. Aumentar mejora la calidad a costa de mucho más tiempo.",
		Affects:     []string{"quality", "speed"},
		BetterSide:  "quality",
	},
	"segment": {
		Default:     0,
		Min:         0,
		Max:         7,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Longitud de los segmentos analizados por Demucs, en segundos. 0 deja que el modelo elija automáticamente. Valores mayores suelen dar mejor calidad hasta el óptimo del modelo.",
		Affects:     []string{"quality"},
		BetterSide:  "quality",
	},
	"jobs": {
		Default:     1,
		Min:         1,
		Max:         8,
		Step:        1,
		Editable:    true,
		Type:        "int",
		Description: "Número de trabajos paralelos durante la separación. Más trabajos aceleran el proceso pero no afectan la calidad.",
		Affects:     []string{"speed"},
		BetterSide:  "speed",
	},
}

// flagsByType maps a canonical model type to the flag names it exposes.
// This is only used as a fallback when the model manifest does not declare
// flags. Models with a manifest are the single source of truth for their own
// flag set.
var flagsByType = map[string][]string{
	"bs_roformer":       {"segment_size", "num_overlap", "chunk_size", "batch_size", "device"},
	"mel_band_roformer": {"segment_size", "num_overlap", "chunk_size", "batch_size", "device"},
	"mdx23c":            {"segment_size", "num_overlap", "batch_size", "device"},
	"mdx_net":           {"segment_size", "num_overlap", "batch_size", "device"},
	"scnet":             {"segment_size", "num_overlap", "chunk_size", "batch_size", "device"},
	"demucs":            {"shifts", "segment", "jobs", "device"},
	"htdemucs":          {"shifts", "segment", "jobs", "device"},
}

// loadModelManifestFlags returns flag declarations from the model manifest, if present.
func loadModelManifestFlags(name string) (map[string]modelFlagDef, bool) {
	modelDir, _, found := searchModelOnDisk(name)
	if !found {
		if strings.EqualFold(name, "htdemucs_ft") {
			return builtInHtdemucsFtManifestFlags(), true
		}
		return nil, false
	}
	manifest, ok := loadModelManifest(modelDir)
	if !ok || len(manifest.Flags) == 0 {
		if strings.EqualFold(name, "htdemucs_ft") {
			return builtInHtdemucsFtManifestFlags(), true
		}
		return nil, false
	}
	return manifest.Flags, true
}

// builtInHtdemucsFtManifestFlags returns the canonical flag set for the
// built-in htdemucs_ft Demucs model.  It is used both in tests (where the model
// is not installed on disk) and as a safe fallback in production.
func builtInHtdemucsFtManifestFlags() map[string]modelFlagDef {
	flags := make(map[string]modelFlagDef, len(knownFlags))
	for _, name := range flagsByType["demucs"] {
		flags[name] = copyFlagDef(knownFlags[name])
	}
	// Defaults inferred from the shipped htdemucs_ft.yaml, clamped to the
	// documented valid ranges.
	setFlagDefault(flags, "shifts", 10)
	setFlagDefault(flags, "segment", 7)
	setFlagDefault(flags, "jobs", 8)
	return flags
}

// fallbackFlagsForModel returns generic flag declarations inferred from the
// model type/name and its shipped YAML (if any).
func fallbackFlagsForModel(name string) map[string]modelFlagDef {
	modelType := inferModelTypeFromName(name)
	flagNames, ok := flagsByType[modelType]
	if !ok {
		flagNames = []string{"segment_size", "overlap", "chunk_size", "batch_size", "shifts", "segment", "jobs", "device"}
	}

	flags := make(map[string]modelFlagDef, len(flagNames))
	for _, fn := range flagNames {
		if def, ok := knownFlags[fn]; ok {
			flags[fn] = copyFlagDef(def)
		}
	}

	if yamlPath := findModelYaml(name); yamlPath != "" {
		applyYamlDefaultsToFlags(yamlPath, flags)
	}

	return flags
}

// inferModelTypeFromName guesses a canonical model type from the model name.
func inferModelTypeFromName(name string) string {
	if mt, ok := detectModelTypeByName("", name); ok {
		return mt
	}
	return classifyModelType(name)
}

// applyYamlDefaultsToFlags adjusts generic defaults using values found in the
// model's shipped YAML.
func applyYamlDefaultsToFlags(yamlPath string, flags map[string]modelFlagDef) {
	cfg, ok := parseModelYaml(yamlPath)
	if !ok {
		return
	}
	if cfg.SegmentSize > 0 {
		setFlagDefault(flags, "segment_size", cfg.SegmentSize)
	}
	if cfg.NumOverlap > 0 {
		setFlagDefault(flags, "num_overlap", cfg.NumOverlap)
	}
	if cfg.BatchSize > 0 {
		setFlagDefault(flags, "batch_size", cfg.BatchSize)
	}
	if cfg.ChunkSize > 0 {
		setFlagDefault(flags, "chunk_size", cfg.ChunkSize)
	}
	if cfg.Shifts > 0 {
		setFlagDefault(flags, "shifts", cfg.Shifts)
	}
	if cfg.Segment > 0 {
		setFlagDefault(flags, "segment", cfg.Segment)
	}
	if cfg.Jobs > 0 {
		setFlagDefault(flags, "jobs", cfg.Jobs)
	}
	if cfg.Device != "" {
		setFlagDefault(flags, "device", cfg.Device)
	}
}

func setFlagDefault(flags map[string]modelFlagDef, name string, value interface{}) {
	def := flags[name]
	def.Default = value
	flags[name] = def
}

func copyFlagDef(def modelFlagDef) modelFlagDef {
	if def.Choices != nil {
		copied := make([]string, len(def.Choices))
		copy(copied, def.Choices)
		def.Choices = copied
	}
	if def.Affects != nil {
		copied := make([]string, len(def.Affects))
		copy(copied, def.Affects)
		def.Affects = copied
	}
	return def
}

// readUserFlagValues reads user-saved flag values from config/model_configs/<name>.yaml.
func readUserFlagValues(name string) map[string]interface{} {
	values := make(map[string]interface{})
	path := modelConfigYamlPath(name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return values
	}
	cfg, ok := parseModelYaml(path)
	if !ok {
		return values
	}
	return modelConfigToFlagValues(cfg)
}

// modelConfigToFlagValues converts a ModelConfigResponse into a flag value map.
func modelConfigToFlagValues(cfg ModelConfigResponse) map[string]interface{} {
	numOverlap := cfg.NumOverlap
	if numOverlap <= 0 && cfg.Overlap > 0 && cfg.Overlap < 1 {
		numOverlap = int(math.Round(1.0 / cfg.Overlap))
	}
	return map[string]interface{}{
		"segment_size": cfg.SegmentSize,
		"num_overlap":  numOverlap,
		"chunk_size":   cfg.ChunkSize,
		"batch_size":   cfg.BatchSize,
		"device":       cfg.Device,
		"shifts":       cfg.Shifts,
		"segment":      cfg.Segment,
		"jobs":         cfg.Jobs,
	}
}

// flagValuesToModelConfig converts a flag value map into a ModelConfigResponse.
func flagValuesToModelConfig(values map[string]interface{}) ModelConfigResponse {
	cfg := ModelConfigResponse{
		SegmentSize: 512,
		Overlap:     0.25,
		ChunkSize:   0,
		BatchSize:   1,
		Device:      "cuda",
		Shifts:      1,
		Segment:     0,
		Jobs:        0,
	}
	if v, ok := values["segment_size"]; ok {
		cfg.SegmentSize = toInt(v)
	}
	if v, ok := values["num_overlap"]; ok {
		cfg.NumOverlap = toInt(v)
		if cfg.NumOverlap > 0 {
			cfg.Overlap = 1.0 / float64(cfg.NumOverlap)
		}
	} else if v, ok := values["overlap"]; ok {
		cfg.Overlap = toFloat64(v)
		if cfg.Overlap > 0 && cfg.Overlap < 1 {
			cfg.NumOverlap = int(math.Round(1.0 / cfg.Overlap))
		}
	}
	if v, ok := values["chunk_size"]; ok {
		cfg.ChunkSize = toInt(v)
	}
	if v, ok := values["batch_size"]; ok {
		cfg.BatchSize = toInt(v)
	}
	if v, ok := values["device"]; ok {
		cfg.Device = toString(v)
	}
	if v, ok := values["shifts"]; ok {
		cfg.Shifts = toInt(v)
	}
	if v, ok := values["segment"]; ok {
		cfg.Segment = toFloat64(v)
	}
	if v, ok := values["jobs"]; ok {
		cfg.Jobs = toInt(v)
	}
	cfg.Segment = clampDemucsSegment(cfg.Segment)
	return cfg
}

// modelStemsFromManifest returns the stem list declared by the model manifest,
// falling back to sensible defaults for built-in or inferred models.
func modelStemsFromManifest(name string) modelManifestStems {
	if modelDir, _, found := searchModelOnDisk(name); found {
		if m, ok := loadModelManifest(modelDir); ok {
			return m.Stems
		}
	}
	if strings.EqualFold(name, "htdemucs_ft") {
		if m, ok := htdemucsFtManifest(); ok {
			return m.Stems
		}
	}
	mt := inferModelTypeFromName(name)
	return inferManifestStems(mt, name)
}

// getModelFlagsResponse builds the effective flag list for a model.
func getModelFlagsResponse(name string) (*ModelFlagsResponse, error) {
	defs, ok := loadModelManifestFlags(name)
	if !ok {
		defs = fallbackFlagsForModel(name)
	}
	user := readUserFlagValues(name)

	ordered := make([]string, 0, len(defs))
	for k := range defs {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)

	flags := make([]ModelFlagValue, 0, len(ordered))
	for _, k := range ordered {
		def := defs[k]
		val := def.Default
		if v, ok := user[k]; ok {
			val = v
		}
		flags = append(flags, ModelFlagValue{
			Name:        k,
			Value:       val,
			Default:     def.Default,
			Min:         def.Min,
			Max:         def.Max,
			Step:        def.Step,
			Editable:    def.Editable,
			Type:        def.Type,
			Choices:     def.Choices,
			Description: def.Description,
			Affects:     def.Affects,
			BetterSide:  def.BetterSide,
		})
	}

	stems := modelStemsFromManifest(name)
	return &ModelFlagsResponse{
		Model:    name,
		Flags:    flags,
		Stems:    stems.Stems,
		NumStems: stems.NumStems,
		Target:   stems.Target,
	}, nil
}

// saveModelFlags validates and persists user changes to a model's flags.
func saveModelFlags(name string, updates []ModelFlagValue) error {
	defs, ok := loadModelManifestFlags(name)
	if !ok {
		defs = fallbackFlagsForModel(name)
	}

	values := make(map[string]interface{})
	for k, v := range readUserFlagValues(name) {
		values[k] = v
	}
	for k, def := range defs {
		if _, ok := values[k]; !ok {
			values[k] = def.Default
		}
	}

	for _, u := range updates {
		def, ok := defs[u.Name]
		if !ok {
			return fmt.Errorf("unknown flag %q for model %q", u.Name, name)
		}
		if err := validateFlagValue(u.Name, name, def, u.Value); err != nil {
			return err
		}
		values[u.Name] = u.Value
	}

	cfg := flagValuesToModelConfig(values)
	if err := writeModelConfigToYaml(name, cfg); err != nil {
		return fmt.Errorf("failed to save model config YAML: %w", err)
	}
	if err := writeUVRModelConfigJSON(name, cfg); err != nil {
		log.Printf("ERROR: failed to sync UVR JSON for %s: %v", name, err)
	}
	return nil
}

// validateFlagValue checks a single flag value against its declaration.
func validateFlagValue(flagName, modelName string, def modelFlagDef, value interface{}) error {
	if def.Type == "choice" {
		s := toString(value)
		for _, c := range def.Choices {
			if s == c {
				return nil
			}
		}
		return fmt.Errorf("flag %q for model %q must be one of %v", flagName, modelName, def.Choices)
	}

	v := toFloat64(value)
	if def.Min != nil {
		min := toFloat64(def.Min)
		if v < min {
			return fmt.Errorf("flag %q for model %q must be between %v and %v", flagName, modelName, def.Min, def.Max)
		}
	}
	if def.Max != nil {
		max := toFloat64(def.Max)
		if v > max {
			return fmt.Errorf("flag %q for model %q must be between %v and %v", flagName, modelName, def.Min, def.Max)
		}
	}
	return nil
}

// migrateLegacyModelConfigs converts legacy UVR JSON files at <dataRoot>/model_configs
// into the new YAML format under config/model_configs. It is idempotent: once the
// YAML exists the JSON is left untouched, so the operation is reversible by deleting
// the generated YAML.
func migrateLegacyModelConfigs() {
	dir := legacyModelConfigsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		yamlPath := modelConfigYamlPath(name)
		if _, err := os.Stat(yamlPath); err == nil {
			continue
		}
		jsonPath := filepath.Join(dir, entry.Name())
		cfg, ok := readUVRModelConfigJSON(jsonPath)
		if !ok {
			log.Printf("[model-config] legacy JSON %s could not be parsed, skipping migration", jsonPath)
			continue
		}
		if err := writeModelConfigToYaml(name, cfg); err != nil {
			log.Printf("[model-config] failed to migrate %s: %v", jsonPath, err)
			continue
		}
		log.Printf("[model-config] migrated legacy config %s -> %s", jsonPath, yamlPath)
	}
}

// legacyModelConfigsDir returns the legacy UVR JSON directory at <dataRoot>/model_configs.
func legacyModelConfigsDir() string {
	return filepath.Join(dataRoot(), "model_configs")
}

// toFloat64 coerces a numeric JSON/YAML value to float64.
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	case json.Number:
		f, _ := n.Float64()
		return f
	case yaml.Node:
		f, _ := strconv.ParseFloat(n.Value, 64)
		return f
	}
	return 0
}

// toInt coerces a numeric JSON/YAML value to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(math.Round(n))
	case float32:
		return int(math.Round(float64(n)))
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case yaml.Node:
		i, _ := strconv.Atoi(n.Value)
		return i
	}
	return 0
}

// toString coerces a value to string.
func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", s)
	}
}
