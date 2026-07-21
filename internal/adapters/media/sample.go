package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const FramesManifestName = "frames.json"

var unsafeArtifactName = regexp.MustCompile(`[^a-zA-Z0-9_.=-]+`)

type SampleOptions struct {
	FFmpegPath   string
	InputPath    string
	OutputDir    string
	FPS          string
	Start        time.Duration
	Duration     time.Duration
	ImageQuality int
	Overwrite    bool
}

type SampleResult struct {
	InputPath       string
	OutputDir       string
	FPS             string
	FPSValue        float64
	Start           time.Duration
	Duration        time.Duration
	ImageQuality    int
	FrameCount      int
	Frames          []FrameArtifact
	FramesManifest  string
	FrameNamePrefix string
}

type FrameArtifact struct {
	Index            int     `json:"index"`
	TimestampSeconds float64 `json:"timestamp_seconds"`
	Path             string  `json:"path"`
}

type FramesManifest struct {
	SchemaVersion   int             `json:"schema_version"`
	VODLabel        string          `json:"vod_label"`
	VideoID         string          `json:"video_id"`
	Rank            string          `json:"rank"`
	SourcePath      string          `json:"source_path"`
	SampleName      string          `json:"sample_name"`
	FPS             string          `json:"fps"`
	FPSValue        float64         `json:"fps_value"`
	StartSeconds    float64         `json:"start_seconds"`
	DurationSeconds float64         `json:"duration_seconds,omitempty"`
	ImageQuality    int             `json:"image_quality"`
	FrameCount      int             `json:"frame_count"`
	Frames          []FrameArtifact `json:"frames"`
	GeneratedAt     time.Time       `json:"generated_at"`
}

func RunSample(ctx context.Context, options SampleOptions) (SampleResult, error) {
	if options.FFmpegPath == "" {
		options.FFmpegPath = "ffmpeg"
	}
	if options.ImageQuality <= 0 {
		options.ImageQuality = 3
	}

	fpsValue, err := ParseFPS(options.FPS)
	if err != nil {
		return SampleResult{}, err
	}
	if options.InputPath == "" {
		return SampleResult{}, fmt.Errorf("input path is required")
	}
	if options.OutputDir == "" {
		return SampleResult{}, fmt.Errorf("output dir is required")
	}

	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return SampleResult{}, err
	}

	manifestPath := filepath.Join(options.OutputDir, FramesManifestName)
	if !options.Overwrite {
		if _, err := os.Stat(manifestPath); err == nil {
			return SampleResult{}, fmt.Errorf("sample already exists: %s", manifestPath)
		}
	}

	if options.Overwrite {
		if err := removeExistingFrames(options.OutputDir); err != nil {
			return SampleResult{}, err
		}
	}

	framePattern := filepath.Join(options.OutputDir, "frame_%06d.jpg")
	args := buildFFmpegSampleArgs(options, framePattern)
	if err := runFFmpeg(ctx, options.FFmpegPath, args); err != nil {
		return SampleResult{}, err
	}

	frames, err := collectFrames(options.OutputDir, options.Start, fpsValue)
	if err != nil {
		return SampleResult{}, err
	}

	return SampleResult{
		InputPath:       options.InputPath,
		OutputDir:       options.OutputDir,
		FPS:             options.FPS,
		FPSValue:        fpsValue,
		Start:           options.Start,
		Duration:        options.Duration,
		ImageQuality:    options.ImageQuality,
		FrameCount:      len(frames),
		Frames:          frames,
		FramesManifest:  manifestPath,
		FrameNamePrefix: "frame_",
	}, nil
}

func WriteFramesManifest(vod VODInfo, sampleName string, result SampleResult) (string, error) {
	manifest := FramesManifest{
		SchemaVersion: 1,
		VODLabel:      vod.Label,
		VideoID:       vod.VideoID,
		Rank:          vod.Rank,
		SourcePath:    result.InputPath,
		SampleName:    sampleName,
		FPS:           result.FPS,
		FPSValue:      result.FPSValue,
		StartSeconds:  result.Start.Seconds(),
		ImageQuality:  result.ImageQuality,
		FrameCount:    result.FrameCount,
		Frames:        result.Frames,
		GeneratedAt:   time.Now().UTC(),
	}

	if result.Duration > 0 {
		manifest.DurationSeconds = result.Duration.Seconds()
	}

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')

	path := result.FramesManifest
	if path == "" {
		return "", fmt.Errorf("frames manifest path is required")
	}

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}

	return path, nil
}

func SampleOutputDir(processedRoot string, vodLabel string, sampleName string) string {
	return filepath.Join(processedRoot, vodLabel, "frames", SafeArtifactName(sampleName))
}

func DefaultSampleName(fps string, start, duration time.Duration) string {
	parts := []string{"sample", SafeArtifactName(fps) + "fps"}
	if start > 0 {
		parts = append(parts, "from_"+compactDuration(start))
	}
	if duration > 0 {
		parts = append(parts, compactDuration(duration))
	} else {
		parts = append(parts, "full")
	}
	return strings.Join(parts, "_")
}

func SafeArtifactName(value string) string {
	value = strings.TrimSpace(value)
	value = unsafeArtifactName.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "sample"
	}
	return value
}

func ParseFPS(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("fps is required")
	}

	fps, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid fps %q: %w", value, err)
	}
	if fps <= 0 {
		return 0, fmt.Errorf("fps must be positive")
	}
	return fps, nil
}

func buildFFmpegSampleArgs(options SampleOptions, framePattern string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if options.Overwrite {
		args = append(args, "-y")
	} else {
		args = append(args, "-n")
	}
	if options.Start > 0 {
		args = append(args, "-ss", formatFFmpegDuration(options.Start))
	}
	args = append(args, "-i", options.InputPath)
	if options.Duration > 0 {
		args = append(args, "-t", formatFFmpegDuration(options.Duration))
	}
	args = append(args, "-vf", "fps="+options.FPS, "-q:v", strconv.Itoa(options.ImageQuality), framePattern)
	return args
}

func runFFmpeg(ctx context.Context, ffmpegPath string, args []string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("ffmpeg: %s", message)
	}
	return nil
}

func collectFrames(outputDir string, start time.Duration, fps float64) ([]FrameArtifact, error) {
	paths, err := filepath.Glob(filepath.Join(outputDir, "frame_*.jpg"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	frames := make([]FrameArtifact, 0, len(paths))
	for index, path := range paths {
		timestamp := start.Seconds() + float64(index)/fps
		frames = append(frames, FrameArtifact{
			Index:            index + 1,
			TimestampSeconds: timestamp,
			Path:             filepath.ToSlash(path),
		})
	}
	return frames, nil
}

func removeExistingFrames(outputDir string) error {
	paths, err := filepath.Glob(filepath.Join(outputDir, "frame_*.jpg"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func compactDuration(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(duration/time.Second))
	}
	return fmt.Sprintf("%dms", int64(duration/time.Millisecond))
}

func formatFFmpegDuration(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', 3, 64)
}
