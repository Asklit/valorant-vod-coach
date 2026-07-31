package domain

import "time"

const GuidedReviewSetSchemaVersion = 1

type GuidedReviewSet struct {
	SchemaVersion int                      `json:"schema_version"`
	VODLabel      string                   `json:"vod_label"`
	ReportRunID   string                   `json:"report_run_id"`
	UpdatedAt     time.Time                `json:"updated_at,omitempty"`
	Assessments   []GuidedReviewAssessment `json:"assessments"`
}

type GuidedReviewAssessment struct {
	ID         string                       `json:"id"`
	DecisionID string                       `json:"decision_id"`
	WindowID   string                       `json:"window_id"`
	Answers    map[string]string            `json:"answers"`
	Result     CoachDecision                `json:"result"`
	Author     string                       `json:"author,omitempty"`
	CreatedAt  time.Time                    `json:"created_at"`
	UpdatedAt  time.Time                    `json:"updated_at"`
	Feedback   *CoachRecommendationFeedback `json:"feedback,omitempty"`
}

type CoachRecommendationFeedback struct {
	Verdict   string    `json:"verdict"`
	Comment   string    `json:"comment,omitempty"`
	Author    string    `json:"author,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}
