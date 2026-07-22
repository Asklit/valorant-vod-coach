package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestBuildAnalysisEvents(t *testing.T) {
	report := domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion,
		RunID:         "run_01",
		Status:        "completed",
		GeneratedAt:   time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		VOD: domain.VOD{
			Label: "iron_example",
			Rank:  "iron",
		},
		Media: domain.MediaSummary{
			HasDuration:     true,
			DurationSeconds: 180,
			Width:           1920,
			Height:          1080,
			HasAudio:        true,
		},
		Sample: domain.FrameSampleSummary{
			Name:             "analysis_run_01",
			FPS:              "1",
			FPSValue:         1,
			FrameCount:       180,
			ContactSheetPath: "data/processed/iron_example/frames/analysis_run_01/contact_sheet.jpg",
		},
		Gameplay: &domain.GameplaySummary{
			ReviewWindowCount: 2,
			RoundSegmentCount: 1,
		},
		Findings: []domain.Finding{{ID: "finding_01"}},
		Artifacts: []domain.Artifact{
			{Type: "probe", Format: "json", Path: "probe.ffprobe.json"},
			{Type: "frame_manifest", Format: "json", Path: "frames.json"},
		},
		Metadata: domain.AnalysisRunMetadata{
			Analyzer: "visual-heuristic-gameplay",
			Mode:     "local",
		},
	}

	nextID := 0
	events, err := BuildAnalysisEvents(PersistAnalysisRequest{
		Report: report,
		Saved: SavedReport{
			JSONPath:     "data/processed/iron_example/reports/run_01/report.json",
			MarkdownPath: "data/processed/iron_example/reports/run_01/report.md",
		},
	}, report.GeneratedAt, "test-producer", func() string {
		nextID++
		return "event_0" + string(rune('0'+nextID))
	})
	if err != nil {
		t.Fatalf("build analysis events: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	expected := []struct {
		eventType string
		topic     string
	}{
		{domain.EventTypeVODProbed, domain.TopicVODProcessing},
		{domain.EventTypeFramesExtracted, domain.TopicVODProcessing},
		{domain.EventTypeReportReady, domain.TopicVODLifecycle},
	}
	for index, expectedEvent := range expected {
		event := events[index]
		if event.EventType != expectedEvent.eventType || event.Topic != expectedEvent.topic {
			t.Fatalf("unexpected event[%d]: %+v", index, event)
		}
		if event.AggregateID != "iron_example" || event.CorrelationID != "run_01" || event.Producer != "test-producer" {
			t.Fatalf("unexpected envelope metadata: %+v", event)
		}
		if !json.Valid(event.Payload) {
			t.Fatalf("payload is not valid JSON: %s", event.Payload)
		}
	}
}
