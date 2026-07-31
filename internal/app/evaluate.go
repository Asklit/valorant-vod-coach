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
	if strings.TrimSpace(request.Annotations.ReportRunID) != "" && request.Annotations.ReportRunID != request.Report.RunID {
		return domain.GameplayEvaluationReport{}, fmt.Errorf("annotation report run %q does not match report run %q", request.Annotations.ReportRunID, request.Report.RunID)
	}
	if err := validateEvaluationAnnotations(request.Report, request.Annotations); err != nil {
		return domain.GameplayEvaluationReport{}, err
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

	predictions := evaluatedEvents(request.Report.Gameplay.GameplayEvents, request.Annotations)
	matches, missed, falsePositive := matchEvaluationLabels(labels, predictions, toleranceSeconds)

	report := domain.GameplayEvaluationReport{
		SchemaVersion:    domain.EvaluationReportSchemaVersion,
		RunID:            runID,
		GeneratedAt:      generatedAt,
		VODLabel:         request.Report.VOD.Label,
		ReportRunID:      request.Report.RunID,
		ToleranceSeconds: round4(toleranceSeconds),
		Overall:          buildEvaluationMetrics(len(labels), len(predictions), len(matches)),
		ByType:           buildTypeMetrics(labels, predictions, matches, request.Annotations.EvaluatedTypes),
		Matches:          matches,
		MissedLabels:     missed,
		FalsePositives:   falsePositive,
		Notes: []string{
			"Gameplay event evaluation matches manual labels to predicted gameplay_events within the configured timestamp tolerance.",
			"CPU HUD and OCR signals select evidence windows; combat and macro events remain candidates until guided context is confirmed.",
		},
	}
	return report, nil
}

func validateEvaluationAnnotations(report domain.AnalysisReport, annotations domain.EvaluationAnnotationSet) error {
	if annotations.SchemaVersion != 0 && annotations.SchemaVersion != 1 {
		return fmt.Errorf("unsupported annotation schema version %d", annotations.SchemaVersion)
	}
	if annotations.ToleranceSeconds < 0 {
		return fmt.Errorf("annotation tolerance_seconds must not be negative")
	}
	if len(annotations.Labels) == 0 && len(annotations.EvaluatedTypes) == 0 {
		return fmt.Errorf("annotations must contain labels or evaluated_types")
	}
	if annotations.Sample != nil {
		sample := annotations.Sample
		if sample.DurationSeconds <= 0 || sample.FPS <= 0 || sample.StartSeconds < 0 {
			return fmt.Errorf("annotation sample requires non-negative start_seconds and positive duration_seconds and fps")
		}
		if math.Abs(sample.StartSeconds-report.Sample.StartSeconds) > 0.001 ||
			math.Abs(sample.DurationSeconds-report.Sample.DurationSeconds) > 0.001 ||
			math.Abs(sample.FPS-report.Sample.FPSValue) > 0.001 {
			return fmt.Errorf("annotation sample start=%.3f duration=%.3f fps=%.3f does not match report sample start=%.3f duration=%.3f fps=%.3f",
				sample.StartSeconds, sample.DurationSeconds, sample.FPS,
				report.Sample.StartSeconds, report.Sample.DurationSeconds, report.Sample.FPSValue,
			)
		}
	}
	for _, evaluatedType := range annotations.EvaluatedTypes {
		if strings.TrimSpace(evaluatedType) == "" {
			return fmt.Errorf("evaluated_types must not contain an empty value")
		}
	}
	seenIDs := map[string]struct{}{}
	for index, label := range annotations.Labels {
		id := strings.TrimSpace(label.ID)
		if id == "" {
			return fmt.Errorf("annotation label %d requires an id", index)
		}
		if _, duplicate := seenIDs[id]; duplicate {
			return fmt.Errorf("annotation label id %q is duplicated", id)
		}
		seenIDs[id] = struct{}{}
		if strings.TrimSpace(label.Type) == "" && strings.TrimSpace(label.Category) == "" {
			return fmt.Errorf("annotation label %q requires a type or category", id)
		}
		if label.TimestampSeconds < 0 {
			return fmt.Errorf("annotation label %q timestamp_seconds must not be negative", id)
		}
	}
	return nil
}

func evaluatedEvents(events []domain.GameplayEvent, annotations domain.EvaluationAnnotationSet) []domain.GameplayEvent {
	wanted := map[string]struct{}{}
	for _, evaluatedType := range annotations.EvaluatedTypes {
		key := canonicalEvaluationType(evaluatedType)
		if key != "" && key != "unknown" {
			wanted[key] = struct{}{}
		}
	}
	for _, label := range annotations.Labels {
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

func buildTypeMetrics(labels []domain.EvaluationLabel, predictions []domain.GameplayEvent, matches []domain.EvaluationMatch, evaluatedTypes []string) []domain.EvaluationTypeMetrics {
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
	for _, evaluatedType := range evaluatedTypes {
		ensure(canonicalEvaluationType(evaluatedType))
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
	case "combat", "combat_spike", "combat_candidate", "fight", "fight_selection", "bad_fight", "kill", "duel":
		return "combat_candidate"
	case "death", "death_review", "death_state", "death_state_confirmed":
		return "death_state_confirmed"
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
