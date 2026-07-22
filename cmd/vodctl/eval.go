package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	evaluationJSONName     = "evaluation.json"
	evaluationMarkdownName = "evaluation.md"
)

type savedEvaluation struct {
	JSONPath     string
	MarkdownPath string
}

func runEval(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printEvalUsage(stderr)
		return 2
	}

	switch args[0] {
	case "run":
		return runEvalRun(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printEvalUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown eval command %q\n\n", args[0])
		printEvalUsage(stderr)
		return 2
	}
}

func runEvalRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl eval run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reportPath := fs.String("report", "", "analysis report.json to evaluate")
	annotationsPath := fs.String("annotations", "", "manual gameplay event annotations JSON")
	outRoot := fs.String("out-root", filepath.Join(defaultOutRoot, "evaluations"), "root directory for evaluation artifacts")
	runID := fs.String("run-id", "", "stable evaluation run ID; defaults to eval_<report_run_id>")
	toleranceRaw := fs.String("tolerance", "", "timestamp tolerance, for example 6s; defaults to annotations or 6s")
	overwrite := fs.Bool("force", false, "overwrite existing evaluation artifacts")
	printJSON := fs.Bool("print-json", false, "print evaluation JSON to stdout instead of the summary table")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	if strings.TrimSpace(*reportPath) == "" {
		fmt.Fprintln(stderr, "--report is required")
		return 2
	}
	if strings.TrimSpace(*annotationsPath) == "" {
		fmt.Fprintln(stderr, "--annotations is required")
		return 2
	}

	var tolerance time.Duration
	if strings.TrimSpace(*toleranceRaw) != "" {
		parsed, err := parseDurationArg("--tolerance", *toleranceRaw)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if parsed <= 0 {
			fmt.Fprintln(stderr, "--tolerance must be positive")
			return 2
		}
		tolerance = parsed
	}

	var report domain.AnalysisReport
	if err := readJSONFile(*reportPath, &report); err != nil {
		fmt.Fprintf(stderr, "read report: %v\n", err)
		return 1
	}

	var annotations domain.EvaluationAnnotationSet
	if err := readJSONFile(*annotationsPath, &annotations); err != nil {
		fmt.Fprintf(stderr, "read annotations: %v\n", err)
		return 1
	}

	evaluation, err := app.EvaluateGameplayEvents(app.GameplayEvaluationRequest{
		RunID:       *runID,
		GeneratedAt: time.Now().UTC(),
		Report:      report,
		Annotations: annotations,
		Tolerance:   tolerance,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	saved, err := writeEvaluationArtifacts(context.Background(), *outRoot, evaluation, *overwrite)
	if err != nil {
		fmt.Fprintf(stderr, "write evaluation artifacts: %v\n", err)
		return 1
	}

	if *printJSON {
		raw, err := json.MarshalIndent(evaluation, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "marshal evaluation: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(raw, '\n'))
		return 0
	}

	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "RUN_ID\tVOD\tREPORT_RUN\tLABELS\tPREDICTIONS\tMATCHES\tPRECISION\tRECALL\tF1\tEVAL_JSON\tEVAL_MD")
	fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%d\t%d\t%.2f\t%.2f\t%.2f\t%s\t%s\n",
		evaluation.RunID,
		evaluation.VODLabel,
		evaluation.ReportRunID,
		evaluation.Overall.LabelCount,
		evaluation.Overall.PredictionCount,
		evaluation.Overall.MatchCount,
		evaluation.Overall.Precision,
		evaluation.Overall.Recall,
		evaluation.Overall.F1,
		saved.JSONPath,
		saved.MarkdownPath,
	)
	table.Flush()
	return 0
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return nil
}

func writeEvaluationArtifacts(ctx context.Context, outRoot string, evaluation domain.GameplayEvaluationReport, overwrite bool) (savedEvaluation, error) {
	if err := ctx.Err(); err != nil {
		return savedEvaluation{}, err
	}
	dir := filepath.Join(outRoot, safeEvalName(evaluation.RunID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return savedEvaluation{}, err
	}

	jsonPath := filepath.Join(dir, evaluationJSONName)
	markdownPath := filepath.Join(dir, evaluationMarkdownName)
	if !overwrite {
		if _, err := os.Stat(jsonPath); err == nil {
			return savedEvaluation{}, fmt.Errorf("evaluation already exists: %s", jsonPath)
		}
	}

	rawJSON, err := json.MarshalIndent(evaluation, "", "  ")
	if err != nil {
		return savedEvaluation{}, err
	}
	rawJSON = append(rawJSON, '\n')
	if err := os.WriteFile(jsonPath, rawJSON, 0o644); err != nil {
		return savedEvaluation{}, err
	}
	if err := os.WriteFile(markdownPath, renderEvaluationMarkdown(evaluation), 0o644); err != nil {
		return savedEvaluation{}, err
	}
	return savedEvaluation{JSONPath: jsonPath, MarkdownPath: markdownPath}, nil
}

func renderEvaluationMarkdown(evaluation domain.GameplayEvaluationReport) []byte {
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

func printEvalUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  vodctl eval run --report path --annotations path [--out-root path] [--run-id id] [--tolerance duration] [--force] [--print-json]

The eval command compares manual gameplay event labels against report.gameplay.gameplay_events.
Use it to measure precision, recall, F1, missed labels, and false positives for the current heuristic pipeline.`)
}
