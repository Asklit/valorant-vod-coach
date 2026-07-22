package app

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const defaultEvaluationTolerance = 6 * time.Second

type GameplayEvaluationRequest struct {
	RunID       string
	GeneratedAt time.Time
	Report      domain.AnalysisReport
	Annotations domain.EvaluationAnnotationSet
	Tolerance   time.Duration
}

func EvaluateGameplayEvents(request GameplayEvaluationRequest) (domain.GameplayEvaluationReport, error) {
	if request.Report.Gameplay == nil {
		return domain.GameplayEvaluationReport{}, fmt.Errorf("report does not contain gameplay summary")
	}
	if strings.TrimSpace(request.Annotations.VODLabel) != "" && request.Annotations.VODLabel != request.Report.VOD.Label {
		return domain.GameplayEvaluationReport{}, fmt.Errorf("annotation VOD %q does not match report VOD %q", request.Annotations.VODLabel, request.Report.VOD.Label)
	}

	generatedAt := request.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	runID := strings.TrimSpace(request.RunID)
	if runID == "" {
		runID = "eval_" + request.Report.RunID
	}

	tolerance := request.Tolerance
	if tolerance <= 0 && request.Annotations.ToleranceSeconds > 0 {
		tolerance = time.Duration(request.Annotations.ToleranceSeconds * float64(time.Second))
	}
	if tolerance <= 0 {
		tolerance = defaultEvaluationTolerance
	}
	toleranceSeconds := tolerance.Seconds()

	labels := append([]domain.EvaluationLabel(nil), request.Annotations.Labels...)
	sort.SliceStable(labels, func(i, j int) bool {
		if labels[i].TimestampSeconds == labels[j].TimestampSeconds {
			return labels[i].ID < labels[j].ID
		}
		return labels[i].TimestampSeconds < labels[j].TimestampSeconds
	})

	predictions := evaluatedEvents(request.Report.Gameplay.GameplayEvents, labels)
	matches, missed, falsePositive := matchEvaluationLabels(labels, predictions, toleranceSeconds)

	report := domain.GameplayEvaluationReport{
		SchemaVersion:    domain.EvaluationReportSchemaVersion,
		RunID:            runID,
		GeneratedAt:      generatedAt,
		VODLabel:         request.Report.VOD.Label,
		ReportRunID:      request.Report.RunID,
		ToleranceSeconds: round4(toleranceSeconds),
		Overall:          buildEvaluationMetrics(len(labels), len(predictions), len(matches)),
		ByType:           buildTypeMetrics(labels, predictions, matches),
		Matches:          matches,
		MissedLabels:     missed,
		FalsePositives:   falsePositive,
		Notes: []string{
			"Gameplay event evaluation matches manual labels to predicted gameplay_events within the configured timestamp tolerance.",
			"Current visual heuristic events are candidates, not OCR-confirmed kills, deaths, score changes, or round boundaries.",
		},
	}
	return report, nil
}

func evaluatedEvents(events []domain.GameplayEvent, labels []domain.EvaluationLabel) []domain.GameplayEvent {
	wanted := map[string]struct{}{}
	for _, label := range labels {
		key := labelMetricKey(label)
		if key != "" {
			wanted[key] = struct{}{}
		}
	}

	out := make([]domain.GameplayEvent, 0, len(events))
	for _, event := range events {
		if event.Type == "capture_quality" {
			continue
		}
		key := eventMetricKey(event)
		if len(wanted) > 0 {
			if _, ok := wanted[key]; !ok {
				continue
			}
		}
		out = append(out, event)
	}
	return out
}

func matchEvaluationLabels(labels []domain.EvaluationLabel, predictions []domain.GameplayEvent, toleranceSeconds float64) ([]domain.EvaluationMatch, []domain.EvaluationLabel, []domain.GameplayEvent) {
	usedPredictions := map[int]struct{}{}
	matches := make([]domain.EvaluationMatch, 0)
	missed := make([]domain.EvaluationLabel, 0)

	for _, label := range labels {
		bestIndex := -1
		bestDelta := math.MaxFloat64
		for index, event := range predictions {
			if _, used := usedPredictions[index]; used {
				continue
			}
			if !labelMatchesEvent(label, event) {
				continue
			}
			delta := math.Abs(label.TimestampSeconds - event.TimestampSeconds)
			if delta <= toleranceSeconds && delta < bestDelta {
				bestIndex = index
				bestDelta = delta
			}
		}
		if bestIndex == -1 {
			missed = append(missed, label)
			continue
		}
		usedPredictions[bestIndex] = struct{}{}
		matches = append(matches, domain.EvaluationMatch{
			Label:        label,
			Event:        predictions[bestIndex],
			DeltaSeconds: round4(bestDelta),
		})
	}

	falsePositive := make([]domain.GameplayEvent, 0)
	for index, event := range predictions {
		if _, used := usedPredictions[index]; !used {
			falsePositive = append(falsePositive, event)
		}
	}
	return matches, missed, falsePositive
}

func labelMatchesEvent(label domain.EvaluationLabel, event domain.GameplayEvent) bool {
	if strings.TrimSpace(label.Type) != "" {
		return canonicalEvaluationType(label.Type) == eventMetricKey(event)
	}
	if strings.TrimSpace(label.Category) != "" {
		return strings.EqualFold(strings.TrimSpace(label.Category), event.Category)
	}
	return false
}

func buildTypeMetrics(labels []domain.EvaluationLabel, predictions []domain.GameplayEvent, matches []domain.EvaluationMatch) []domain.EvaluationTypeMetrics {
	type counts struct {
		labels      int
		predictions int
		matches     int
	}
	byType := map[string]*counts{}
	ensure := func(key string) *counts {
		if key == "" {
			key = "unknown"
		}
		if byType[key] == nil {
			byType[key] = &counts{}
		}
		return byType[key]
	}

	for _, label := range labels {
		ensure(labelMetricKey(label)).labels++
	}
	for _, event := range predictions {
		ensure(eventMetricKey(event)).predictions++
	}
	for _, match := range matches {
		ensure(labelMetricKey(match.Label)).matches++
	}

	keys := make([]string, 0, len(byType))
	for key := range byType {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]domain.EvaluationTypeMetrics, 0, len(keys))
	for _, key := range keys {
		value := byType[key]
		out = append(out, domain.EvaluationTypeMetrics{
			Type:    key,
			Metrics: buildEvaluationMetrics(value.labels, value.predictions, value.matches),
		})
	}
	return out
}

func buildEvaluationMetrics(labels, predictions, matches int) domain.EvaluationMetrics {
	precision := ratio(matches, predictions)
	recall := ratio(matches, labels)
	f1 := 0.0
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}
	return domain.EvaluationMetrics{
		LabelCount:      labels,
		PredictionCount: predictions,
		MatchCount:      matches,
		Precision:       round4(precision),
		Recall:          round4(recall),
		F1:              round4(f1),
	}
}

func labelMetricKey(label domain.EvaluationLabel) string {
	if strings.TrimSpace(label.Type) != "" {
		return canonicalEvaluationType(label.Type)
	}
	if strings.TrimSpace(label.Category) != "" {
		return "category:" + strings.ToLower(strings.TrimSpace(label.Category))
	}
	return "unknown"
}

func eventMetricKey(event domain.GameplayEvent) string {
	return canonicalEvaluationType(event.Type)
}

func canonicalEvaluationType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "combat", "combat_spike", "combat_candidate", "fight", "fight_selection", "bad_fight", "death", "kill", "duel":
		return "combat_candidate"
	case "rotation", "rotation_spike", "rotation_candidate", "rotate", "bad_rotate", "macro_rotation":
		return "rotation_candidate"
	case "tempo", "tempo_candidate", "low_activity", "hold", "passive", "pacing":
		return "tempo_candidate"
	case "round", "round_start", "round_boundary", "round_estimate", "estimated_round":
		return "round_estimate"
	default:
		if value == "" {
			return "unknown"
		}
		return value
	}
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}
