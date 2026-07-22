package domain

import "time"

const ManualCorrectionSetSchemaVersion = 1

type ManualCorrectionSet struct {
	SchemaVersion int                `json:"schema_version"`
	VODLabel      string             `json:"vod_label"`
	ReportRunID   string             `json:"report_run_id,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at"`
	Corrections   []ManualCorrection `json:"corrections"`
}

type ManualCorrection struct {
	ID               string     `json:"id"`
	Type             string     `json:"type"`
	TargetID         string     `json:"target_id,omitempty"`
	CorrectedValue   string     `json:"corrected_value,omitempty"`
	Comment          string     `json:"comment,omitempty"`
	TimestampSeconds *float64   `json:"timestamp_seconds,omitempty"`
	Status           string     `json:"status"`
	Author           string     `json:"author,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
}
