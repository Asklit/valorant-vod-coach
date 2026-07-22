package app

import (
	"context"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type AnalysisCatalog interface {
	SaveAnalysisResult(ctx context.Context, request PersistAnalysisRequest) error
}

type ReportCatalog interface {
	ListReportSummaries(ctx context.Context, vodLabel string) ([]ReportCatalogSummary, error)
}

type PersistAnalysisRequest struct {
	Report domain.AnalysisReport
	Saved  SavedReport
}

type ReportCatalogSummary struct {
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
