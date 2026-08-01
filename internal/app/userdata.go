package app

import (
	"context"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type UserDataStore interface {
	LoadManualCorrections(ctx context.Context, ownerID string, vodLabel string, reportRunID string) (domain.ManualCorrectionSet, bool, error)
	SaveManualCorrections(ctx context.Context, ownerID string, set domain.ManualCorrectionSet) error
	LoadGuidedReviews(ctx context.Context, ownerID string, vodLabel string, reportRunID string) (domain.GuidedReviewSet, bool, error)
	SaveGuidedReviews(ctx context.Context, ownerID string, set domain.GuidedReviewSet) error
}
