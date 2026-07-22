package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const ManualCorrectionsJSONName = "corrections.json"

type SavedManualCorrections struct {
	JSONPath string
}

func LoadManualCorrections(root string, vodLabel string, reportRunID string) (domain.ManualCorrectionSet, SavedManualCorrections, error) {
	path := manualCorrectionsPath(root, vodLabel, reportRunID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyManualCorrectionSet(vodLabel, reportRunID), SavedManualCorrections{JSONPath: path}, nil
		}
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}

	var set domain.ManualCorrectionSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}
	if set.SchemaVersion == 0 {
		set.SchemaVersion = domain.ManualCorrectionSetSchemaVersion
	}
	if set.VODLabel == "" {
		set.VODLabel = vodLabel
	}
	if set.ReportRunID == "" {
		set.ReportRunID = reportRunID
	}
	return set, SavedManualCorrections{JSONPath: path}, nil
}

func AppendManualCorrection(ctx context.Context, root string, vodLabel string, reportRunID string, correction domain.ManualCorrection, now time.Time) (domain.ManualCorrectionSet, SavedManualCorrections, error) {
	if err := ctx.Err(); err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}
	set, saved, err := LoadManualCorrections(root, vodLabel, reportRunID)
	if err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}

	correction, err = normalizeManualCorrection(correction, now)
	if err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}
	set.SchemaVersion = domain.ManualCorrectionSetSchemaVersion
	set.VODLabel = strings.TrimSpace(vodLabel)
	set.ReportRunID = strings.TrimSpace(reportRunID)
	set.UpdatedAt = now.UTC()
	set.Corrections = append(set.Corrections, correction)

	if err := os.MkdirAll(filepath.Dir(saved.JSONPath), 0o755); err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}
	raw, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(saved.JSONPath, raw, 0o644); err != nil {
		return domain.ManualCorrectionSet{}, SavedManualCorrections{}, err
	}
	return set, saved, nil
}

func emptyManualCorrectionSet(vodLabel string, reportRunID string) domain.ManualCorrectionSet {
	return domain.ManualCorrectionSet{
		SchemaVersion: domain.ManualCorrectionSetSchemaVersion,
		VODLabel:      strings.TrimSpace(vodLabel),
		ReportRunID:   strings.TrimSpace(reportRunID),
		Corrections:   []domain.ManualCorrection{},
	}
}

func normalizeManualCorrection(correction domain.ManualCorrection, now time.Time) (domain.ManualCorrection, error) {
	correction.Type = strings.TrimSpace(correction.Type)
	correction.TargetID = strings.TrimSpace(correction.TargetID)
	correction.CorrectedValue = strings.TrimSpace(correction.CorrectedValue)
	correction.Comment = strings.TrimSpace(correction.Comment)
	correction.Status = strings.TrimSpace(correction.Status)
	correction.Author = strings.TrimSpace(correction.Author)

	if correction.Type == "" {
		return domain.ManualCorrection{}, errors.New("correction type is required")
	}
	if correction.CorrectedValue == "" && correction.Comment == "" {
		return domain.ManualCorrection{}, errors.New("correction value or comment is required")
	}
	if correction.Status == "" {
		correction.Status = "open"
	}
	if correction.ID == "" {
		correction.ID = fmt.Sprintf("correction_%s", now.UTC().Format("20060102T150405.000000000Z"))
	}
	if correction.CreatedAt.IsZero() {
		correction.CreatedAt = now.UTC()
	} else {
		correction.CreatedAt = correction.CreatedAt.UTC()
	}
	return correction, nil
}

func manualCorrectionsPath(root string, vodLabel string, reportRunID string) string {
	report := safeEvalName(reportRunID)
	if strings.TrimSpace(reportRunID) == "" {
		report = "general"
	}
	return filepath.Join(root, safeEvalName(vodLabel), report, ManualCorrectionsJSONName)
}
