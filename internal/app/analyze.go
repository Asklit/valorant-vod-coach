package app

import (
	"context"
	"fmt"
	"math"
	"sort"
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

type ReviewClipExtractor interface {
	ExtractReviewClips(ctx context.Context, vod domain.VOD, videoPath string, request ReviewClipRequest) (ReviewClipResult, error)
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

type ReviewClipRequest struct {
	RunID      string
	SampleName string
	Windows    []domain.ReviewWindow
	Overwrite  bool
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

type ReviewClipResult struct {
	Windows   []domain.ReviewWindow
	Artifacts []domain.Artifact
}

type ObservationRequest struct {
	RunID       string
	GeneratedAt time.Time
	VOD         domain.VOD
	Media       domain.MediaSummary
	Sample      domain.FrameSampleSummary
}

type ObservationResult struct {
	Findings  []domain.Finding
	Timeline  []domain.TimelineEvent
	Gameplay  *domain.GameplaySummary
	Artifacts []domain.Artifact
	Metadata  domain.AnalysisRunMetadata
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

	if observations.Gameplay != nil && len(observations.Gameplay.ReviewWindows) > 0 {
		if extractor, ok := r.Media.(ReviewClipExtractor); ok {
			clips, err := extractor.ExtractReviewClips(ctx, vod, videoPath, ReviewClipRequest{
				RunID:      runID,
				SampleName: sampleName,
				Windows:    observations.Gameplay.ReviewWindows,
				Overwrite:  request.Overwrite,
			})
			if err != nil {
				return RunAnalysisResult{}, err
			}
			if len(clips.Windows) > 0 {
				observations.Gameplay.ReviewWindows = clips.Windows
				observations.Gameplay.ReviewWindowCount = len(clips.Windows)
			}
			observations.Artifacts = append(observations.Artifacts, clips.Artifacts...)
		}
	}

	report := domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion,
		RunID:         runID,
		Status:        "completed",
		GeneratedAt:   now,
		VOD:           vod,
		Media:         probe.Summary,
		Sample:        sample.Summary,
		Gameplay:      observations.Gameplay,
		Findings:      observations.Findings,
		Timeline:      observations.Timeline,
		Artifacts: []domain.Artifact{
			probe.Artifact,
			sample.Artifact,
		},
		Metadata: domain.AnalysisRunMetadata{
			Analyzer: "heuristic-baseline",
			Mode:     "local",
		},
	}
	if sample.ContactSheetArtifact.Path != "" {
		report.Artifacts = append(report.Artifacts, sample.ContactSheetArtifact)
	}
	report.Artifacts = append(report.Artifacts, observations.Artifacts...)
	sortTimeline(report.Timeline)
	if observations.Metadata.Analyzer != "" {
		report.Metadata = observations.Metadata
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
			ID:             "baseline_ingestion_ready",
			Severity:       domain.FindingSeverityInfo,
			Category:       "pipeline",
			Title:          "Local analysis artifacts are ready",
			Detail:         "The pipeline loaded the VOD, probed media metadata, sampled frames, generated review artifacts, and wrote a reproducible report.",
			Recommendation: "Use this run to verify the video, contact sheet, sampled frames, and report contract before enabling OCR or VLM gameplay review.",
			Confidence:     1,
			Tags:           []string{"mvp", "local-pipeline"},
		},
		{
			ID:             "baseline_ai_not_enabled",
			Severity:       domain.FindingSeverityInfo,
			Category:       "analysis_gap",
			Title:          "Gameplay AI review is not enabled yet",
			Detail:         "This run is a deterministic heuristic baseline. It does not yet inspect minimap, HUD, crosshair placement, utility, economy, or positioning.",
			Recommendation: "The next MVP stage should add the Python vision-service adapter: OCR timer/score/HUD, detect round windows, then run VLM review only on selected clips.",
			Confidence:     1,
			Tags:           []string{"baseline", "vision-service"},
		},
	}

	if request.Sample.FrameCount == 0 {
		findings = append(findings, domain.Finding{
			ID:             "baseline_no_frames",
			Severity:       domain.FindingSeverityHigh,
			Category:       "media_quality",
			Title:          "No frames were extracted",
			Detail:         "The frame sample is empty, so downstream visual analysis cannot run on this artifact.",
			Recommendation: "Check that ffmpeg can decode the file, then retry with a short 30-60 second sample before running a full VOD pass.",
			Confidence:     1,
		})
	} else {
		findings[0].Evidence = append(findings[0].Evidence, domain.EvidenceRef{
			ArtifactType:     "frame_sample",
			Path:             request.Sample.ManifestPath,
			TimestampSeconds: request.Sample.StartSeconds,
			FrameIndex:       1,
		})
		if request.Sample.ContactSheetPath != "" {
			findings[0].Evidence = append(findings[0].Evidence, domain.EvidenceRef{
				ArtifactType:     "contact_sheet",
				Path:             request.Sample.ContactSheetPath,
				TimestampSeconds: request.Sample.StartSeconds,
			})
			findings = append(findings, domain.Finding{
				ID:             "baseline_manual_review_ready",
				Severity:       domain.FindingSeverityLow,
				Category:       "review",
				Title:          "Manual evidence board is ready",
				Detail:         "The contact sheet gives a quick visual overview of the sampled window and the frame grid exposes timestamped evidence.",
				Recommendation: "Scan the contact sheet for buy phases, deaths, score changes, scoreboard moments, and visible HUD/minimap quality. Use it to audit the automated review windows and catch false positives.",
				Confidence:     0.9,
				Evidence: []domain.EvidenceRef{
					{ArtifactType: "contact_sheet", Path: request.Sample.ContactSheetPath, TimestampSeconds: request.Sample.StartSeconds},
				},
				Tags: []string{"manual-review", "evidence"},
			})
		}
	}

	if !request.Media.HasAudio {
		findings = append(findings, domain.Finding{
			ID:             "baseline_audio_missing",
			Severity:       domain.FindingSeverityMedium,
			Category:       "media_quality",
			Title:          "Audio stream is missing",
			Detail:         "The video has no detected audio stream. Voice comms and sound-cue analysis would be unavailable for this VOD.",
			Recommendation: "Prefer VODs with game audio when evaluating sound-cue awareness, reload discipline, rotations, and post-plant decisions.",
			Confidence:     1,
		})
	}

	if request.Media.Width > 0 && request.Media.Height > 0 && (request.Media.Width < 1280 || request.Media.Height < 720) {
		findings = append(findings, domain.Finding{
			ID:             "baseline_low_resolution",
			Severity:       domain.FindingSeverityMedium,
			Category:       "media_quality",
			Title:          "Capture resolution is below 720p",
			Detail:         "Low resolution can make HUD, minimap, killfeed, weapon, and utility recognition less reliable.",
			Recommendation: "Use 1080p recordings for the dataset whenever possible; OCR and minimap detectors will be materially more reliable.",
			Confidence:     1,
		})
	}

	if request.Media.HasDuration && request.VOD.ManifestDurationSeconds > 0 {
		delta := math.Abs(request.Media.DurationSeconds - request.VOD.ManifestDurationSeconds)
		if delta > 120 {
			findings = append(findings, domain.Finding{
				ID:             "baseline_duration_mismatch",
				Severity:       domain.FindingSeverityLow,
				Category:       "dataset_quality",
				Title:          "Manifest duration differs from media duration",
				Detail:         fmt.Sprintf("The downloaded media duration differs from the manifest by %.0f seconds. The dataset row should be reviewed.", delta),
				Recommendation: "Update the manifest duration or replace the VOD if it contains menus, cuts, or non-match footage.",
				Confidence:     1,
			})
		}
	}

	if request.Media.HasDuration && request.Sample.DurationSeconds > 0 {
		covered := request.Sample.DurationSeconds
		if covered < request.Media.DurationSeconds*0.95 {
			severity := domain.FindingSeverityInfo
			title := "Only part of the VOD was sampled"
			recommendation := "Use a 180-300 second sample for quick iteration, then run duration 0 when you want a full-match extraction benchmark."
			if covered < 120 {
				severity = domain.FindingSeverityMedium
				title = "Sample is too short for gameplay coaching"
				recommendation = "Increase sample seconds to at least 180 for a meaningful review window, or run full VOD mode when you are ready for a slower pass."
			}
			findings = append(findings, domain.Finding{
				ID:             "baseline_partial_sample",
				Severity:       severity,
				Category:       "coverage",
				Title:          title,
				Detail:         fmt.Sprintf("This run sampled %.0f seconds out of %.0f seconds. A very short sample is useful for pipeline smoke tests, not for real gameplay feedback.", covered, request.Media.DurationSeconds),
				Recommendation: recommendation,
				Confidence:     1,
				Tags:           []string{"sampling"},
			})
		}
	}

	if request.Sample.FPSValue > 0 && request.Sample.FPSValue < 1 {
		findings = append(findings, domain.Finding{
			ID:             "baseline_sparse_sampling",
			Severity:       domain.FindingSeverityLow,
			Category:       "coverage",
			Title:          "Frame sampling is sparse",
			Detail:         fmt.Sprintf("The run sampled at %.2f fps. This is fine for coarse smoke tests but can miss kills, deaths, utility usage, and scoreboard moments.", request.Sample.FPSValue),
			Recommendation: "Use 1 fps for general timeline detection and 2 fps for short windows where OCR or VLM review needs more temporal detail.",
			Confidence:     1,
			Tags:           []string{"sampling"},
		})
	}

	timeline := []domain.TimelineEvent{
		{
			TimestampSeconds: request.Sample.StartSeconds,
			Type:             "sample_started",
			Title:            "Frame sampling started",
		},
	}
	if request.Sample.ContactSheetPath != "" {
		timeline = append(timeline, domain.TimelineEvent{
			TimestampSeconds: request.Sample.StartSeconds,
			Type:             "contact_sheet_ready",
			Title:            "Contact sheet generated",
			Detail:           request.Sample.ContactSheetPath,
		})
	}
	if endSeconds, ok := sampleEndSeconds(request); ok {
		timeline = append(timeline, domain.TimelineEvent{
			TimestampSeconds: endSeconds,
			Type:             "sample_finished",
			Title:            "Frame sampling finished",
		})
	}

	return ObservationResult{
		Findings: findings,
		Timeline: timeline,
		Metadata: domain.AnalysisRunMetadata{
			Analyzer: "heuristic-baseline",
			Mode:     "local",
		},
	}, nil
}

func sampleEndSeconds(request ObservationRequest) (float64, bool) {
	if request.Sample.DurationSeconds > 0 {
		return request.Sample.StartSeconds + request.Sample.DurationSeconds, true
	}
	if len(request.Sample.Frames) > 0 {
		return request.Sample.Frames[len(request.Sample.Frames)-1].TimestampSeconds, true
	}
	if request.Media.HasDuration && request.Media.DurationSeconds > 0 {
		return request.Media.DurationSeconds, true
	}
	return 0, false
}

func sortTimeline(events []domain.TimelineEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].TimestampSeconds == events[j].TimestampSeconds {
			leftPriority := timelinePriority(events[i].Type)
			rightPriority := timelinePriority(events[j].Type)
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
			return events[i].Type < events[j].Type
		}
		return events[i].TimestampSeconds < events[j].TimestampSeconds
	})
}

func timelinePriority(eventType string) int {
	switch eventType {
	case "sample_started":
		return 0
	case "contact_sheet_ready":
		return 1
	case "sample_finished":
		return 9
	default:
		return 5
	}
}
