package app

import (
	"context"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestEvidenceCoachBuildsCandidatesWithoutClaimingMistakes(t *testing.T) {
	engine := EvidenceCoachEngine{}
	review, err := engine.BuildReview(context.Background(), CoachReviewRequest{
		Media:  domain.MediaSummary{HasAudio: true},
		Sample: domain.FrameSampleSummary{FPSValue: 1, FrameCount: 100},
		Gameplay: domain.GameplaySummary{
			SampledFrames:        100,
			AnalyzedFrames:       100,
			AverageHUDSignal:     0.3,
			AverageMinimapSignal: 0.3,
			ReviewWindows: []domain.ReviewWindow{{
				ID: "combat_001", Kind: "combat_spike", PeakSeconds: 42, Score: 0.8,
				Summary: "Visual intensity peaked at 00:42.",
			}},
		},
	})
	if err != nil {
		t.Fatalf("build review: %v", err)
	}
	if review.Status != "guided_review_required" || len(review.Decisions) != 1 {
		t.Fatalf("unexpected review: %+v", review)
	}
	decision := review.Decisions[0]
	if decision.Assessment != "needs_confirmation" || decision.Recommendation != nil {
		t.Fatalf("candidate must not claim a mistake: %+v", decision)
	}
	if decision.Confidence > 0.64 || len(decision.Questions) == 0 || len(decision.Requirements) == 0 {
		t.Fatalf("unexpected evidence contract: %+v", decision)
	}
}

func TestEvidenceCoachConfirmsUntradeableCombat(t *testing.T) {
	engine := EvidenceCoachEngine{}
	decision := decisionForWindow(domain.ReviewWindow{ID: "combat_001", Kind: "combat_spike", Score: 0.8}, domain.CoachEvidenceQuality{Score: 0.8})
	result, err := engine.AssessDecision(context.Background(), CoachAssessmentRequest{
		Decision: decision,
		Answers: map[string]string{
			"fight_occurred": "yes", "outcome": "death", "tradeable": "no",
			"utility_available": "yes", "utility_used": "no", "crosshair_ready": "yes", "escape_route": "no",
		},
	})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if result.RuleID != "combat_untradeable_contact" || result.Assessment != "validated_risk" || result.Recommendation == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Confidence < 0.85 {
		t.Fatalf("expected confirmation confidence, got %.2f", result.Confidence)
	}
}

func TestEvidenceCoachLeavesIncompleteAssessmentPending(t *testing.T) {
	engine := EvidenceCoachEngine{}
	decision := decisionForWindow(domain.ReviewWindow{ID: "rotation_001", Kind: "rotation_spike", Score: 0.7}, domain.CoachEvidenceQuality{Score: 0.7})
	result, err := engine.AssessDecision(context.Background(), CoachAssessmentRequest{
		Decision: decision,
		Answers:  map[string]string{"movement_was_rotation": "yes", "new_information": "unknown"},
	})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if result.Assessment != "needs_confirmation" || result.Recommendation != nil {
		t.Fatalf("incomplete assessment must remain pending: %+v", result)
	}
}

func TestEvidenceCoachAssessesConfirmedDeathContext(t *testing.T) {
	engine := EvidenceCoachEngine{}
	decision := decisionForWindow(domain.ReviewWindow{ID: "death_001", Kind: "death_review", Score: 0.98}, domain.CoachEvidenceQuality{Score: 0.8})
	if decision.ID == "" || len(decision.Questions) == 0 {
		t.Fatalf("expected death review decision: %+v", decision)
	}
	result, err := engine.AssessDecision(context.Background(), CoachAssessmentRequest{
		Decision: decision,
		Answers: map[string]string{
			"death_confirmed": "yes", "tradeable": "no", "utility_available": "no",
			"utility_used": "not_available", "crosshair_ready": "yes", "escape_route": "no",
		},
	})
	if err != nil {
		t.Fatalf("assess death: %v", err)
	}
	if result.RuleID != "combat_untradeable_contact" || result.Assessment != "validated_risk" || result.Recommendation == nil {
		t.Fatalf("unexpected death assessment: %+v", result)
	}
}

func TestEvidenceCoachTempoRuleProducesActionAndDrill(t *testing.T) {
	engine := EvidenceCoachEngine{}
	decision := decisionForWindow(domain.ReviewWindow{ID: "decision_001", Kind: "low_activity", Score: 0.7}, domain.CoachEvidenceQuality{Score: 0.7})
	result, err := engine.AssessDecision(context.Background(), CoachAssessmentRequest{
		Decision: decision,
		Answers: map[string]string{
			"window_is_hold": "yes", "purpose_clear": "no", "information_gained": "no",
			"team_aligned": "yes", "safe_space_available": "yes",
		},
	})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if result.RuleID != "tempo_unproductive_wait" || result.Recommendation == nil || result.Recommendation.Drill == "" {
		t.Fatalf("unexpected tempo result: %+v", result)
	}
}
