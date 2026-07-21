package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	JSONReportName     = "report.json"
	MarkdownReportName = "report.md"
)

var unsafeArtifactName = regexp.MustCompile(`[^a-zA-Z0-9_.=-]+`)

type LocalStore struct {
	ProcessedRoot string
}

func (s LocalStore) SaveReport(ctx context.Context, analysisReport domain.AnalysisReport, overwrite bool) (app.SavedReport, error) {
	if err := ctx.Err(); err != nil {
		return app.SavedReport{}, err
	}

	dir := filepath.Join(s.ProcessedRoot, analysisReport.VOD.Label, "reports", safeArtifactName(analysisReport.RunID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return app.SavedReport{}, err
	}

	jsonPath := filepath.Join(dir, JSONReportName)
	markdownPath := filepath.Join(dir, MarkdownReportName)
	if !overwrite {
		if _, err := os.Stat(jsonPath); err == nil {
			return app.SavedReport{}, fmt.Errorf("report already exists: %s", jsonPath)
		}
	}

	rawJSON, err := json.MarshalIndent(analysisReport, "", "  ")
	if err != nil {
		return app.SavedReport{}, err
	}
	rawJSON = append(rawJSON, '\n')

	if err := os.WriteFile(jsonPath, rawJSON, 0o644); err != nil {
		return app.SavedReport{}, err
	}

	if err := os.WriteFile(markdownPath, renderMarkdown(analysisReport), 0o644); err != nil {
		return app.SavedReport{}, err
	}

	return app.SavedReport{
		JSONPath:     jsonPath,
		MarkdownPath: markdownPath,
	}, nil
}

func renderMarkdown(report domain.AnalysisReport) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "# VOD Analysis Report\n\n")
	fmt.Fprintf(&buf, "- Run: `%s`\n", report.RunID)
	fmt.Fprintf(&buf, "- Status: `%s`\n", report.Status)
	fmt.Fprintf(&buf, "- Generated: `%s`\n", report.GeneratedAt.UTC().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&buf, "- Analyzer: `%s` (`%s`)\n\n", report.Metadata.Analyzer, report.Metadata.Mode)

	fmt.Fprintf(&buf, "## VOD\n\n")
	fmt.Fprintf(&buf, "- Label: `%s`\n", report.VOD.Label)
	fmt.Fprintf(&buf, "- Rank: `%s`\n", report.VOD.Rank)
	fmt.Fprintf(&buf, "- Title: %s\n", report.VOD.Title)
	fmt.Fprintf(&buf, "- Channel: %s\n", report.VOD.Channel)
	fmt.Fprintf(&buf, "- Source: %s\n\n", report.VOD.SourceURL)

	fmt.Fprintf(&buf, "## Media\n\n")
	fmt.Fprintf(&buf, "- Duration: %s\n", formatSeconds(report.Media.DurationSeconds, report.Media.HasDuration))
	fmt.Fprintf(&buf, "- Video: %s %s\n", strings.TrimSpace(report.Media.VideoCodec), formatResolution(report.Media.Width, report.Media.Height))
	fmt.Fprintf(&buf, "- Frame rate: %s\n", fallback(report.Media.FrameRate, "unknown"))
	fmt.Fprintf(&buf, "- Audio: %s\n", formatAudio(report.Media))
	fmt.Fprintf(&buf, "- Size: %s\n\n", formatSize(report.Media.SizeBytes, report.Media.HasSize))

	fmt.Fprintf(&buf, "## Frame Sample\n\n")
	fmt.Fprintf(&buf, "- Name: `%s`\n", report.Sample.Name)
	fmt.Fprintf(&buf, "- FPS: `%s`\n", report.Sample.FPS)
	fmt.Fprintf(&buf, "- Start: %.3fs\n", report.Sample.StartSeconds)
	if report.Sample.DurationSeconds > 0 {
		fmt.Fprintf(&buf, "- Duration: %.3fs\n", report.Sample.DurationSeconds)
	} else {
		fmt.Fprintf(&buf, "- Duration: full input\n")
	}
	fmt.Fprintf(&buf, "- Frames: %d\n", report.Sample.FrameCount)
	fmt.Fprintf(&buf, "- Manifest: `%s`\n", report.Sample.ManifestPath)
	if report.Sample.ContactSheetPath != "" {
		fmt.Fprintf(&buf, "- Contact sheet: `%s`\n", report.Sample.ContactSheetPath)
	}
	fmt.Fprintf(&buf, "\n")

	if report.Gameplay != nil {
		fmt.Fprintf(&buf, "## Gameplay Review\n\n")
		fmt.Fprintf(&buf, "- Analyzer: `%s`\n", fallback(report.Gameplay.Analyzer, report.Metadata.Analyzer))
		fmt.Fprintf(&buf, "- Frames analyzed: %d/%d\n", report.Gameplay.AnalyzedFrames, report.Gameplay.SampledFrames)
		if report.Gameplay.SkippedFrames > 0 {
			fmt.Fprintf(&buf, "- Frames skipped: %d\n", report.Gameplay.SkippedFrames)
		}
		fmt.Fprintf(&buf, "- Review windows: %d\n", report.Gameplay.ReviewWindowCount)
		fmt.Fprintf(&buf, "- Average motion: %.2f\n", report.Gameplay.AverageMotionScore)
		fmt.Fprintf(&buf, "- Average minimap signal: %.2f\n", report.Gameplay.AverageMinimapSignal)
		fmt.Fprintf(&buf, "- Average HUD signal: %.2f\n", report.Gameplay.AverageHUDSignal)
		fmt.Fprintf(&buf, "- Peak combat signal: %.2f\n\n", report.Gameplay.PeakCombatScore)

		if report.Gameplay.Coach != nil {
			fmt.Fprintf(&buf, "### Coach Summary\n\n")
			fmt.Fprintf(&buf, "- Verdict: %s\n", report.Gameplay.Coach.Verdict)
			fmt.Fprintf(&buf, "- Confidence: %.2f\n", report.Gameplay.Coach.Confidence)
			if report.Gameplay.Coach.CoverageSeconds > 0 {
				fmt.Fprintf(&buf, "- Coverage: %.3fs\n", report.Gameplay.Coach.CoverageSeconds)
			}
			if len(report.Gameplay.Coach.FocusAreas) > 0 {
				fmt.Fprintf(&buf, "\n#### Focus Areas\n\n")
				for _, area := range report.Gameplay.Coach.FocusAreas {
					fmt.Fprintf(&buf, "- `%s` `%s`: %s - %s", area.Priority, area.Category, area.Title, area.Detail)
					if len(area.WindowIDs) > 0 {
						fmt.Fprintf(&buf, " Windows: `%s`", strings.Join(area.WindowIDs, "`, `"))
					}
					fmt.Fprintf(&buf, "\n")
				}
			}
			if len(report.Gameplay.Coach.PracticePlan) > 0 {
				fmt.Fprintf(&buf, "\n#### Practice Plan\n\n")
				for _, task := range report.Gameplay.Coach.PracticePlan {
					fmt.Fprintf(&buf, "- %s (`%s`): %s\n", task.Title, task.Cadence, task.Detail)
				}
			}
			fmt.Fprintf(&buf, "\n")
		}

		if len(report.Gameplay.PhaseProfile) > 0 {
			fmt.Fprintf(&buf, "### Phase Profile\n\n")
			for _, phase := range report.Gameplay.PhaseProfile {
				fmt.Fprintf(&buf, "- `%s`: %d frames (%.0f%%)\n", phase.Phase, phase.Count, phase.Ratio*100)
			}
			fmt.Fprintf(&buf, "\n")
		}

		if len(report.Gameplay.RoundSegments) > 0 {
			fmt.Fprintf(&buf, "### Estimated Round Segments\n\n")
			for _, segment := range report.Gameplay.RoundSegments {
				fmt.Fprintf(&buf, "- Round %d: %.3fs - %.3fs", segment.RoundNumber, segment.StartSeconds, segment.EndSeconds)
				if segment.DurationSeconds > 0 {
					fmt.Fprintf(&buf, " (%.3fs)", segment.DurationSeconds)
				}
				if segment.Confidence > 0 {
					fmt.Fprintf(&buf, " confidence %.0f%%", segment.Confidence*100)
				}
				if len(segment.ReviewWindowIDs) > 0 {
					fmt.Fprintf(&buf, " windows `%s`", strings.Join(segment.ReviewWindowIDs, "`, `"))
				}
				if segment.Summary != "" {
					fmt.Fprintf(&buf, " - %s", segment.Summary)
				}
				fmt.Fprintf(&buf, "\n")
			}
			fmt.Fprintf(&buf, "\n")
		}

		if len(report.Gameplay.ModelReviewTasks) > 0 {
			fmt.Fprintf(&buf, "### Model Review Tasks\n\n")
			for _, task := range report.Gameplay.ModelReviewTasks {
				fmt.Fprintf(&buf, "- `%s`: %s priority `%s`, window `%s`", task.ID, task.Status, task.Priority, task.WindowID)
				if task.RoundNumber > 0 {
					fmt.Fprintf(&buf, ", estimated round %d", task.RoundNumber)
				}
				if task.ClipPath != "" {
					fmt.Fprintf(&buf, ", clip `%s`", task.ClipPath)
				}
				fmt.Fprintf(&buf, ", prompt `%s`\n", task.PromptVersion)
			}
			fmt.Fprintf(&buf, "\n")
		}

		if len(report.Gameplay.ModelReviewRuns) > 0 {
			fmt.Fprintf(&buf, "### Model Review Results\n\n")
			for _, run := range report.Gameplay.ModelReviewRuns {
				fmt.Fprintf(&buf, "- `%s`: %s window `%s`", run.ID, run.Status, run.WindowID)
				if run.Model != "" {
					fmt.Fprintf(&buf, ", model `%s`", run.Model)
				}
				if run.Verdict != "" {
					fmt.Fprintf(&buf, " - %s", run.Verdict)
				}
				fmt.Fprintf(&buf, "\n")
			}
			fmt.Fprintf(&buf, "\n")
		}

		if len(report.Gameplay.ReviewWindows) > 0 {
			for _, window := range report.Gameplay.ReviewWindows {
				fmt.Fprintf(&buf, "### %s: %s\n\n", strings.ToUpper(string(window.Severity)), window.Title)
				fmt.Fprintf(&buf, "- ID: `%s`\n", window.ID)
				fmt.Fprintf(&buf, "- Kind: `%s`\n", window.Kind)
				if window.RoundNumber > 0 {
					fmt.Fprintf(&buf, "- Estimated round: %d\n", window.RoundNumber)
				}
				fmt.Fprintf(&buf, "- Window: %.3fs - %.3fs (peak %.3fs)\n", window.StartSeconds, window.EndSeconds, window.PeakSeconds)
				fmt.Fprintf(&buf, "- Score: %.2f\n", window.Score)
				if window.ClipPath != "" {
					fmt.Fprintf(&buf, "- Clip: `%s`", window.ClipPath)
					if window.ClipDurationSeconds > 0 {
						fmt.Fprintf(&buf, " (%.3fs)", window.ClipDurationSeconds)
					}
					fmt.Fprintf(&buf, "\n")
				}
				fmt.Fprintf(&buf, "- Summary: %s\n", window.Summary)
				fmt.Fprintf(&buf, "- Recommendation: %s\n", window.Recommendation)
				if len(window.Evidence) > 0 {
					fmt.Fprintf(&buf, "- Evidence:")
					for _, evidence := range window.Evidence {
						fmt.Fprintf(&buf, " `%s`", evidence.Path)
					}
					fmt.Fprintf(&buf, "\n")
				}
				fmt.Fprintf(&buf, "\n")
			}
		}
	}

	fmt.Fprintf(&buf, "## Findings\n\n")
	if len(report.Findings) == 0 {
		fmt.Fprintf(&buf, "No findings.\n\n")
	} else {
		for _, finding := range report.Findings {
			fmt.Fprintf(&buf, "### %s: %s\n\n", strings.ToUpper(string(finding.Severity)), finding.Title)
			fmt.Fprintf(&buf, "- ID: `%s`\n", finding.ID)
			fmt.Fprintf(&buf, "- Category: `%s`\n", finding.Category)
			fmt.Fprintf(&buf, "- Detail: %s\n", finding.Detail)
			if finding.Recommendation != "" {
				fmt.Fprintf(&buf, "- Recommendation: %s\n", finding.Recommendation)
			}
			if finding.Confidence > 0 {
				fmt.Fprintf(&buf, "- Confidence: %.2f\n", finding.Confidence)
			}
			if len(finding.Evidence) > 0 {
				fmt.Fprintf(&buf, "- Evidence:")
				for _, evidence := range finding.Evidence {
					fmt.Fprintf(&buf, " `%s`", evidence.Path)
				}
				fmt.Fprintf(&buf, "\n")
			}
			fmt.Fprintf(&buf, "\n")
		}
	}

	fmt.Fprintf(&buf, "## Timeline\n\n")
	if len(report.Timeline) == 0 {
		fmt.Fprintf(&buf, "No timeline events.\n\n")
	} else {
		for _, event := range report.Timeline {
			fmt.Fprintf(&buf, "- %.3fs `%s`: %s", event.TimestampSeconds, event.Type, event.Title)
			if event.Detail != "" {
				fmt.Fprintf(&buf, " - %s", event.Detail)
			}
			fmt.Fprintf(&buf, "\n")
		}
		fmt.Fprintf(&buf, "\n")
	}

	fmt.Fprintf(&buf, "## Artifacts\n\n")
	for _, artifact := range report.Artifacts {
		fmt.Fprintf(&buf, "- `%s` `%s`: `%s`\n", artifact.Type, artifact.Format, artifact.Path)
	}

	return buf.Bytes()
}

func safeArtifactName(value string) string {
	value = strings.TrimSpace(value)
	value = unsafeArtifactName.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "analysis"
	}
	return value
}

func formatResolution(width, height int) string {
	if width <= 0 || height <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%dx%d", width, height)
}

func formatAudio(media domain.MediaSummary) string {
	if !media.HasAudio {
		return "none"
	}
	return fallback(media.AudioCodec, "unknown")
}

func formatSeconds(seconds float64, ok bool) string {
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%.3fs", seconds)
}

func formatSize(size int64, ok bool) string {
	if !ok {
		return "unknown"
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value = value / unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func fallback(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
