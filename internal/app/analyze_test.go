package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestAnalysisRunnerCreatesBaselineReport(t *testing.T) {
	clock := func() time.Time {
		return time.Date(2026, 7, 21, 12, 30, 0, 0, time.UTC)
	}
	reports := &fakeReportStore{}

	runner := AnalysisRunner{
		Resolver: fakeResolver{},
		Media:    fakeMediaProcessor{},
		Reports:  reports,
		Clock:    clock,
	}

	result, err := runner.Run(context.Background(), RunAnalysisRequest{
		VODLabel:     "diamond_example",
		FPS:          "0.5",
		Duration:     3 * time.Minute,
		ImageQuality: 3,
	})
	if err != nil {
		t.Fatalf("run analysis: %v", err)
	}

	if result.Report.RunID != "20260721T123000Z" {
		t.Fatalf("unexpected run ID: %s", result.Report.RunID)
	}
	if result.Report.VOD.Label != "diamond_example" {
		t.Fatalf("unexpected VOD label: %s", result.Report.VOD.Label)
	}
	if result.Report.Sample.Name != "analysis_20260721T123000Z" {
		t.Fatalf("unexpected sample name: %s", result.Report.Sample.Name)
	}
	if result.Report.Status != "completed" {
		t.Fatalf("unexpected status: %s", result.Report.Status)
	}
	if result.Report.Metadata.Analyzer != "heuristic-baseline" {
		t.Fatalf("unexpected analyzer: %s", result.Report.Metadata.Analyzer)
	}
	if len(result.Report.Artifacts) != 3 {
		t.Fatalf("expected 3 input artifacts, got %d", len(result.Report.Artifacts))
	}
	if result.Saved.JSONPath == "" || result.Saved.MarkdownPath == "" {
		t.Fatalf("expected saved report paths: %+v", result.Saved)
	}
	if reports.last.RunID != result.Report.RunID {
		t.Fatalf("report store received wrong report: %+v", reports.last)
	}

	if !hasFinding(result.Report.Findings, "baseline_ai_not_enabled") {
		t.Fatalf("expected AI-not-enabled finding: %+v", result.Report.Findings)
	}
	if !hasFinding(result.Report.Findings, "baseline_partial_sample") {
		t.Fatalf("expected partial-sample finding: %+v", result.Report.Findings)
	}
}

func TestAnalysisRunnerRequiresPorts(t *testing.T) {
	_, err := (AnalysisRunner{}).Run(context.Background(), RunAnalysisRequest{VODLabel: "x"})
	if err == nil || !strings.Contains(err.Error(), "VOD resolver") {
		t.Fatalf("expected missing resolver error, got %v", err)
	}
}

func TestAnalysisRunnerAttachesReviewClips(t *testing.T) {
	runner := AnalysisRunner{
		Resolver: fakeResolver{},
		Media:    fakeClipMediaProcessor{},
		Analyzer: fakeGameplayAnalyzer{},
		Reports:  &fakeReportStore{},
		Clock: func() time.Time {
			return time.Date(2026, 7, 21, 12, 45, 0, 0, time.UTC)
		},
	}

	result, err := runner.Run(context.Background(), RunAnalysisRequest{
		VODLabel:     "diamond_example",
		RunID:        "clip_contract",
		FPS:          "1",
		Duration:     30 * time.Second,
		ImageQuality: 3,
		Overwrite:    true,
	})
	if err != nil {
		t.Fatalf("run analysis: %v", err)
	}

	if result.Report.Gameplay == nil || len(result.Report.Gameplay.ReviewWindows) != 1 {
		t.Fatalf("expected gameplay review window, got %+v", result.Report.Gameplay)
	}
	window := result.Report.Gameplay.ReviewWindows[0]
	if window.ClipPath != "/tmp/clips/review_01.mp4" {
		t.Fatalf("expected clip path on review window, got %q", window.ClipPath)
	}
	if window.ClipDurationSeconds != 12 {
		t.Fatalf("expected clip duration, got %.3f", window.ClipDurationSeconds)
	}
	if result.Report.Gameplay.ModelReviewTaskCount != 1 || len(result.Report.Gameplay.ModelReviewTasks) != 1 {
		t.Fatalf("expected model review task: %+v", result.Report.Gameplay)
	}
	task := result.Report.Gameplay.ModelReviewTasks[0]
	if task.Status != "ready" || task.ClipPath != window.ClipPath {
		t.Fatalf("unexpected model review task: %+v", task)
	}
	if !strings.Contains(task.Prompt, window.ClipPath) || !strings.Contains(task.ExpectedOutput, `"findings"`) {
		t.Fatalf("unexpected model review prompt: %+v", task)
	}
	if !hasArtifact(result.Report.Artifacts, "review_clip") {
		t.Fatalf("expected review clip artifact: %+v", result.Report.Artifacts)
	}
}

func TestBaselineAnalyzerEndsFullSampleAtLastFrame(t *testing.T) {
	result, err := BaselineObservationAnalyzer{}.AnalyzeObservations(context.Background(), ObservationRequest{
		Media: domain.MediaSummary{DurationSeconds: 120, HasDuration: true},
		Sample: domain.FrameSampleSummary{
			StartSeconds: 0,
			FrameCount:   3,
			Frames: []domain.Frame{
				{Index: 1, TimestampSeconds: 0, Path: "/tmp/frame_000001.jpg"},
				{Index: 2, TimestampSeconds: 1, Path: "/tmp/frame_000002.jpg"},
				{Index: 3, TimestampSeconds: 119, Path: "/tmp/frame_000003.jpg"},
			},
		},
	})
	if err != nil {
		t.Fatalf("analyze observations: %v", err)
	}

	if got := timelineTimestamp(result.Timeline, "sample_finished"); got != 119 {
		t.Fatalf("expected full sample to finish at last frame timestamp, got %.3f", got)
	}
}

type fakeResolver struct{}

func (fakeResolver) ResolveVOD(context.Context, string) (domain.VOD, string, error) {
	return domain.VOD{
		Label:                   "diamond_example",
		VideoID:                 "abc123",
		Rank:                    "diamond",
		SourceURL:               "https://www.youtube.com/watch?v=abc123",
		Title:                   "Diamond VOD",
		Channel:                 "Channel",
		ManifestDurationSeconds: 37 * 60,
	}, "/tmp/diamond_example.mp4", nil
}

type fakeMediaProcessor struct{}

func (fakeMediaProcessor) ProbeMedia(context.Context, domain.VOD, string) (MediaProbeResult, error) {
	return MediaProbeResult{
		Summary: domain.MediaSummary{
			DurationSeconds: 37 * 60,
			HasDuration:     true,
			SizeBytes:       100,
			HasSize:         true,
			VideoCodec:      "h264",
			Width:           1920,
			Height:          1080,
			FrameRate:       "60 fps",
			AudioCodec:      "aac",
			HasAudio:        true,
		},
		Artifact: domain.Artifact{Type: "media_probe", Format: "ffprobe_json", Path: "/tmp/probe.json"},
	}, nil
}

func (fakeMediaProcessor) SampleFrames(_ context.Context, _ domain.VOD, _ string, request FrameSampleRequest) (FrameSampleResult, error) {
	return FrameSampleResult{
		Summary: domain.FrameSampleSummary{
			Name:             request.Name,
			OutputDir:        "/tmp/frames",
			ManifestPath:     "/tmp/frames/frames.json",
			FPS:              request.FPS,
			FPSValue:         0.5,
			DurationSeconds:  request.Duration.Seconds(),
			FrameCount:       90,
			ContactSheetPath: "/tmp/frames/contact_sheet.jpg",
			Frames: []domain.Frame{
				{Index: 1, TimestampSeconds: 0, Path: "/tmp/frames/frame_000001.jpg"},
			},
		},
		Artifact:             domain.Artifact{Type: "frame_sample", Format: "frames_manifest_json", Path: "/tmp/frames/frames.json"},
		ContactSheetArtifact: domain.Artifact{Type: "contact_sheet", Format: "jpeg", Path: "/tmp/frames/contact_sheet.jpg"},
	}, nil
}

type fakeClipMediaProcessor struct {
	fakeMediaProcessor
}

func (fakeClipMediaProcessor) ExtractReviewClips(_ context.Context, _ domain.VOD, _ string, request ReviewClipRequest) (ReviewClipResult, error) {
	windows := append([]domain.ReviewWindow{}, request.Windows...)
	for index := range windows {
		windows[index].ClipPath = "/tmp/clips/review_01.mp4"
		windows[index].ClipDurationSeconds = 12
	}
	return ReviewClipResult{
		Windows: windows,
		Artifacts: []domain.Artifact{
			{Type: "review_clip", Format: "mp4", Path: "/tmp/clips/review_01.mp4"},
		},
	}, nil
}

type fakeGameplayAnalyzer struct{}

func (fakeGameplayAnalyzer) AnalyzeObservations(context.Context, ObservationRequest) (ObservationResult, error) {
	return ObservationResult{
		Gameplay: &domain.GameplaySummary{
			Analyzer:          "fake-gameplay",
			SampledFrames:     1,
			AnalyzedFrames:    1,
			ReviewWindowCount: 1,
			ReviewWindows: []domain.ReviewWindow{
				{
					ID:             "review_01",
					Kind:           "combat_spike",
					Severity:       domain.FindingSeverityMedium,
					Title:          "Review this duel",
					Summary:        "A concentrated combat moment needs manual review.",
					Recommendation: "Pause before the swing and inspect crosshair placement.",
					StartSeconds:   10,
					EndSeconds:     22,
					PeakSeconds:    16,
					Score:          0.82,
				},
			},
		},
		Metadata: domain.AnalysisRunMetadata{
			Analyzer: "fake-gameplay",
			Mode:     "local-test",
		},
	}, nil
}

type fakeReportStore struct {
	last domain.AnalysisReport
}

func (s *fakeReportStore) SaveReport(_ context.Context, report domain.AnalysisReport, _ bool) (SavedReport, error) {
	s.last = report
	return SavedReport{
		JSONPath:     "/tmp/report.json",
		MarkdownPath: "/tmp/report.md",
	}, nil
}

func hasFinding(findings []domain.Finding, id string) bool {
	for _, finding := range findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func hasArtifact(artifacts []domain.Artifact, artifactType string) bool {
	for _, artifact := range artifacts {
		if artifact.Type == artifactType {
			return true
		}
	}
	return false
}

func timelineTimestamp(events []domain.TimelineEvent, eventType string) float64 {
	for _, event := range events {
		if event.Type == eventType {
			return event.TimestampSeconds
		}
	}
	return -1
}
