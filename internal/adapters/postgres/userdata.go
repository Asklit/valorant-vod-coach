package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	userDocumentManualCorrections = "manual_corrections"
	userDocumentGuidedReviews     = "guided_reviews"
)

func (s Store) LoadManualCorrections(ctx context.Context, ownerID string, vodLabel string, reportRunID string) (domain.ManualCorrectionSet, bool, error) {
	raw, found, err := s.loadUserDocument(ctx, ownerID, userDocumentManualCorrections, vodLabel, reportRunID)
	if err != nil || !found {
		return domain.ManualCorrectionSet{}, found, err
	}
	var set domain.ManualCorrectionSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return domain.ManualCorrectionSet{}, false, fmt.Errorf("decode manual corrections: %w", err)
	}
	return set, true, nil
}

func (s Store) SaveManualCorrections(ctx context.Context, ownerID string, set domain.ManualCorrectionSet) error {
	return s.saveUserDocument(ctx, ownerID, userDocumentManualCorrections, set.VODLabel, set.ReportRunID, set.SchemaVersion, set)
}

func (s Store) LoadGuidedReviews(ctx context.Context, ownerID string, vodLabel string, reportRunID string) (domain.GuidedReviewSet, bool, error) {
	raw, found, err := s.loadUserDocument(ctx, ownerID, userDocumentGuidedReviews, vodLabel, reportRunID)
	if err != nil || !found {
		return domain.GuidedReviewSet{}, found, err
	}
	var set domain.GuidedReviewSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return domain.GuidedReviewSet{}, false, fmt.Errorf("decode guided reviews: %w", err)
	}
	return set, true, nil
}

func (s Store) SaveGuidedReviews(ctx context.Context, ownerID string, set domain.GuidedReviewSet) error {
	return s.saveUserDocument(ctx, ownerID, userDocumentGuidedReviews, set.VODLabel, set.ReportRunID, set.SchemaVersion, set)
}

func (s Store) loadUserDocument(ctx context.Context, ownerID string, kind string, vodLabel string, reportRunID string) ([]byte, bool, error) {
	if s.DB == nil {
		return nil, false, errors.New("postgres store requires DB")
	}
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(vodLabel) == "" {
		return nil, false, errors.New("document owner ID and VOD label are required")
	}
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `
SELECT body
FROM user_documents
WHERE owner_id = $1 AND kind = $2 AND vod_label = $3 AND report_run_id = $4
`, strings.TrimSpace(ownerID), kind, strings.TrimSpace(vodLabel), strings.TrimSpace(reportRunID)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load user document: %w", err)
	}
	return raw, true, nil
}

func (s Store) saveUserDocument(ctx context.Context, ownerID string, kind string, vodLabel string, reportRunID string, schemaVersion int, body any) error {
	if s.DB == nil {
		return errors.New("postgres store requires DB")
	}
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(vodLabel) == "" {
		return errors.New("document owner ID and VOD label are required")
	}
	if schemaVersion <= 0 {
		schemaVersion = 1
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode user document: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `
INSERT INTO user_documents (
  owner_id, kind, vod_label, report_run_id, schema_version, body, updated_at
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, now())
ON CONFLICT (owner_id, kind, vod_label, report_run_id) DO UPDATE SET
  schema_version = EXCLUDED.schema_version,
  body = EXCLUDED.body,
  updated_at = now()
`, strings.TrimSpace(ownerID), kind, strings.TrimSpace(vodLabel), strings.TrimSpace(reportRunID), schemaVersion, string(raw))
	if err != nil {
		return fmt.Errorf("save user document: %w", err)
	}
	return nil
}

var _ app.UserDataStore = Store{}
