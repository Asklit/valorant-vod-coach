package app

import (
	"context"
	"errors"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

var ErrReportNotFound = errors.New("analysis report not found")

type AnalysisCatalog interface {
	SaveAnalysisResult(ctx context.Context, request PersistAnalysisRequest) error
}

type ReportCatalog interface {
	ListReportSummaries(ctx context.Context, ownerID string, vodLabel string, includeSystem bool) ([]ReportCatalogSummary, error)
	FindReport(ctx context.Context, ownerID string, vodLabel string, runID string, includeSystem bool) (ReportCatalogRecord, bool, error)
}

type PersistAnalysisRequest struct {
	Report domain.AnalysisReport
	Saved  SavedReport
}

type ReportCatalogRecord struct {
	Report       domain.AnalysisReport
	JSONPath     string
	MarkdownPath string
}

type ReportCatalogSummary struct {
	OwnerID              string
	SchemaVersion        int
	VODLabel             string
	RunID                string
	Status               string
	GeneratedAt          time.Time
	FindingCount         int
	FrameCount           int
	ReviewWindowCount    int
	RoundSegmentCount    int
	ModelReviewTaskCount int
	ModelReviewRunCount  int
	Analyzer             string
	Mode                 string
	SampleName           string
	SampleFPS            string
	SampleDuration       float64
	ContactSheetPath     string
	JSONPath             string
	MarkdownPath         string
}
