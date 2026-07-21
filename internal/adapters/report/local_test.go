package report

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestLocalStoreWritesJSONAndMarkdownReports(t *testing.T) {
	root := t.TempDir()
	store := LocalStore{ProcessedRoot: root}
	report := sampleReport()

	saved, err := store.SaveReport(context.Background(), report, false)
	if err != nil {
		t.Fatalf("save report: %v", err)
	}

	if saved.JSONPath != filepath.Join(root, "diamond_example", "reports", "run_01", JSONReportName) {
		t.Fatalf("unexpected JSON path: %s", saved.JSONPath)
	}
	if saved.MarkdownPath != filepath.Join(root, "diamond_example", "reports", "run_01", MarkdownReportName) {
		t.Fatalf("unexpected markdown path: %s", saved.MarkdownPath)
	}

	rawJSON, err := os.ReadFile(saved.JSONPath)
	if err != nil {
		t.Fatalf("read JSON report: %v", err)
	}
	if !strings.Contains(string(rawJSON), `"run_id": "run_01"`) {
		t.Fatalf("unexpected JSON report:\n%s", string(rawJSON))
	}

	rawMarkdown, err := os.ReadFile(saved.MarkdownPath)
	if err != nil {
		t.Fatalf("read markdown report: %v", err)
	}
	if !strings.Contains(string(rawMarkdown), "# VOD Analysis Report") ||
		!strings.Contains(string(rawMarkdown), "## Gameplay Review") ||
		!strings.Contains(string(rawMarkdown), "Estimated Round Segments") ||
		!strings.Contains(string(rawMarkdown), "Model Review Tasks") ||
		!strings.Contains(string(rawMarkdown), "High-impact fight window") ||
		!strings.Contains(string(rawMarkdown), "Baseline finding") ||
		!strings.Contains(string(rawMarkdown), "Review the recommendation.") ||
		!strings.Contains(string(rawMarkdown), "Confidence: 0.80") ||
		!strings.Contains(string(rawMarkdown), "contact_sheet.jpg") {
		t.Fatalf("unexpected markdown report:\n%s", string(rawMarkdown))
	}
}

func TestLocalStoreRejectsExistingReportWithoutOverwrite(t *testing.T) {
	store := LocalStore{ProcessedRoot: t.TempDir()}
	report := sampleReport()

	if _, err := store.SaveReport(context.Background(), report, false); err != nil {
		t.Fatalf("save initial report: %v", err)
	}
	if _, err := store.SaveReport(context.Background(), report, false); err == nil {
		t.Fatalf("expected existing report error")
	}
}

func sampleReport() domain.AnalysisReport {
	return domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion,
		RunID:         "run_01",
		Status:        "completed",
		GeneratedAt:   time.Date(2026, 7, 21, 12, 30, 0, 0, time.UTC),
		VOD: domain.VOD{
			Label:     "diamond_example",
			VideoID:   "abc123",
			Rank:      "diamond",
			SourceURL: "https://www.youtube.com/watch?v=abc123",
			Title:     "Diamond VOD",
			Channel:   "Channel",
		},
		Media: domain.MediaSummary{
			DurationSeconds: 120,
			HasDuration:     true,
			VideoCodec:      "h264",
			Width:           1920,
			Height:          1080,
			FrameRate:       "60 fps",
			AudioCodec:      "aac",
			HasAudio:        true,
			SizeBytes:       1024,
			HasSize:         true,
		},
		Sample: domain.FrameSampleSummary{
			Name:             "analysis_run_01",
			ManifestPath:     "frames.json",
			FPS:              "0.5",
			DurationSeconds:  120,
			FrameCount:       60,
			ContactSheetPath: "frames/contact_sheet.jpg",
		},
		Gameplay: &domain.GameplaySummary{
			Analyzer:             "visual-heuristic-gameplay",
			SampledFrames:        60,
			AnalyzedFrames:       60,
			ReviewWindowCount:    1,
			AverageMotionScore:   0.42,
			AverageMinimapSignal: 0.32,
			AverageHUDSignal:     0.21,
			PeakCombatScore:      0.66,
			RoundSegmentCount:    1,
			ModelReviewTaskCount: 1,
			RoundSegments: []domain.RoundSegment{
				{
					RoundNumber:     1,
					StartSeconds:    0,
					EndSeconds:      120,
					DurationSeconds: 120,
					DetectionMethod: "estimated_from_visual_timeline",
					Confidence:      0.62,
					ReviewWindowIDs: []string{"combatspike_001"},
					Summary:         "Estimated round segment.",
				},
			},
			ModelReviewTasks: []domain.ModelReviewTask{
				{
					ID:             "vlm_combatspike_001",
					Status:         "ready",
					Priority:       "medium",
					PromptVersion:  "vlm-review-v1",
					WindowID:       "combatspike_001",
					RoundNumber:    1,
					ClipPath:       "clips/review_001.mp4",
					Prompt:         "Review this clip.",
					ExpectedOutput: `{"findings":[]}`,
				},
			},
			ReviewWindows: []domain.ReviewWindow{
				{
					ID:             "combatspike_001",
					Kind:           "combat_spike",
					Severity:       domain.FindingSeverityMedium,
					Title:          "High-impact fight window",
					Summary:        "Visual intensity peaked.",
					Recommendation: "Review the duel setup.",
					StartSeconds:   8,
					EndSeconds:     24,
					PeakSeconds:    16,
					RoundNumber:    1,
					Score:          0.66,
					Evidence: []domain.EvidenceRef{
						{ArtifactType: "frame", Path: "frames/frame_000016.jpg", TimestampSeconds: 16, FrameIndex: 16},
					},
				},
			},
		},
		Findings: []domain.Finding{
			{
				ID:             "baseline",
				Severity:       domain.FindingSeverityInfo,
				Category:       "test",
				Title:          "Baseline finding",
				Detail:         "A deterministic test finding.",
				Recommendation: "Review the recommendation.",
				Confidence:     0.8,
			},
		},
	}
}
