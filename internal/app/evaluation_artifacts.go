package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	EvaluationJSONName     = "evaluation.json"
	EvaluationMarkdownName = "evaluation.md"
)

type SavedEvaluation struct {
	JSONPath     string
	MarkdownPath string
}

func WriteEvaluationArtifacts(ctx context.Context, outRoot string, evaluation domain.GameplayEvaluationReport, overwrite bool) (SavedEvaluation, error) {
	if err := ctx.Err(); err != nil {
		return SavedEvaluation{}, err
	}
	dir := filepath.Join(outRoot, safeEvalName(evaluation.RunID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SavedEvaluation{}, err
	}

	jsonPath := filepath.Join(dir, EvaluationJSONName)
	markdownPath := filepath.Join(dir, EvaluationMarkdownName)
	if !overwrite {
		if _, err := os.Stat(jsonPath); err == nil {
			return SavedEvaluation{}, fmt.Errorf("evaluation already exists: %s", jsonPath)
		}
	}

	rawJSON, err := json.MarshalIndent(evaluation, "", "  ")
	if err != nil {
		return SavedEvaluation{}, err
	}
	rawJSON = append(rawJSON, '\n')
	if err := os.WriteFile(jsonPath, rawJSON, 0o644); err != nil {
		return SavedEvaluation{}, err
	}
	if err := os.WriteFile(markdownPath, RenderEvaluationMarkdown(evaluation), 0o644); err != nil {
		return SavedEvaluation{}, err
	}
	return SavedEvaluation{JSONPath: jsonPath, MarkdownPath: markdownPath}, nil
}

func RenderEvaluationMarkdown(evaluation domain.GameplayEvaluationReport) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Gameplay Event Evaluation\n\n")
	fmt.Fprintf(&builder, "- Run: `%s`\n", evaluation.RunID)
	fmt.Fprintf(&builder, "- VOD: `%s`\n", evaluation.VODLabel)
	fmt.Fprintf(&builder, "- Report run: `%s`\n", evaluation.ReportRunID)
	fmt.Fprintf(&builder, "- Generated: `%s`\n", evaluation.GeneratedAt.UTC().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&builder, "- Tolerance: %.3fs\n\n", evaluation.ToleranceSeconds)

	fmt.Fprintf(&builder, "## Overall\n\n")
	fmt.Fprintf(&builder, "- Labels: %d\n", evaluation.Overall.LabelCount)
	fmt.Fprintf(&builder, "- Predictions: %d\n", evaluation.Overall.PredictionCount)
	fmt.Fprintf(&builder, "- Matches: %d\n", evaluation.Overall.MatchCount)
	fmt.Fprintf(&builder, "- Precision: %.2f\n", evaluation.Overall.Precision)
	fmt.Fprintf(&builder, "- Recall: %.2f\n", evaluation.Overall.Recall)
	fmt.Fprintf(&builder, "- F1: %.2f\n\n", evaluation.Overall.F1)

	if len(evaluation.ByType) > 0 {
		fmt.Fprintf(&builder, "## By Type\n\n")
		for _, item := range evaluation.ByType {
			fmt.Fprintf(&builder, "- `%s`: labels %d, predictions %d, matches %d, precision %.2f, recall %.2f, F1 %.2f\n",
				item.Type,
				item.Metrics.LabelCount,
				item.Metrics.PredictionCount,
				item.Metrics.MatchCount,
				item.Metrics.Precision,
				item.Metrics.Recall,
				item.Metrics.F1,
			)
		}
		fmt.Fprintf(&builder, "\n")
	}

	if len(evaluation.Matches) > 0 {
		fmt.Fprintf(&builder, "## Matches\n\n")
		for _, match := range evaluation.Matches {
			fmt.Fprintf(&builder, "- `%s` matched `%s` at %.3fs, delta %.3fs\n", match.Label.ID, match.Event.ID, match.Event.TimestampSeconds, match.DeltaSeconds)
		}
		fmt.Fprintf(&builder, "\n")
	}

	if len(evaluation.MissedLabels) > 0 {
		fmt.Fprintf(&builder, "## Missed Labels\n\n")
		for _, label := range evaluation.MissedLabels {
			fmt.Fprintf(&builder, "- `%s` `%s` at %.3fs", label.ID, label.Type, label.TimestampSeconds)
			if label.Description != "" {
				fmt.Fprintf(&builder, " - %s", label.Description)
			}
			fmt.Fprintf(&builder, "\n")
		}
		fmt.Fprintf(&builder, "\n")
	}

	if len(evaluation.FalsePositives) > 0 {
		fmt.Fprintf(&builder, "## False Positives\n\n")
		for _, event := range evaluation.FalsePositives {
			fmt.Fprintf(&builder, "- `%s` `%s` at %.3fs - %s\n", event.ID, event.Type, event.TimestampSeconds, event.Title)
		}
		fmt.Fprintf(&builder, "\n")
	}

	return []byte(builder.String())
}

func safeEvalName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "evaluation"
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	value = strings.Trim(builder.String(), "._-")
	if value == "" {
		return "evaluation"
	}
	return value
}
