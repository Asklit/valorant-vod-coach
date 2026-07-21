package app

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type VODResolver interface {
	ResolveVOD(ctx context.Context, label string) (vod domain.VOD, videoPath string, err error)
}

type MediaProcessor interface {
	ProbeMedia(ctx context.Context, vod domain.VOD, videoPath string) (MediaProbeResult, error)
	SampleFrames(ctx context.Context, vod domain.VOD, videoPath string, request FrameSampleRequest) (FrameSampleResult, error)
}

type ObservationAnalyzer interface {
	AnalyzeObservations(ctx context.Context, request ObservationRequest) (ObservationResult, error)
}

type ReportStore interface {
	SaveReport(ctx context.Context, report domain.AnalysisReport, overwrite bool) (SavedReport, error)
}

type AnalysisRunner struct {
	Resolver VODResolver
	Media    MediaProcessor
	Analyzer ObservationAnalyzer
	Reports  ReportStore
	Clock    func() time.Time
}

type RunAnalysisRequest struct {
	VODLabel     string
	RunID        string
	SampleName   string
	FPS          string
	Start        time.Duration
	Duration     time.Duration
	ImageQuality int
	Overwrite    bool
}

type RunAnalysisResult struct {
	Report domain.AnalysisReport
	Saved  SavedReport
}

type FrameSampleRequest struct {
	Name         string
	FPS          string
	Start        time.Duration
	Duration     time.Duration
	ImageQuality int
	Overwrite    bool
}

type MediaProbeResult struct {
	Summary  domain.MediaSummary
	Artifact domain.Artifact
}

type FrameSampleResult struct {
	Summary              domain.FrameSampleSummary
	Artifact             domain.Artifact
	ContactSheetArtifact domain.Artifact
}

type ObservationRequest struct {
	RunID       string
	GeneratedAt time.Time
	VOD         domain.VOD
	Media       domain.MediaSummary
	Sample      domain.FrameSampleSummary
}

type ObservationResult struct {
	Findings []domain.Finding
	Timeline []domain.TimelineEvent
}

type SavedReport struct {
	JSONPath     string
	MarkdownPath string
}

func (r AnalysisRunner) Run(ctx context.Context, request RunAnalysisRequest) (RunAnalysisResult, error) {
	if r.Resolver == nil {
		return RunAnalysisResult{}, fmt.Errorf("VOD resolver is required")
	}
	if r.Media == nil {
		return RunAnalysisResult{}, fmt.Errorf("media processor is required")
	}
	if r.Reports == nil {
		return RunAnalysisResult{}, fmt.Errorf("report store is required")
	}

	vodLabel := strings.TrimSpace(request.VODLabel)
	if vodLabel == "" {
		return RunAnalysisResult{}, fmt.Errorf("VOD label is required")
	}

	now := time.Now().UTC()
	if r.Clock != nil {
		now = r.Clock().UTC()
	}

	runID := strings.TrimSpace(request.RunID)
	if runID == "" {
		runID = DefaultRunID(now)
	}

	sampleName := strings.TrimSpace(request.SampleName)
	if sampleName == "" {
		sampleName = "analysis_" + runID
	}

	vod, videoPath, err := r.Resolver.ResolveVOD(ctx, vodLabel)
	if err != nil {
		return RunAnalysisResult{}, err
	}

	probe, err := r.Media.ProbeMedia(ctx, vod, videoPath)
	if err != nil {
		return RunAnalysisResult{}, err
	}

	sample, err := r.Media.SampleFrames(ctx, vod, videoPath, FrameSampleRequest{
		Name:         sampleName,
		FPS:          request.FPS,
		Start:        request.Start,
		Duration:     request.Duration,
		ImageQuality: request.ImageQuality,
		Overwrite:    request.Overwrite,
	})
	if err != nil {
		return RunAnalysisResult{}, err
	}

	analyzer := r.Analyzer
	if analyzer == nil {
		analyzer = BaselineObservationAnalyzer{}
	}

	observations, err := analyzer.AnalyzeObservations(ctx, ObservationRequest{
		RunID:       runID,
		GeneratedAt: now,
		VOD:         vod,
		Media:       probe.Summary,
		Sample:      sample.Summary,
	})
	if err != nil {
		return RunAnalysisResult{}, err
	}

	report := domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion,
		RunID:         runID,
		Status:        "completed",
		GeneratedAt:   now,
		VOD:           vod,
		Media:         probe.Summary,
		Sample:        sample.Summary,
		Findings:      observations.Findings,
		Timeline:      observations.Timeline,
		Artifacts: []domain.Artifact{
			probe.Artifact,
			sample.Artifact,
		},
		Metadata: domain.AnalysisRunMetadata{
			Analyzer: "baseline",
			Mode:     "local",
		},
	}
	if sample.ContactSheetArtifact.Path != "" {
		report.Artifacts = append(report.Artifacts, sample.ContactSheetArtifact)
	}

	saved, err := r.Reports.SaveReport(ctx, report, request.Overwrite)
	if err != nil {
		return RunAnalysisResult{}, err
	}

	return RunAnalysisResult{Report: report, Saved: saved}, nil
}

func DefaultRunID(now time.Time) string {
	return now.UTC().Format("20060102T150405Z")
}

type BaselineObservationAnalyzer struct{}

func (BaselineObservationAnalyzer) AnalyzeObservations(ctx context.Context, request ObservationRequest) (ObservationResult, error) {
	if err := ctx.Err(); err != nil {
		return ObservationResult{}, err
	}

	findings := []domain.Finding{
		{
			ID:       "baseline_ingestion_ready",
			Severity: domain.FindingSeverityInfo,
			Category: "ingestion",
			Title:    "VOD artifacts generated",
			Detail:   "The MVP pipeline successfully loaded the VOD, probed media metadata, sampled frames, and produced a reproducible report artifact.",
			Tags:     []string{"mvp", "local-pipeline"},
		},
		{
			ID:       "baseline_ai_not_enabled",
			Severity: domain.FindingSeverityInfo,
			Category: "analysis_gap",
			Title:    "Vision model analysis is not enabled in this run",
			Detail:   "This report is a deterministic baseline. Gameplay findings from minimap, HUD, OCR, economy, utility, and positioning analysis will be added through the vision-service adapter.",
			Tags:     []string{"baseline", "vision-service"},
		},
	}

	if request.Sample.FrameCount == 0 {
		findings = append(findings, domain.Finding{
			ID:       "baseline_no_frames",
			Severity: domain.FindingSeverityHigh,
			Category: "media_quality",
			Title:    "No frames were extracted",
			Detail:   "The frame sample is empty, so downstream visual analysis cannot run on this artifact.",
		})
	} else {
		findings[0].Evidence = append(findings[0].Evidence, domain.EvidenceRef{
			ArtifactType:     "frame_sample",
			Path:             request.Sample.ManifestPath,
			TimestampSeconds: request.Sample.StartSeconds,
			FrameIndex:       1,
		})
	}

	if !request.Media.HasAudio {
		findings = append(findings, domain.Finding{
			ID:       "baseline_audio_missing",
			Severity: domain.FindingSeverityMedium,
			Category: "media_quality",
			Title:    "Audio stream is missing",
			Detail:   "The video has no detected audio stream. Voice comms and sound-cue analysis would be unavailable for this VOD.",
		})
	}

	if request.Media.Width > 0 && request.Media.Height > 0 && (request.Media.Width < 1280 || request.Media.Height < 720) {
		findings = append(findings, domain.Finding{
			ID:       "baseline_low_resolution",
			Severity: domain.FindingSeverityMedium,
			Category: "media_quality",
			Title:    "Capture resolution is below 720p",
			Detail:   "Low resolution can make HUD, minimap, killfeed, weapon, and utility recognition less reliable.",
		})
	}

	if request.Media.HasDuration && request.VOD.ManifestDurationSeconds > 0 {
		delta := math.Abs(request.Media.DurationSeconds - request.VOD.ManifestDurationSeconds)
		if delta > 120 {
			findings = append(findings, domain.Finding{
				ID:       "baseline_duration_mismatch",
				Severity: domain.FindingSeverityLow,
				Category: "dataset_quality",
				Title:    "Manifest duration differs from media duration",
				Detail:   fmt.Sprintf("The downloaded media duration differs from the manifest by %.0f seconds. The dataset row should be reviewed.", delta),
			})
		}
	}

	if request.Media.HasDuration && request.Sample.DurationSeconds > 0 {
		covered := request.Sample.DurationSeconds
		if covered < request.Media.DurationSeconds*0.95 {
			findings = append(findings, domain.Finding{
				ID:       "baseline_partial_sample",
				Severity: domain.FindingSeverityInfo,
				Category: "coverage",
				Title:    "Only part of the VOD was sampled",
				Detail:   fmt.Sprintf("This run sampled %.0f seconds out of %.0f seconds. Use --duration 0 when you want a full-video extraction benchmark.", covered, request.Media.DurationSeconds),
				Tags:     []string{"sampling"},
			})
		}
	}

	timeline := []domain.TimelineEvent{
		{
			TimestampSeconds: request.Sample.StartSeconds,
			Type:             "sample_started",
			Title:            "Frame sampling started",
		},
	}
	if request.Sample.DurationSeconds > 0 {
		timeline = append(timeline, domain.TimelineEvent{
			TimestampSeconds: request.Sample.StartSeconds + request.Sample.DurationSeconds,
			Type:             "sample_finished",
			Title:            "Frame sampling finished",
		})
	}

	return ObservationResult{
		Findings: findings,
		Timeline: timeline,
	}, nil
}
