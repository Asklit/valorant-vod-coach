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

	saved, err := app.WriteEvaluationArtifacts(context.Background(), *outRoot, evaluation, *overwrite)
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

func printEvalUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  vodctl eval run --report path --annotations path [--out-root path] [--run-id id] [--tolerance duration] [--force] [--print-json]

The eval command compares manual gameplay event labels against report.gameplay.gameplay_events.
Use it to measure precision, recall, F1, missed labels, and false positives for the current heuristic pipeline.`)
}
