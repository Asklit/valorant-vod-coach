package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestGuidedReviewAssessmentPersistsAndUpdates(t *testing.T) {
	root := t.TempDir()
	report := guidedReviewTestReport(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	answers := map[string]string{
		"fight_occurred": "yes", "outcome": "death", "tradeable": "no", "utility_available": "yes",
		"utility_used": "no", "crosshair_ready": "yes", "escape_route": "no",
	}
	set, saved, err := SaveGuidedAssessment(context.Background(), root, nil, SaveGuidedAssessmentRequest{
		Report: report, WindowID: "combat_001", Answers: answers, Author: "coach@example.com", Now: now,
	})
	if err != nil {
		t.Fatalf("save assessment: %v", err)
	}
	if len(set.Assessments) != 1 || set.Assessments[0].Result.RuleID != "combat_untradeable_contact" {
		t.Fatalf("unexpected set: %+v", set)
	}
	if saved.JSONPath != filepath.Join(root, "diamond_example", "run_01", GuidedReviewsJSONName) {
		t.Fatalf("unexpected path: %s", saved.JSONPath)
	}

	answers["tradeable"] = "yes"
	answers["crosshair_ready"] = "no"
	set, _, err = SaveGuidedAssessment(context.Background(), root, nil, SaveGuidedAssessmentRequest{
		Report: report, WindowID: "combat_001", Answers: answers, Author: "coach@example.com", Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update assessment: %v", err)
	}
	if len(set.Assessments) != 1 || set.Assessments[0].Result.RuleID != "combat_utility_sequence" || !set.Assessments[0].CreatedAt.Equal(now) {
		t.Fatalf("assessment must be updated in place: %+v", set.Assessments)
	}

	loaded, _, err := LoadGuidedReviews(root, report.VOD.Label, report.RunID)
	if err != nil || len(loaded.Assessments) != 1 {
		t.Fatalf("reload: %+v / %v", loaded, err)
	}
}

func TestCoachFeedbackRequiresAssessment(t *testing.T) {
	_, _, err := SaveCoachFeedback(context.Background(), t.TempDir(), SaveCoachFeedbackRequest{
		VODLabel: "diamond_example", ReportRunID: "run_01", WindowID: "combat_001", Verdict: "useful",
	})
	if err == nil {
		t.Fatal("expected missing assessment error")
	}
}

func guidedReviewTestReport(t *testing.T) domain.AnalysisReport {
	t.Helper()
	engine := EvidenceCoachEngine{}
	gameplay := domain.GameplaySummary{
		SampledFrames: 10, AnalyzedFrames: 10, AverageHUDSignal: .3, AverageMinimapSignal: .3,
		ReviewWindows: []domain.ReviewWindow{{ID: "combat_001", Kind: "combat_spike", Score: .8, PeakSeconds: 42}},
	}
	review, err := engine.BuildReview(context.Background(), CoachReviewRequest{
		Media: domain.MediaSummary{HasAudio: true}, Sample: domain.FrameSampleSummary{FPSValue: 1}, Gameplay: gameplay,
	})
	if err != nil {
		t.Fatalf("build review: %v", err)
	}
	gameplay.CoachReview = review
	return domain.AnalysisReport{RunID: "run_01", VOD: domain.VOD{Label: "diamond_example"}, Gameplay: &gameplay}
}
