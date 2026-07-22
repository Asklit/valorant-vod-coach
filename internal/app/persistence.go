package app

import (
	"context"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type AnalysisCatalog interface {
	SaveAnalysisResult(ctx context.Context, request PersistAnalysisRequest) error
}

type PersistAnalysisRequest struct {
	Report domain.AnalysisReport
	Saved  SavedReport
}
