package app

import (
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	ModelReviewPromptVersion = "vlm-review-v1"
	ModelReviewModelHint     = "qwen-vl-compatible"
	ModelReviewExpectedJSON  = `{"window_id":"string","verdict":"string","findings":[{"category":"positioning|crosshair|timing|utility|trading|rotation|tempo|capture_quality","severity":"low|medium|high|critical","timestamp_seconds":0,"evidence":"visible evidence from the clip","recommendation":"specific correction","confidence":0.0}],"practice":"one concrete drill or review habit","needs_manual_review":false}`
)

func BuildModelReviewTasks(vod domain.VOD, gameplay *domain.GameplaySummary) []domain.ModelReviewTask {
	if gameplay == nil || len(gameplay.ReviewWindows) == 0 {
		return nil
	}

	tasks := make([]domain.ModelReviewTask, 0, len(gameplay.ReviewWindows))
	for _, window := range gameplay.ReviewWindows {
		contextLines := modelReviewContext(vod, gameplay, window)
		questions := modelReviewQuestions(window)
		task := domain.ModelReviewTask{
			ID:                  "vlm_" + window.ID,
			Status:              modelReviewStatus(window),
			Priority:            modelReviewPriority(window),
			PromptVersion:       ModelReviewPromptVersion,
			ModelHint:           ModelReviewModelHint,
			WindowID:            window.ID,
			RoundNumber:         window.RoundNumber,
			Kind:                window.Kind,
			Severity:            window.Severity,
			ClipPath:            window.ClipPath,
			ClipDurationSeconds: window.ClipDurationSeconds,
			StartSeconds:        window.StartSeconds,
			EndSeconds:          window.EndSeconds,
			PeakSeconds:         window.PeakSeconds,
			Evidence:            window.Evidence,
			Context:             contextLines,
			Questions:           questions,
			ExpectedOutput:      ModelReviewExpectedJSON,
		}
		task.Prompt = buildModelReviewPrompt(task, contextLines, questions)
		tasks = append(tasks, task)
	}

	return tasks
}

func modelReviewStatus(window domain.ReviewWindow) string {
	if strings.TrimSpace(window.ClipPath) == "" {
		return "pending_clip"
	}
	return "ready"
}

func modelReviewPriority(window domain.ReviewWindow) string {
	switch window.Severity {
	case domain.FindingSeverityCritical, domain.FindingSeverityHigh:
		return "high"
	case domain.FindingSeverityMedium:
		return "medium"
	default:
		if window.Kind == "combat_spike" && window.Score >= 0.5 {
			return "medium"
		}
		return "low"
	}
}

func modelReviewContext(vod domain.VOD, gameplay *domain.GameplaySummary, window domain.ReviewWindow) []string {
	context := []string{
		fmt.Sprintf("VOD: %s", nonEmpty(vod.Title, vod.Label)),
		fmt.Sprintf("Rank label: %s", nonEmpty(string(vod.Rank), "unknown")),
		fmt.Sprintf("Window: %s from %.3fs to %.3fs, peak %.3fs, score %.2f.", window.Kind, window.StartSeconds, window.EndSeconds, window.PeakSeconds, window.Score),
		fmt.Sprintf("Heuristic summary: %s", window.Summary),
		fmt.Sprintf("Heuristic recommendation: %s", window.Recommendation),
	}
	if window.RoundNumber > 0 {
		context = append(context, fmt.Sprintf("Estimated round segment: R%d. This is not OCR-confirmed.", window.RoundNumber))
	}
	if window.ClipPath != "" {
		context = append(context, fmt.Sprintf("Review clip artifact: %s", window.ClipPath))
	}
	for _, segment := range gameplay.RoundSegments {
		if segment.RoundNumber == window.RoundNumber {
			context = append(context, fmt.Sprintf("Round context: %s Confidence %.0f%%. Method: %s.", segment.Summary, segment.Confidence*100, segment.DetectionMethod))
			break
		}
	}
	for _, focus := range matchingFocusAreas(gameplay.Coach, window.ID) {
		context = append(context, fmt.Sprintf("Coach focus: %s / %s. %s", focus.Priority, focus.Category, focus.Detail))
	}
	if len(window.Evidence) > 0 {
		evidence := make([]string, 0, len(window.Evidence))
		for _, item := range window.Evidence {
			evidence = append(evidence, fmt.Sprintf("%s at %.3fs (%s)", item.ArtifactType, item.TimestampSeconds, item.Path))
		}
		context = append(context, "Evidence frames: "+strings.Join(evidence, "; "))
	}
	return context
}

func matchingFocusAreas(coach *domain.CoachSummary, windowID string) []domain.CoachFocusArea {
	if coach == nil {
		return nil
	}
	matches := make([]domain.CoachFocusArea, 0)
	for _, focus := range coach.FocusAreas {
		for _, id := range focus.WindowIDs {
			if id == windowID {
				matches = append(matches, focus)
				break
			}
		}
	}
	return matches
}

func modelReviewQuestions(window domain.ReviewWindow) []string {
	questions := []string{
		"What concrete mistake or strong decision is visible before the peak timestamp?",
		"Was the action justified by visible HUD, minimap, teammate spacing, and objective state?",
		"What should the player do differently in the next similar round?",
	}
	switch window.Kind {
	case "combat_spike":
		questions = append(questions, "Inspect crosshair placement, angle isolation, movement before first contact, tradeability, and utility before the fight.")
	case "rotation_spike":
		questions = append(questions, "Inspect route timing, sound discipline, teammate distance, and whether the rotation follows visible information.")
	case "low_activity":
		questions = append(questions, "Inspect whether the hold had a clear purpose or lost tempo without gaining information.")
	}
	return questions
}

func buildModelReviewPrompt(task domain.ModelReviewTask, contextLines, questions []string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "You are reviewing a VALORANT first-person VOD clip for coaching.\n")
	fmt.Fprintf(&builder, "Use only visible evidence from the provided clip and metadata. Do not invent team comms, hidden enemies, economy state, or map events that are not visible.\n")
	fmt.Fprintf(&builder, "Task ID: %s\nWindow ID: %s\n", task.ID, task.WindowID)
	if task.ClipPath != "" {
		fmt.Fprintf(&builder, "Clip: %s\n", task.ClipPath)
	}
	fmt.Fprintf(&builder, "\nContext:\n")
	for _, line := range contextLines {
		fmt.Fprintf(&builder, "- %s\n", line)
	}
	fmt.Fprintf(&builder, "\nQuestions:\n")
	for _, question := range questions {
		fmt.Fprintf(&builder, "- %s\n", question)
	}
	fmt.Fprintf(&builder, "\nReturn JSON only, matching this shape:\n%s\n", task.ExpectedOutput)
	return builder.String()
}

func nonEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
