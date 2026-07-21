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
