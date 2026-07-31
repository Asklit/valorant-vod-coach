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

	if result.Metadata.Analyzer != GameplayAnalyzerName {
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
	if result.Gameplay.Coach != nil {
		t.Fatalf("vision adapter must not produce coaching conclusions: %+v", result.Gameplay.Coach)
	}
	if len(result.Gameplay.PhaseProfile) == 0 {
		t.Fatalf("expected phase profile: %+v", result.Gameplay)
	}
	if len(result.Gameplay.RoundSegments) == 0 || result.Gameplay.RoundSegmentCount == 0 {
		t.Fatalf("expected estimated round segments: %+v", result.Gameplay)
	}
	if result.Gameplay.ReviewWindows[0].RoundNumber == 0 {
		t.Fatalf("expected review window round number: %+v", result.Gameplay.ReviewWindows[0])
	}
	if len(result.Gameplay.GameplayEvents) == 0 {
		t.Fatalf("expected gameplay events: %+v", result.Gameplay)
	}
	if !hasGameplayEventType(result.Gameplay.GameplayEvents, "combat_candidate") {
		t.Fatalf("expected combat candidate gameplay event: %+v", result.Gameplay.GameplayEvents)
	}
	combatWindow := windowsByKindForTest(result.Gameplay.ReviewWindows, "combat_spike")
	if len(combatWindow) != 1 || len(combatWindow[0].Evidence) != 3 {
		t.Fatalf("expected one corroborated fight with evidence sequence: %+v", combatWindow)
	}
	if combatWindow[0].Evidence[0].Role != "before" || combatWindow[0].Evidence[1].Role != "event" || combatWindow[0].Evidence[2].Role != "after" {
		t.Fatalf("unexpected evidence roles: %+v", combatWindow[0].Evidence)
	}
	if hasFinding(result.Findings, "baseline_ai_not_enabled") {
		t.Fatalf("baseline AI placeholder finding should be removed")
	}
	if !hasFinding(result.Findings, "gameplay_review_ready") {
		t.Fatalf("expected gameplay ready finding: %+v", result.Findings)
	}
	if !hasFinding(result.Findings, "gameplay_round_segments_detected") {
		t.Fatalf("expected round segment finding: %+v", result.Findings)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Type != "gameplay_review" {
		t.Fatalf("expected gameplay artifact: %+v", result.Artifacts)
	}
	if _, err := os.Stat(result.Artifacts[0].Path); err != nil {
		t.Fatalf("expected gameplay artifact file: %v", err)
	}
}

func TestHighImpactWindowsRejectMotionWithoutGameplayCorroboration(t *testing.T) {
	observations := []domain.FrameObservation{
		{Index: 1, TimestampSeconds: 10, HUDLayoutConfidence: 0.7, MotionScore: 1, CenterActivity: 1, CombatSignal: 0.95},
		{Index: 2, TimestampSeconds: 20, HUDLayoutConfidence: 0.7, MotionScore: 0.9, CenterActivity: 0.9, CombatSignal: 0.91},
	}
	if windows := buildHighImpactWindows(observations, 4, nil); len(windows) != 0 {
		t.Fatalf("motion-only observations must not become fight windows: %+v", windows)
	}

	observations[0].KillfeedEventSignal = 0.8
	if windows := buildHighImpactWindows(observations, 4, nil); len(windows) != 1 {
		t.Fatalf("corroborated observation should become a fight window: %+v", windows)
	}
}

func TestHighImpactWindowsKeepFightBeforeSeparateDeathReview(t *testing.T) {
	observations := []domain.FrameObservation{
		{Index: 1, TimestampSeconds: 137, Path: "fight.jpg", HUDLayoutConfidence: 0.6, MotionScore: 0.6, CenterActivity: 0.7, CombatSignal: 0.9, KillfeedEventSignal: 1},
	}
	existing := []domain.ReviewWindow{{Kind: "death_review", StartSeconds: 150, EndSeconds: 168, PeakSeconds: 160}}

	windows := buildHighImpactWindows(observations, 2, existing)
	if len(windows) != 1 || windows[0].PeakSeconds != 137 {
		t.Fatalf("a distinct fight must not be swallowed by a later death review: %+v", windows)
	}
}

func TestRotationWindowsRejectRoundEndOverlay(t *testing.T) {
	observations := []domain.FrameObservation{
		{Index: 1, TimestampSeconds: 70, HUDLayoutConfidence: 0.6, MotionScore: 0.1},
		{Index: 2, TimestampSeconds: 76, HUDLayoutConfidence: 0.6, MotionScore: 1, RoundEndSignal: 0.90},
		{Index: 3, TimestampSeconds: 160, HUDLayoutConfidence: 0.6, MotionScore: 0.1, CombatReportSignal: 0.98},
		{Index: 4, TimestampSeconds: 162, HUDLayoutConfidence: 0.6, MotionScore: 1},
	}
	if windows := buildRotationWindows(observations, 2, nil); len(windows) != 0 {
		t.Fatalf("round-end animation must not become a rotation window: %+v", windows)
	}
}

func TestDeathReviewWindowsClusterPersistentCombatReport(t *testing.T) {
	observations := []domain.FrameObservation{
		{Index: 1, TimestampSeconds: 5, Path: "before-1.jpg", HUDLayoutConfidence: 0.6},
		{Index: 2, TimestampSeconds: 10, Path: "death-1.jpg", HUDLayoutConfidence: 0.6, CombatReportSignal: 0.98},
		{Index: 3, TimestampSeconds: 12, Path: "report-still-visible.jpg", HUDLayoutConfidence: 0.6, CombatReportSignal: 0.97},
		{Index: 4, TimestampSeconds: 18, Path: "after-1.jpg", HUDLayoutConfidence: 0.6},
		{Index: 5, TimestampSeconds: 40, Path: "before-2.jpg", HUDLayoutConfidence: 0.6},
		{Index: 6, TimestampSeconds: 46, Path: "death-2.jpg", HUDLayoutConfidence: 0.6, CombatReportSignal: 0.99},
		{Index: 7, TimestampSeconds: 50, Path: "after-2.jpg", HUDLayoutConfidence: 0.6},
	}

	windows := buildDeathReviewWindows(observations, 4)
	if len(windows) != 2 {
		t.Fatalf("expected two death events rather than one per persistent frame: %+v", windows)
	}
	for _, window := range windows {
		if window.Kind != "death_review" || len(window.Evidence) != 3 {
			t.Fatalf("unexpected death review window: %+v", window)
		}
	}
}

func TestDeathReviewWindowsRejectPostRoundCombatReport(t *testing.T) {
	observations := []domain.FrameObservation{
		{Index: 1, TimestampSeconds: 10, HUDLayoutConfidence: 0.6, CombatReportSignal: 0.98, BuyPhaseSignal: 0.90},
		{Index: 2, TimestampSeconds: 20, HUDLayoutConfidence: 0.6, CombatReportSignal: 0.98, ScoreboardSignal: 0.80},
		{Index: 3, TimestampSeconds: 30, HUDLayoutConfidence: 0.6, CombatReportSignal: 0.98, RoundEndSignal: 0.90},
	}
	if windows := buildDeathReviewWindows(observations, 4); len(windows) != 0 {
		t.Fatalf("post-round combat report must not confirm a death: %+v", windows)
	}
}

func TestRoundSegmentsPreferBuyPhaseAnchors(t *testing.T) {
	observations := []domain.FrameObservation{
		{Index: 1, TimestampSeconds: 0, HUDLayoutConfidence: 0.5},
		{Index: 2, TimestampSeconds: 4, HUDLayoutConfidence: 0.5, BuyPhaseSignal: 0.96},
		{Index: 3, TimestampSeconds: 5, HUDLayoutConfidence: 0.5, BuyPhaseSignal: 0.96},
		{Index: 4, TimestampSeconds: 100, HUDLayoutConfidence: 0.5, BuyPhaseSignal: 0.96},
		{Index: 5, TimestampSeconds: 101, HUDLayoutConfidence: 0.5, BuyPhaseSignal: 0.96},
		{Index: 6, TimestampSeconds: 200, HUDLayoutConfidence: 0.5, BuyPhaseSignal: 0.96},
		{Index: 7, TimestampSeconds: 201, HUDLayoutConfidence: 0.5, BuyPhaseSignal: 0.96},
		{Index: 8, TimestampSeconds: 260, HUDLayoutConfidence: 0.5},
	}

	segments := buildRoundSegments(observations, nil, app.ObservationRequest{Sample: domain.FrameSampleSummary{FPSValue: 1}})
	if len(segments) != 3 {
		t.Fatalf("expected three anchored round segments: %+v", segments)
	}
	for _, segment := range segments {
		if segment.DetectionMethod != "buy_phase_visual_anchor" {
			t.Fatalf("expected buy-phase detection method: %+v", segment)
		}
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

	teal := color.RGBA{R: 45, G: 165, B: 150, A: 255}
	red := color.RGBA{R: 205, G: 62, B: 72, A: 255}
	white := color.RGBA{R: 238, G: 240, B: 235, A: 255}
	dark := color.RGBA{R: 28, G: 33, B: 38, A: 255}

	// A compact synthetic VALORANT-style layout: minimap, both team bars,
	// timer glyphs, and three bottom HUD groups.
	draw.Draw(img, image.Rect(4, 4, 78, 62), &image.Uniform{C: dark}, image.Point{}, draw.Src)
	for x := 8; x < 74; x += 10 {
		draw.Draw(img, image.Rect(x, 8, x+2, 58), &image.Uniform{C: white}, image.Point{}, draw.Src)
	}
	for y := 10; y < 58; y += 9 {
		draw.Draw(img, image.Rect(8, y, 74, y+2), &image.Uniform{C: white}, image.Point{}, draw.Src)
	}
	draw.Draw(img, image.Rect(64, 2, 143, 18), &image.Uniform{C: teal}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(177, 2, 256, 18), &image.Uniform{C: red}, image.Point{}, draw.Src)
	for x := 148; x < 173; x += 6 {
		draw.Draw(img, image.Rect(x, 4, x+3, 17), &image.Uniform{C: white}, image.Point{}, draw.Src)
	}
	for _, rect := range []image.Rectangle{image.Rect(84, 160, 116, 177), image.Rect(120, 160, 200, 177), image.Rect(204, 160, 236, 177)} {
		draw.Draw(img, rect, &image.Uniform{C: dark}, image.Point{}, draw.Src)
		for x := rect.Min.X + 3; x < rect.Max.X-2; x += 8 {
			draw.Draw(img, image.Rect(x, rect.Min.Y+3, x+3, rect.Max.Y-3), &image.Uniform{C: white}, image.Point{}, draw.Src)
		}
	}

	if combat {
		draw.Draw(img, image.Rect(120, 62, 205, 126), &image.Uniform{C: color.RGBA{R: 220, G: 38, B: 48, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(152, 80, 168, 104), &image.Uniform{C: color.RGBA{R: 250, G: 230, B: 210, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(232, 12, 317, 23), &image.Uniform{C: red}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(274, 12, 317, 23), &image.Uniform{C: teal}, image.Point{}, draw.Src)
		for x := 238; x < 312; x += 9 {
			draw.Draw(img, image.Rect(x, 15, x+4, 20), &image.Uniform{C: white}, image.Point{}, draw.Src)
		}
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

func hasGameplayEventType(events []domain.GameplayEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func windowsByKindForTest(windows []domain.ReviewWindow, kind string) []domain.ReviewWindow {
	result := make([]domain.ReviewWindow, 0)
	for _, window := range windows {
		if window.Kind == kind {
			result = append(result, window)
		}
	}
	return result
}
