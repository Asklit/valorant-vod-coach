package app

import (
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestEvaluateGameplayEventsMatchesLabels(t *testing.T) {
	report := domain.AnalysisReport{
		RunID: "analysis_01",
		VOD:   domain.VOD{Label: "iron_example"},
		Gameplay: &domain.GameplaySummary{
			GameplayEvents: []domain.GameplayEvent{
				{
					ID:               "event_combat_001",
					Type:             "combat_candidate",
					Category:         "fight_selection",
					TimestampSeconds: 10,
					Title:            "Combat candidate",
				},
				{
					ID:               "event_combat_002",
					Type:             "combat_candidate",
					Category:         "fight_selection",
					TimestampSeconds: 35,
					Title:            "Combat candidate",
				},
				{
					ID:               "event_rotation_001",
					Type:             "rotation_candidate",
					Category:         "rotation_timing",
					TimestampSeconds: 70,
					Title:            "Rotation candidate",
				},
			},
		},
	}
	annotations := domain.EvaluationAnnotationSet{
		VODLabel: "iron_example",
		Labels: []domain.EvaluationLabel{
			{
				ID:               "label_fight_001",
				Type:             "death",
				TimestampSeconds: 12,
				Description:      "Bad first contact.",
			},
			{
				ID:               "label_rotation_001",
				Type:             "rotation",
				TimestampSeconds: 120,
				Description:      "Late rotation.",
			},
		},
	}

	result, err := EvaluateGameplayEvents(GameplayEvaluationRequest{
		RunID:       "eval_01",
		GeneratedAt: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		Report:      report,
		Annotations: annotations,
		Tolerance:   6 * time.Second,
	})
	if err != nil {
		t.Fatalf("evaluate gameplay events: %v", err)
	}

	if result.Overall.LabelCount != 2 || result.Overall.PredictionCount != 3 || result.Overall.MatchCount != 1 {
		t.Fatalf("unexpected overall metrics: %+v", result.Overall)
	}
	if result.Overall.Precision != 0.3333 || result.Overall.Recall != 0.5 || result.Overall.F1 != 0.4 {
		t.Fatalf("unexpected rates: %+v", result.Overall)
	}
	if len(result.Matches) != 1 || result.Matches[0].DeltaSeconds != 2 {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	if len(result.MissedLabels) != 1 || result.MissedLabels[0].ID != "label_rotation_001" {
		t.Fatalf("unexpected missed labels: %+v", result.MissedLabels)
	}
	if len(result.FalsePositives) != 2 {
		t.Fatalf("unexpected false positives: %+v", result.FalsePositives)
	}
}

func TestEvaluateGameplayEventsRejectsVODMismatch(t *testing.T) {
	_, err := EvaluateGameplayEvents(GameplayEvaluationRequest{
		Report: domain.AnalysisReport{
			VOD:      domain.VOD{Label: "vod_a"},
			Gameplay: &domain.GameplaySummary{},
		},
		Annotations: domain.EvaluationAnnotationSet{VODLabel: "vod_b"},
	})
	if err == nil {
		t.Fatalf("expected VOD mismatch error")
	}
}
