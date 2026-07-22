package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type EventIDGenerator func() string

func BuildAnalysisEvents(request PersistAnalysisRequest, occurredAt time.Time, producer string, newID EventIDGenerator) ([]domain.EventEnvelope, error) {
	if newID == nil {
		return nil, fmt.Errorf("event id generator is required")
	}
	if producer == "" {
		producer = "vodcoach"
	}

	report := request.Report
	base := func(eventType string, topic string, payload any) (domain.EventEnvelope, error) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return domain.EventEnvelope{}, err
		}
		return domain.EventEnvelope{
			EventID:       newID(),
			Topic:         topic,
			EventType:     eventType,
			EventVersion:  domain.EventEnvelopeVersion,
			OccurredAt:    occurredAt.UTC(),
			Producer:      producer,
			AggregateType: "vod",
			AggregateID:   report.VOD.Label,
			CorrelationID: report.RunID,
			Payload:       raw,
		}, nil
	}

	events := make([]domain.EventEnvelope, 0, 3)
	for _, spec := range []struct {
		eventType string
		topic     string
		payload   any
	}{
		{
			eventType: domain.EventTypeVODProbed,
			topic:     domain.TopicVODProcessing,
			payload: map[string]any{
				"vod_label":                 report.VOD.Label,
				"run_id":                    report.RunID,
				"duration_seconds":          report.Media.DurationSeconds,
				"has_duration":              report.Media.HasDuration,
				"size_bytes":                report.Media.SizeBytes,
				"has_size":                  report.Media.HasSize,
				"video_codec":               report.Media.VideoCodec,
				"width":                     report.Media.Width,
				"height":                    report.Media.Height,
				"frame_rate":                report.Media.FrameRate,
				"audio_codec":               report.Media.AudioCodec,
				"has_audio":                 report.Media.HasAudio,
				"manifest_duration_seconds": report.VOD.ManifestDurationSeconds,
			},
		},
		{
			eventType: domain.EventTypeFramesExtracted,
			topic:     domain.TopicVODProcessing,
			payload: map[string]any{
				"vod_label":          report.VOD.Label,
				"run_id":             report.RunID,
				"sample_name":        report.Sample.Name,
				"fps":                report.Sample.FPS,
				"fps_value":          report.Sample.FPSValue,
				"start_seconds":      report.Sample.StartSeconds,
				"duration_seconds":   report.Sample.DurationSeconds,
				"frame_count":        report.Sample.FrameCount,
				"manifest_path":      report.Sample.ManifestPath,
				"contact_sheet_path": report.Sample.ContactSheetPath,
			},
		},
		{
			eventType: domain.EventTypeReportReady,
			topic:     domain.TopicVODLifecycle,
			payload: map[string]any{
				"vod_label":              report.VOD.Label,
				"run_id":                 report.RunID,
				"status":                 report.Status,
				"schema_version":         report.SchemaVersion,
				"generated_at":           report.GeneratedAt.UTC(),
				"report_json_path":       request.Saved.JSONPath,
				"report_markdown_path":   request.Saved.MarkdownPath,
				"finding_count":          len(report.Findings),
				"artifact_count":         len(report.Artifacts),
				"review_window_count":    eventReviewWindowCount(report.Gameplay),
				"round_segment_count":    eventRoundSegmentCount(report.Gameplay),
				"model_review_run_count": eventModelReviewRunCount(report.Gameplay),
				"analyzer":               report.Metadata.Analyzer,
				"mode":                   report.Metadata.Mode,
			},
		},
	} {
		event, err := base(spec.eventType, spec.topic, spec.payload)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

func eventReviewWindowCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	return gameplay.ReviewWindowCount
}

func eventRoundSegmentCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.RoundSegmentCount == 0 && len(gameplay.RoundSegments) > 0 {
		return len(gameplay.RoundSegments)
	}
	return gameplay.RoundSegmentCount
}

func eventModelReviewRunCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.ModelReviewRunCount == 0 && len(gameplay.ModelReviewRuns) > 0 {
		return len(gameplay.ModelReviewRuns)
	}
	return gameplay.ModelReviewRunCount
}
