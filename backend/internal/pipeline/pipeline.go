package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/starmito/onda/internal/audio"
	"github.com/starmito/onda/internal/cli"
)

const statusFilePath = "/tmp/onda_pipeline_status.json"

// Status represents the current state of the pipeline.
type Status struct {
	Status   string  `json:"status"`            // running, done, error
	Progress float64 `json:"progress"`
	Step     string  `json:"step"`              // starting, vocals, stems, dedicated, pitch, done
	Song     string  `json:"song"`
	Elapsed  int     `json:"elapsed"`           // seconds
	ETA      int     `json:"eta"`               // seconds
	Error    string  `json:"error,omitempty"`
}

// Pipeline orchestrates the audio separation process via docker exec commands.
type Pipeline struct {
	flags        *cli.PipelineFlags
	song         string // basename of input without extension
	outputDir    string // host output directory
	dockerInput  string // path to input file inside container
	dockerOutput string // output directory path inside container
	copyFromDir  string // if outputDir is outside project, copy files from here after pipeline
	start        time.Time
}

// modelScripts maps vocal model names to their Python inference scripts inside the container.
var modelScripts = map[string]string{
	"viperx":           "inference_universal.py",
	"polarformer":      "inference_universal.py",
	"melband_kj":       "inference_universal.py",
	"melband_karaoke":  "inference_universal.py",
}

// modelDirs maps vocal model names to their checkpoint directory inside the container.
var modelDirs = map[string]string{
	"viperx":          "BS_Roformer_Viperx",
	"polarformer":     "BS_PolarFormer",
	"melband_kj":      "MelBand_Roformer_KJ",
	"melband_karaoke": "MelBand_Karaoke",
}

// singleStemCheckpoints maps dedicated stem model names to their checkpoint filenames
// inside the single_stem/ subdirectory.
var singleStemCheckpoints = map[string]string{
	"htdemucs_drums": "f7e0c4bc-ba3fe64a.th",
	"htdemucs_bass":  "d12395a8-e57c48e6.th",
}

// dockerContainer is the name of the Docker container running the Python environment.
const dockerContainer = "onda"

// StatusFile returns the path to the pipeline status JSON file.
func StatusFile() string {
	return statusFilePath
}

// findProjectRoot walks up from the current directory until it finds go.mod,
// then returns the parent directory (the project root where output/ lives).
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "VERSION")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// New creates a new Pipeline from parsed CLI flags.
func New(flags *cli.PipelineFlags) *Pipeline {
	song := strings.TrimSuffix(filepath.Base(flags.Input), filepath.Ext(flags.Input))
	outputDir := flags.Output
	if outputDir == "" {
		outputDir = filepath.Join(".", "output", song)
	}

	// Translate host output path to container output path.
	// Docker bind mount: <project_root>/output/ -> /output/
	projectRoot := findProjectRoot()
	hostPrefix := filepath.Join(projectRoot, "output") + "/"
	var dockerOutput string
	var copyFromDir string
	if projectRoot != "" && strings.HasPrefix(outputDir, hostPrefix) {
		dockerOutput = filepath.Join("/output", strings.TrimPrefix(outputDir, hostPrefix))
	} else {
		dockerOutput = filepath.Join("/output", song)
		// If the user-specified output directory is outside the project,
		// the pipeline still writes inside the container to /output/<song>
		// (which maps to <projectRoot>/output/<song> on the host).
		// After the pipeline finishes, we copy everything to the actual outputDir.
		if projectRoot != "" && !strings.HasPrefix(outputDir, hostPrefix) {
			copyFromDir = filepath.Join(projectRoot, "output", song)
		}
	}

	return &Pipeline{
		flags:        flags,
		song:         song,
		outputDir:    outputDir,
		dockerInput:  filepath.Join("/input", filepath.Base(flags.Input)),
		dockerOutput: dockerOutput,
		copyFromDir:  copyFromDir,
		start:        time.Now(),
	}
}

// Run executes the complete pipeline. It is blocking — runs until completion or error.
func Run(flags *cli.PipelineFlags) error {
	p := New(flags)
	return p.Run()
}

// Run executes the pipeline steps sequentially.
func (p *Pipeline) Run() error {
	p.writeStatus("running", 0, "starting")

	// Ensure output directory exists on the host
	if err := os.MkdirAll(p.outputDir, 0755); err != nil {
		err = fmt.Errorf("creating output directory: %w", err)
		p.writeError(err)
		return err
	}

	// ---- Step 1: Vocal separation ----
	if err := p.runVocalSeparation(); err != nil {
		p.writeError(err)
		return err
	}
	p.writeStatus("running", 0.40, "vocals")

	// ---- Step 2: Stem separation ----
	if !p.skipStemSeparation() {
		if err := p.runStemSeparation(); err != nil {
			p.writeError(err)
			return err
		}
	}
	p.writeStatus("running", 0.70, "stems")

	// ---- Step 3: Dedicated stems (master/ultimate presets) ----
	if !p.skipDedicatedStems() {
		if err := p.runDedicatedStems(); err != nil {
			p.writeError(err)
			return err
		}
	}
	p.writeStatus("running", 0.90, "dedicated")

	// ---- Step 4: Pitch shift ----
	if p.flags.Pitch != 0 {
		if err := p.runPitchShift(); err != nil {
			p.writeError(err)
			return err
		}
	}
	p.writeStatus("running", 1.0, "pitch")

	// ---- Done ----
	p.writeStatus("done", 1.0, "done")

	// If the user requested an output directory outside the project root,
	// copy the generated files from the internal output to the user's directory.
	if p.copyFromDir != "" {
		if err := copyDir(p.copyFromDir, p.outputDir); err != nil {
			err = fmt.Errorf("copying output to %s: %w", p.outputDir, err)
			p.writeError(err)
			return err
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Step implementations
// ---------------------------------------------------------------------------

// runVocalSeparation executes the vocal/instrumental separation step.
// It runs the appropriate inference script inside the container.
func (p *Pipeline) runVocalSeparation() error {
	script, ok := modelScripts[p.flags.VocalModel]
	if !ok {
		// Fallback: use inference_roformer.py as default
		script = "inference_roformer.py"
	}

	// Model directory inside docker container
	modelDir := filepath.Join("/app/models/VR_Models", modelDirs[p.flags.VocalModel])

	args := []string{"exec", dockerContainer, "python3", script, modelDir, p.dockerInput, p.dockerOutput}
	if p.flags.VocalOverlap > 0 {
		args = append(args, fmt.Sprintf("%d", p.flags.VocalOverlap))
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("vocal separation failed (model=%s): %w\nOutput: %s",
			p.flags.VocalModel, err, string(output))
	}

	// Handle vocal-keep: remove files we don't need
	if p.flags.VocalKeep != "both" {
		removeFile := p.song + "_instrumental.wav"
		if p.flags.VocalKeep == "instrumental" {
			removeFile = p.song + "_vocals.wav"
		}
		// Remove on host side
		hostPath := filepath.Join(p.outputDir, removeFile)
		os.Remove(hostPath)
		// Also try removing inside container (best-effort)
		exec.Command("docker", "exec", dockerContainer,
			"rm", "-f", filepath.Join(p.dockerOutput, removeFile)).Run()
	}

	return nil
}

// skipStemSeparation returns true if Demucs stem separation should be skipped.
func (p *Pipeline) skipStemSeparation() bool {
	// If no stem model is set, skip Demucs (ultimate preset skips this step
	// in favor of dedicated passes)
	return p.flags.StemModel == ""
}

// runStemSeparation runs Demucs on the instrumental track to separate stems.
func (p *Pipeline) runStemSeparation() error {
	instrumentalPath := filepath.Join(p.dockerOutput, p.song+"_instrumental.wav")

	args := []string{
		"exec", dockerContainer, "demucs",
		"--two-stems=vocals",
		"-n", p.flags.StemModel,
		"-o", p.dockerOutput,
		instrumentalPath,
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("demucs stem separation failed: %w\nOutput: %s", err, string(output))
	}

	// Demucs outputs to <model>/<trackname>/ with two stems: vocals.wav and no_vocals.wav
	demucsSubDir := filepath.Join(p.dockerOutput, p.flags.StemModel, p.song+"_instrumental")
	stems := []string{"vocals.wav", "no_vocals.wav"}
	for _, stem := range stems {
		src := filepath.Join(demucsSubDir, stem)
		dst := filepath.Join(p.dockerOutput, stem)
		// Copy inside container using cp
		exec.Command("docker", "exec", dockerContainer,
			"cp", src, dst).Run()
	}

	// Clean up the Demucs subdirectory
	exec.Command("docker", "exec", dockerContainer,
		"rm", "-rf", demucsSubDir).Run()

	// Apply --stem-keep filter on host side
	if len(p.flags.StemKeep) > 0 {
		p.filterStems()
	}

	return nil
}

// runDedicatedStems runs dedicated single-stem inference passes
// for models that have been explicitly set (drums, bass, other).
func (p *Pipeline) runDedicatedStems() error {
	instrumentalPath := filepath.Join(p.dockerOutput, p.song+"_instrumental.wav")

	// Dedicated drums pass
	if p.flags.DrumsModel != "" {
		if err := p.runSingleStem(instrumentalPath, "drums.wav",
			filepath.Join("/app/models/Demucs_Models/single_stem", singleStemCheckpoints[p.flags.DrumsModel]),
			"inference_demucs_single.py"); err != nil {
			return fmt.Errorf("dedicated drums separation: %w", err)
		}
	}

	// Dedicated bass pass
	if p.flags.BassModel != "" {
		if err := p.runSingleStem(instrumentalPath, "bass.wav",
			filepath.Join("/app/models/Demucs_Models/single_stem", singleStemCheckpoints[p.flags.BassModel]),
			"inference_demucs_single.py"); err != nil {
			return fmt.Errorf("dedicated bass separation: %w", err)
		}
	}

	// Dedicated other pass (uses universal for other-stem models)
	if p.flags.OtherModel != "" {
		otherModelPath := filepath.Join("/app/models/VR_Models", modelDirs[p.flags.OtherModel])
		otherOutput := filepath.Join(p.dockerOutput, "other.wav")
		args := []string{"exec", dockerContainer, "python3", "inference_universal.py", otherModelPath, instrumentalPath, p.dockerOutput}
		if p.flags.VocalOverlap > 0 {
			args = append(args, fmt.Sprintf("%d", p.flags.VocalOverlap))
		}
		cmd := exec.Command("docker", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("dedicated other stem failed: %w\nOutput: %s", err, string(output))
		}
		// Rename output to other.wav if roformer produced a different name
		// roformer outputs vocals.wav + instrumental.wav; we rename instrumental to other
		exec.Command("docker", "exec", dockerContainer,
			"mv", filepath.Join(p.dockerOutput, p.song+"_instrumental.wav"), otherOutput).Run()
		// Remove the vocals.wav from the roformer run (it was re-separating from instrumental)
		exec.Command("docker", "exec", dockerContainer,
			"rm", "-f", filepath.Join(p.dockerOutput, p.song+"_vocals.wav")).Run()
	}

	// Apply --stem-keep filter
	if len(p.flags.StemKeep) > 0 {
		p.filterStems()
	}

	return nil
}

// runSingleStem executes a single-stem inference pass using the specified script.
func (p *Pipeline) runSingleStem(inputFile, outputFile, checkpoint, script string) error {
	outputPath := filepath.Join(p.dockerOutput, outputFile)
	args := []string{
		"exec", dockerContainer, "python3", script,
		"--checkpoint", checkpoint,
		"--input", inputFile,
		"--output", outputPath,
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed (script=%s, checkpoint=%s): %w\nOutput: %s",
			script, checkpoint, err, string(output))
	}
	return nil
}

// runPitchShift applies rubberband pitch shift to selected stems (drums excluded).
func (p *Pipeline) runPitchShift() error {
	// Determine which stems exist and should be pitch-shifted
	stems := p.collectPitchStems()

	if len(stems) == 0 {
		return nil
	}

	for _, stem := range stems {
		hostPath := filepath.Join(p.outputDir, stem)
		if _, err := os.Stat(hostPath); os.IsNotExist(err) {
			continue
		}

		// Create a temp file for the pitched version
		tmpPath := hostPath + ".pitched.tmp"
		if err := audio.RubberbandPitch(p.flags.Pitch, hostPath, tmpPath); err != nil {
			return fmt.Errorf("pitch shift failed for %s: %w", stem, err)
		}

		// Replace original with pitched version
		if err := os.Rename(tmpPath, hostPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("replacing pitched file %s: %w", stem, err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// filterStems removes stem files based on --stem-keep flag.
// If "all" is in StemKeep, all stems are kept.
func (p *Pipeline) filterStems() {
	keepMap := make(map[string]bool)
	for _, s := range p.flags.StemKeep {
		if s == "all" {
			return // keep everything
		}
		keepMap[s] = true
	}

	allStems := []string{"drums.wav", "bass.wav", "other.wav"}
	for _, stem := range allStems {
		if !keepMap[strings.TrimSuffix(stem, ".wav")] {
			hostPath := filepath.Join(p.outputDir, stem)
			os.Remove(hostPath)
			// Also clean up inside container
			exec.Command("docker", "exec", dockerContainer,
				"rm", "-f", filepath.Join(p.dockerOutput, stem)).Run()
		}
	}
}

// collectPitchStems returns the list of stem filenames that should be pitch-shifted.
// Drums are excluded from pitch shift.
func (p *Pipeline) collectPitchStems() []string {
	var stems []string

	// Always include vocals if vocal-keep says so
	if p.flags.VocalKeep == "both" || p.flags.VocalKeep == "vocals" {
		stems = append(stems, p.song+"_vocals.wav")
	}
	if p.flags.VocalKeep == "both" || p.flags.VocalKeep == "instrumental" {
		// Only pitch instrumental if no stem separation happened
		if p.skipStemSeparation() && p.skipDedicatedStems() {
			stems = append(stems, p.song+"_instrumental.wav")
		}
	}

	// Add bass and other from stem separation (if they exist)
	// Drums are excluded per spec
	if !p.skipStemSeparation() || !p.skipDedicatedStems() {
		// Only add stems that the user actually wants to keep
		if len(p.flags.StemKeep) == 0 || p.hasStem("bass") {
			stems = append(stems, "bass.wav")
		}
		if len(p.flags.StemKeep) == 0 || p.hasStem("other") {
			stems = append(stems, "other.wav")
		}
	}

	return stems
}

// hasStem checks if the stem is in the StemKeep list (or StemKeep is empty/all).
func (p *Pipeline) hasStem(stem string) bool {
	if len(p.flags.StemKeep) == 0 {
		return true
	}
	for _, s := range p.flags.StemKeep {
		if s == stem || s == "all" {
			return true
		}
	}
	return false
}

// skipDedicatedStems returns true if no dedicated stem models are configured.
func (p *Pipeline) skipDedicatedStems() bool {
	return p.flags.DrumsModel == "" && p.flags.BassModel == "" && p.flags.OtherModel == ""
}

// dockerExec runs a command inside the onda container and returns the combined output.
func (p *Pipeline) dockerExec(args ...string) (string, error) {
	cmdArgs := append([]string{"exec", dockerContainer}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker exec failed: %w\nOutput: %s", err, string(output))
	}
	return string(output), nil
}

// copyDir recursively copies all files and subdirectories from src to dst.
// If dst already exists, files are overwritten.
func copyDir(src, dst string) error {
	// Ensure destination exists
	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading source directory %s: %w", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copying %s -> %s: %w", src, dst, err)
	}

	// Preserve permissions
	info, err := srcFile.Stat()
	if err == nil {
		os.Chmod(dst, info.Mode())
	}

	return nil
}

// writeStatus writes the current pipeline status to the JSON status file.
func (p *Pipeline) writeStatus(status string, progress float64, step string) {
	elapsed := int(time.Since(p.start).Seconds())
	eta := 0
	if progress > 0 && progress < 1.0 {
		estimated := float64(elapsed) / progress
		eta = int(estimated - float64(elapsed))
		if eta < 0 {
			eta = 0
		}
	}

	s := Status{
		Status:   status,
		Progress: progress,
		Step:     step,
		Song:     p.song,
		Elapsed:  elapsed,
		ETA:      eta,
	}

	data, _ := json.Marshal(s)
	os.WriteFile(statusFilePath, data, 0644)
}

// writeError writes an error status to the JSON status file.
func (p *Pipeline) writeError(err error) {
	elapsed := int(time.Since(p.start).Seconds())
	s := Status{
		Status:  "error",
		Step:    "error",
		Song:    p.song,
		Elapsed: elapsed,
		Error:   err.Error(),
	}
	data, _ := json.Marshal(s)
	os.WriteFile(statusFilePath, data, 0644)
}
