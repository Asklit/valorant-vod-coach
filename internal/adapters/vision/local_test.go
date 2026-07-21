package vision

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestLocalGameplayAnalyzerBuildsReviewWindows(t *testing.T) {
	root := t.TempDir()
	frames := []domain.Frame{
		writeTestFrame(t, root, 1, 0, color.RGBA{R: 35, G: 42, B: 48, A: 255}, false),
		writeTestFrame(t, root, 2, 1, color.RGBA{R: 40, G: 48, B: 58, A: 255}, true),
		writeTestFrame(t, root, 3, 2, color.RGBA{R: 36, G: 44, B: 52, A: 255}, false),
		writeTestFrame(t, root, 4, 12, color.RGBA{R: 36, G: 44, B: 52, A: 255}, false),
		writeTestFrame(t, root, 5, 22, color.RGBA{R: 36, G: 44, B: 52, A: 255}, false),
	}

	analyzer := LocalGameplayAnalyzer{}
	result, err := analyzer.AnalyzeObservations(context.Background(), app.ObservationRequest{
		RunID:       "vision_test",
		GeneratedAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		VOD:         domain.VOD{Label: "vision_vod", Rank: "diamond"},
		Media:       domain.MediaSummary{DurationSeconds: 30, HasDuration: true, Width: 1920, Height: 1080, HasAudio: true},
		Sample: domain.FrameSampleSummary{
			Name:            "analysis_vision_test",
			OutputDir:       root,
			ManifestPath:    filepath.Join(root, "frames.json"),
			FPS:             "1",
			FPSValue:        1,
			DurationSeconds: 30,
			FrameCount:      len(frames),
			Frames:          frames,
		},
	})
	if err != nil {
		t.Fatalf("analyze observations: %v", err)
	}

	if result.Metadata.Analyzer != "visual-heuristic-gameplay" {
		t.Fatalf("unexpected analyzer metadata: %+v", result.Metadata)
	}
	if result.Gameplay == nil {
		t.Fatalf("expected gameplay summary")
	}
	if result.Gameplay.AnalyzedFrames != len(frames) {
		t.Fatalf("unexpected analyzed frame count: %+v", result.Gameplay)
	}
	if result.Gameplay.ReviewWindowCount == 0 {
		t.Fatalf("expected review windows: %+v", result.Gameplay)
	}
	if result.Gameplay.Coach == nil || len(result.Gameplay.Coach.FocusAreas) == 0 || len(result.Gameplay.Coach.PracticePlan) == 0 {
		t.Fatalf("expected coach summary with focus areas and practice plan: %+v", result.Gameplay.Coach)
	}
	if len(result.Gameplay.PhaseProfile) == 0 {
		t.Fatalf("expected phase profile: %+v", result.Gameplay)
	}
	if hasFinding(result.Findings, "baseline_ai_not_enabled") {
		t.Fatalf("baseline AI placeholder finding should be removed")
	}
	if !hasFinding(result.Findings, "gameplay_coach_priorities_ready") {
		t.Fatalf("expected coach priorities finding: %+v", result.Findings)
	}
	if !hasFinding(result.Findings, "gameplay_review_ready") {
		t.Fatalf("expected gameplay ready finding: %+v", result.Findings)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Type != "gameplay_review" {
		t.Fatalf("expected gameplay artifact: %+v", result.Artifacts)
	}
	if _, err := os.Stat(result.Artifacts[0].Path); err != nil {
		t.Fatalf("expected gameplay artifact file: %v", err)
	}
}

func TestLocalGameplayAnalyzerHandlesUnreadableFrames(t *testing.T) {
	root := t.TempDir()
	badFrame := filepath.Join(root, "frame_000001.jpg")
	if err := os.WriteFile(badFrame, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write bad frame: %v", err)
	}

	result, err := LocalGameplayAnalyzer{}.AnalyzeObservations(context.Background(), app.ObservationRequest{
		VOD: domain.VOD{Label: "bad_vod"},
		Sample: domain.FrameSampleSummary{
			OutputDir:  root,
			FrameCount: 1,
			Frames: []domain.Frame{
				{Index: 1, TimestampSeconds: 0, Path: badFrame},
			},
		},
	})
	if err != nil {
		t.Fatalf("analyze observations: %v", err)
	}
	if result.Gameplay == nil || result.Gameplay.AnalyzedFrames != 0 || result.Gameplay.SkippedFrames != 1 {
		t.Fatalf("unexpected gameplay summary: %+v", result.Gameplay)
	}
	if !hasFinding(result.Findings, "gameplay_frames_unreadable") {
		t.Fatalf("expected unreadable frames finding: %+v", result.Findings)
	}
}

func writeTestFrame(t *testing.T, root string, index int, seconds float64, background color.RGBA, combat bool) domain.Frame {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)

	draw.Draw(img, image.Rect(5, 5, 76, 54), &image.Uniform{C: color.RGBA{R: 60, G: 95, B: 92, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(120, 4, 200, 18), &image.Uniform{C: color.RGBA{R: 170, G: 175, B: 180, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(100, 152, 220, 178), &image.Uniform{C: color.RGBA{R: 125, G: 142, B: 154, A: 255}}, image.Point{}, draw.Src)

	if combat {
		draw.Draw(img, image.Rect(120, 62, 205, 126), &image.Uniform{C: color.RGBA{R: 220, G: 38, B: 48, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(152, 80, 168, 104), &image.Uniform{C: color.RGBA{R: 250, G: 230, B: 210, A: 255}}, image.Point{}, draw.Src)
	}

	path := filepath.Join(root, "frame_"+zeroPad(index)+".jpg")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create frame: %v", err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode frame: %v", err)
	}

	return domain.Frame{Index: index, TimestampSeconds: seconds, Path: path}
}

func zeroPad(value int) string {
	return fmt.Sprintf("%06d", value)
}

func hasFinding(findings []domain.Finding, id string) bool {
	for _, finding := range findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}
