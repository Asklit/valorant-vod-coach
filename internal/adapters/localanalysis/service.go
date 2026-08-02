package localanalysis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/dataset"
	"github.com/asklit/valorant-vod-coach/internal/adapters/media"
	reportstore "github.com/asklit/valorant-vod-coach/internal/adapters/report"
	"github.com/asklit/valorant-vod-coach/internal/adapters/vision"
	"github.com/asklit/valorant-vod-coach/internal/adapters/visionservice"
	"github.com/asklit/valorant-vod-coach/internal/adapters/vodstore"
	"github.com/asklit/valorant-vod-coach/internal/app"
)

type Config struct {
	ManifestPath  string
	RawRoot       string
	UploadRoot    string
	ProcessedRoot string
	FFprobePath   string
	FFmpegPath    string
	TesseractPath string
	VisionURL     string
	UploadCatalog app.UploadCatalog
	Catalog       app.AnalysisCatalog
	Locks         app.LockManager
	Objects       app.BlobStore
}

type Service struct {
	Config Config
}

func (s Service) RunAnalysis(ctx context.Context, request app.AnalysisJobRequest, progress app.AnalysisProgressReporter) (app.AnalysisExecutionResult, error) {
	if err := validateRequest(request); err != nil {
		return app.AnalysisExecutionResult{}, err
	}

	uploads := vodstore.Store(vodstore.LocalStore{
		Root:        s.Config.UploadRoot,
		FFprobePath: defaultString(s.Config.FFprobePath, "ffprobe"),
	})
	if s.Config.UploadCatalog != nil {
		uploads = vodstore.CatalogStore{
			Files: vodstore.LocalStore{
				Root:        s.Config.UploadRoot,
				FFprobePath: defaultString(s.Config.FFprobePath, "ffprobe"),
			},
			Catalog: s.Config.UploadCatalog,
			Objects: s.Config.Objects,
		}
	}

	processedRoot := ProcessedRootForOwner(s.Config.ProcessedRoot, request.OwnerID)
	localReports := reportstore.LocalStore{ProcessedRoot: processedRoot}
	var reports app.ReportStore = localReports
	if s.Config.Objects != nil {
		reports = reportstore.PublishingStore{
			Local:   localReports,
			Root:    s.Config.ProcessedRoot,
			Objects: s.Config.Objects,
		}
	}
	runner := app.AnalysisRunner{
		Resolver: vodstore.OwnedResolver{
			Store:      uploads,
			OwnerID:    request.OwnerID,
			IncludeAll: request.IncludeAllVODs,
			Fallback: dataset.LocalVODResolver{
				ManifestPath: s.Config.ManifestPath,
				RawRoot:      s.Config.RawRoot,
			},
		},
		Media: media.LocalProcessor{
			FFprobePath:   defaultString(s.Config.FFprobePath, "ffprobe"),
			FFmpegPath:    defaultString(s.Config.FFmpegPath, "ffmpeg"),
			ProcessedRoot: processedRoot,
			ProbeTimeout:  30 * time.Second,
			SampleTimeout: SampleTimeout(request.DurationSeconds),
		},
		Analyzer: vision.LocalGameplayAnalyzer{
			TesseractPath: defaultString(s.Config.TesseractPath, "tesseract"),
		},
		Reports:  reports,
		Catalog:  s.Config.Catalog,
		Locks:    s.Config.Locks,
		Progress: progress,
	}
	if request.ModelReview {
		if strings.TrimSpace(s.Config.VisionURL) == "" {
			return app.AnalysisExecutionResult{}, errors.New("model review requested but vision service URL is not configured")
		}
		runner.Reviewer = visionservice.Client{BaseURL: s.Config.VisionURL}
	}

	result, err := runner.Run(ctx, app.RunAnalysisRequest{
		VODLabel:     request.VODLabel,
		OwnerID:      request.OwnerID,
		RunID:        request.RunID,
		FPS:          request.FPS,
		Start:        secondsDuration(request.StartSeconds),
		Duration:     secondsDuration(request.DurationSeconds),
		ImageQuality: request.ImageQuality,
		Overwrite:    request.Force,
		ModelReview:  request.ModelReview,
	})
	if err != nil {
		return app.AnalysisExecutionResult{}, err
	}
	return app.AnalysisExecutionResult{
		ReportJSONPath: result.Saved.JSONPath,
		ReportMDPath:   result.Saved.MarkdownPath,
	}, nil
}

func ProcessedRootForOwner(root string, ownerID string) string {
	if !isSafeResourceID(ownerID) {
		return filepath.Join(root, "users", "invalid", "analyses")
	}
	return filepath.Join(root, "users", ownerID, "analyses")
}

func OverallTimeout(durationSeconds float64) time.Duration {
	if durationSeconds == 0 || durationSeconds > 10*60 {
		return 50 * time.Minute
	}
	return 15 * time.Minute
}

func SampleTimeout(durationSeconds float64) time.Duration {
	if durationSeconds == 0 || durationSeconds > 10*60 {
		return 45 * time.Minute
	}
	return 10 * time.Minute
}

func validateRequest(request app.AnalysisJobRequest) error {
	if request.SchemaVersion != app.AnalysisJobRequestSchemaVersion {
		return fmt.Errorf("unsupported analysis job request schema version %d", request.SchemaVersion)
	}
	if strings.TrimSpace(request.VODLabel) == "" || strings.TrimSpace(request.OwnerID) == "" || strings.TrimSpace(request.RunID) == "" {
		return errors.New("analysis VOD label, owner ID, and run ID are required")
	}
	if !isSafeResourceID(request.OwnerID) {
		return errors.New("analysis owner ID is invalid")
	}
	if strings.TrimSpace(request.FPS) == "" {
		return errors.New("analysis FPS is required")
	}
	if math.IsNaN(request.StartSeconds) || math.IsInf(request.StartSeconds, 0) || request.StartSeconds < 0 {
		return errors.New("analysis start seconds must be finite and non-negative")
	}
	if math.IsNaN(request.DurationSeconds) || math.IsInf(request.DurationSeconds, 0) || request.DurationSeconds < 0 {
		return errors.New("analysis duration seconds must be finite and non-negative")
	}
	if request.ImageQuality < 1 || request.ImageQuality > 31 {
		return errors.New("analysis image quality must be between 1 and 31")
	}
	return nil
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func isSafeResourceID(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var _ app.AnalysisExecutor = Service{}
