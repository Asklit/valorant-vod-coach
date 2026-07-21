package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const ReviewClipsManifestName = "review_clips.json"

type ReviewClipsManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	VODLabel      string               `json:"vod_label"`
	VideoID       string               `json:"video_id"`
	Rank          string               `json:"rank"`
	RunID         string               `json:"run_id"`
	SampleName    string               `json:"sample_name,omitempty"`
	SourcePath    string               `json:"source_path"`
	ClipCount     int                  `json:"clip_count"`
	Clips         []ReviewClipManifest `json:"clips"`
	GeneratedAt   time.Time            `json:"generated_at"`
}

type ReviewClipManifest struct {
	WindowID        string  `json:"window_id"`
	Kind            string  `json:"kind"`
	Severity        string  `json:"severity"`
	StartSeconds    float64 `json:"start_seconds"`
	EndSeconds      float64 `json:"end_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
	Path            string  `json:"path"`
}

type ReviewClipOptions struct {
	FFmpegPath string
	InputPath  string
	OutputDir  string
	RunID      string
	SampleName string
	Windows    []domain.ReviewWindow
	Overwrite  bool
}

type ReviewClipExtractionResult struct {
	Windows      []domain.ReviewWindow
	Artifacts    []domain.Artifact
	ManifestPath string
}

func (p LocalProcessor) ExtractReviewClips(ctx context.Context, vod domain.VOD, videoPath string, request app.ReviewClipRequest) (app.ReviewClipResult, error) {
	ffmpegPath := p.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	runCtx := ctx
	cancel := func() {}
	if p.SampleTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, p.SampleTimeout)
	}
	defer cancel()

	runName := SafeArtifactName(request.RunID)
	if runName == "sample" {
		runName = SafeArtifactName(request.SampleName)
	}

	result, err := RunReviewClipExtraction(runCtx, ReviewClipOptions{
		FFmpegPath: ffmpegPath,
		InputPath:  videoPath,
		OutputDir:  ReviewClipsOutputDir(p.ProcessedRoot, vod.Label, runName),
		RunID:      request.RunID,
		SampleName: request.SampleName,
		Windows:    request.Windows,
		Overwrite:  request.Overwrite,
	})
	if err != nil {
		return app.ReviewClipResult{}, err
	}

	manifestPath, err := WriteReviewClipsManifest(mediaVODInfo(vod), videoPath, result, request.RunID, request.SampleName)
	if err != nil {
		return app.ReviewClipResult{}, err
	}

	artifacts := append([]domain.Artifact{}, result.Artifacts...)
	artifacts = append(artifacts, domain.Artifact{
		Type:   "review_clip_manifest",
		Format: "json",
		Path:   manifestPath,
	})

	return app.ReviewClipResult{
		Windows:   result.Windows,
		Artifacts: artifacts,
	}, nil
}

func RunReviewClipExtraction(ctx context.Context, options ReviewClipOptions) (ReviewClipExtractionResult, error) {
	if options.FFmpegPath == "" {
		options.FFmpegPath = "ffmpeg"
	}
	if options.InputPath == "" {
		return ReviewClipExtractionResult{}, fmt.Errorf("input path is required")
	}
	if options.OutputDir == "" {
		return ReviewClipExtractionResult{}, fmt.Errorf("output dir is required")
	}
	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return ReviewClipExtractionResult{}, err
	}

	windows := make([]domain.ReviewWindow, 0, len(options.Windows))
	artifacts := make([]domain.Artifact, 0, len(options.Windows))
	for index, window := range options.Windows {
		duration := window.EndSeconds - window.StartSeconds
		if duration <= 0 {
			duration = 1
		}

		clipName := fmt.Sprintf("review_%02d_%s.mp4", index+1, SafeArtifactName(window.ID))
		outputPath := filepath.Join(options.OutputDir, clipName)
		if !options.Overwrite {
			if _, err := os.Stat(outputPath); err == nil {
				return ReviewClipExtractionResult{}, fmt.Errorf("review clip already exists: %s", outputPath)
			}
		}

		if err := extractReviewClip(ctx, options.FFmpegPath, options.InputPath, outputPath, window.StartSeconds, duration, options.Overwrite); err != nil {
			return ReviewClipExtractionResult{}, fmt.Errorf("extract review clip %s: %w", window.ID, err)
		}

		window.ClipPath = filepath.ToSlash(outputPath)
		window.ClipDurationSeconds = roundSeconds(duration)
		windows = append(windows, window)
		artifacts = append(artifacts, domain.Artifact{
			Type:   "review_clip",
			Format: "mp4",
			Path:   window.ClipPath,
		})
	}

	return ReviewClipExtractionResult{
		Windows:      windows,
		Artifacts:    artifacts,
		ManifestPath: filepath.ToSlash(filepath.Join(options.OutputDir, ReviewClipsManifestName)),
	}, nil
}

func WriteReviewClipsManifest(vod VODInfo, sourcePath string, result ReviewClipExtractionResult, runID, sampleName string) (string, error) {
	clips := make([]ReviewClipManifest, 0, len(result.Windows))
	for _, window := range result.Windows {
		clips = append(clips, ReviewClipManifest{
			WindowID:        window.ID,
			Kind:            window.Kind,
			Severity:        string(window.Severity),
			StartSeconds:    roundSeconds(window.StartSeconds),
			EndSeconds:      roundSeconds(window.EndSeconds),
			DurationSeconds: roundSeconds(window.ClipDurationSeconds),
			Path:            window.ClipPath,
		})
	}

	manifest := ReviewClipsManifest{
		SchemaVersion: 1,
		VODLabel:      vod.Label,
		VideoID:       vod.VideoID,
		Rank:          vod.Rank,
		RunID:         runID,
		SampleName:    sampleName,
		SourcePath:    sourcePath,
		ClipCount:     len(clips),
		Clips:         clips,
		GeneratedAt:   time.Now().UTC(),
	}

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')

	if result.ManifestPath == "" {
		return "", fmt.Errorf("review clips manifest path is required")
	}
	if err := os.WriteFile(result.ManifestPath, raw, 0o644); err != nil {
		return "", err
	}
	return result.ManifestPath, nil
}

func ReviewClipsOutputDir(processedRoot, vodLabel, runID string) string {
	return filepath.Join(processedRoot, vodLabel, "clips", SafeArtifactName(runID))
}

func extractReviewClip(ctx context.Context, ffmpegPath, inputPath, outputPath string, startSeconds, durationSeconds float64, overwrite bool) error {
	tempPath := outputPath + ".tmp.mp4"
	if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if overwrite {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	copyArgs := buildReviewClipCopyArgs(inputPath, tempPath, startSeconds, durationSeconds)
	if err := runFFmpeg(ctx, ffmpegPath, copyArgs); err == nil {
		return replaceClip(tempPath, outputPath)
	}

	if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	transcodeArgs := buildReviewClipTranscodeArgs(inputPath, tempPath, startSeconds, durationSeconds)
	if err := runFFmpeg(ctx, ffmpegPath, transcodeArgs); err != nil {
		return err
	}
	return replaceClip(tempPath, outputPath)
}

func buildReviewClipCopyArgs(inputPath, outputPath string, startSeconds, durationSeconds float64) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if startSeconds > 0 {
		args = append(args, "-ss", formatSecondsForFFmpeg(startSeconds))
	}
	args = append(args,
		"-i", inputPath,
		"-t", formatSecondsForFFmpeg(durationSeconds),
		"-map", "0:v:0",
		"-map", "0:a?",
		"-c", "copy",
		"-movflags", "+faststart",
		outputPath,
	)
	return args
}

func buildReviewClipTranscodeArgs(inputPath, outputPath string, startSeconds, durationSeconds float64) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if startSeconds > 0 {
		args = append(args, "-ss", formatSecondsForFFmpeg(startSeconds))
	}
	args = append(args,
		"-i", inputPath,
		"-t", formatSecondsForFFmpeg(durationSeconds),
		"-map", "0:v:0",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "28",
		"-c:a", "aac",
		"-b:a", "96k",
		"-movflags", "+faststart",
		outputPath,
	)
	return args
}

func replaceClip(tempPath, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.Rename(tempPath, outputPath)
}

func formatSecondsForFFmpeg(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	return strconv.FormatFloat(seconds, 'f', 3, 64)
}

func roundSeconds(seconds float64) float64 {
	if seconds == 0 {
		return 0
	}
	return mathRound(seconds*1000) / 1000
}

func mathRound(value float64) float64 {
	if value < 0 {
		return float64(int64(value - 0.5))
	}
	return float64(int64(value + 0.5))
}
