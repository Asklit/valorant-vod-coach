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
