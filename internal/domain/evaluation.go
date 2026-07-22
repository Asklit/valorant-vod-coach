package domain

import "time"

const EvaluationReportSchemaVersion = 1

type EvaluationAnnotationSet struct {
	SchemaVersion    int               `json:"schema_version"`
	VODLabel         string            `json:"vod_label"`
	ReportRunID      string            `json:"report_run_id,omitempty"`
	ToleranceSeconds float64           `json:"tolerance_seconds,omitempty"`
	Labels           []EvaluationLabel `json:"labels"`
}

type EvaluationLabel struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Category         string          `json:"category,omitempty"`
	Severity         FindingSeverity `json:"severity,omitempty"`
	TimestampSeconds float64         `json:"timestamp_seconds"`
	StartSeconds     float64         `json:"start_seconds,omitempty"`
	EndSeconds       float64         `json:"end_seconds,omitempty"`
	RoundNumber      int             `json:"round_number,omitempty"`
	Description      string          `json:"description,omitempty"`
	Tags             []string        `json:"tags,omitempty"`
}

type GameplayEvaluationReport struct {
	SchemaVersion    int                     `json:"schema_version"`
	RunID            string                  `json:"run_id"`
	GeneratedAt      time.Time               `json:"generated_at"`
	VODLabel         string                  `json:"vod_label"`
	ReportRunID      string                  `json:"report_run_id"`
	ToleranceSeconds float64                 `json:"tolerance_seconds"`
	Overall          EvaluationMetrics       `json:"overall"`
	ByType           []EvaluationTypeMetrics `json:"by_type,omitempty"`
	Matches          []EvaluationMatch       `json:"matches,omitempty"`
	MissedLabels     []EvaluationLabel       `json:"missed_labels,omitempty"`
	FalsePositives   []GameplayEvent         `json:"false_positives,omitempty"`
	Notes            []string                `json:"notes,omitempty"`
}

type EvaluationMetrics struct {
	LabelCount      int     `json:"label_count"`
	PredictionCount int     `json:"prediction_count"`
	MatchCount      int     `json:"match_count"`
	Precision       float64 `json:"precision"`
	Recall          float64 `json:"recall"`
	F1              float64 `json:"f1"`
}

type EvaluationTypeMetrics struct {
	Type    string            `json:"type"`
	Metrics EvaluationMetrics `json:"metrics"`
}

type EvaluationMatch struct {
	Label        EvaluationLabel `json:"label"`
	Event        GameplayEvent   `json:"event"`
	DeltaSeconds float64         `json:"delta_seconds"`
}
