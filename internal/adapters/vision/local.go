package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	GameplayReviewArtifactName = "gameplay_review.json"
	DefaultMaxReviewWindows    = 6
)

type LocalGameplayAnalyzer struct {
	Baseline         app.ObservationAnalyzer
	MaxReviewWindows int
	ArtifactName     string
}

func (a LocalGameplayAnalyzer) AnalyzeObservations(ctx context.Context, request app.ObservationRequest) (app.ObservationResult, error) {
	baseline := a.Baseline
	if baseline == nil {
		baseline = app.BaselineObservationAnalyzer{}
	}

	result, err := baseline.AnalyzeObservations(ctx, request)
	if err != nil {
		return app.ObservationResult{}, err
	}
	result.Findings = removeFindings(result.Findings, "baseline_ai_not_enabled")

	review := AnalyzeGameplay(ctx, request, GameplayOptions{
		MaxReviewWindows: a.MaxReviewWindows,
	})
	result.Gameplay = &review.Summary
	result.Findings = append(result.Findings, review.Findings...)
	result.Timeline = append(result.Timeline, review.Timeline...)
	result.Metadata = domain.AnalysisRunMetadata{
		Analyzer: "visual-heuristic-gameplay",
		Mode:     "local",
	}

	artifact, err := a.writeArtifact(ctx, request.Sample, review.Summary)
	if err != nil {
		return app.ObservationResult{}, err
	}
	if artifact.Path != "" {
		result.Artifacts = append(result.Artifacts, artifact)
	}

	return result, nil
}

type GameplayOptions struct {
	MaxReviewWindows int
}

type GameplayResult struct {
	Summary  domain.GameplaySummary
	Findings []domain.Finding
	Timeline []domain.TimelineEvent
}

func AnalyzeGameplay(ctx context.Context, request app.ObservationRequest, options GameplayOptions) GameplayResult {
	maxWindows := options.MaxReviewWindows
	if maxWindows <= 0 {
		maxWindows = DefaultMaxReviewWindows
	}

	observations, skipped := collectFrameObservations(ctx, request.Sample.Frames)
	summary := domain.GameplaySummary{
		Analyzer:       "visual-heuristic-gameplay",
		SampledFrames:  request.Sample.FrameCount,
		AnalyzedFrames: len(observations),
		SkippedFrames:  skipped,
		Notes: []string{
			"Local visual heuristics inspect sampled frames and highlight review windows. The Qwen/VLM service can replace this analyzer later without changing report consumers.",
		},
	}

	if len(observations) == 0 {
		return GameplayResult{
			Summary: summary,
			Findings: []domain.Finding{
				{
					ID:             "gameplay_frames_unreadable",
					Severity:       domain.FindingSeverityHigh,
					Category:       "gameplay_review",
					Title:          "Gameplay frames could not be decoded",
					Detail:         "The frame sampler produced entries, but none of the image files could be decoded by the local gameplay analyzer.",
					Recommendation: "Open the frame sample artifact and verify that ffmpeg produced valid JPG files, then rerun analysis with force enabled.",
					Confidence:     1,
					Tags:           []string{"vision", "frames"},
				},
			},
		}
	}

	summary.AverageMotionScore = round4(avgObservation(observations, func(o domain.FrameObservation) float64 { return o.MotionScore }))
	summary.AverageMinimapSignal = round4(avgObservation(observations, func(o domain.FrameObservation) float64 { return o.MinimapSignal }))
	summary.AverageHUDSignal = round4(avgObservation(observations, func(o domain.FrameObservation) float64 { return o.HUDSignal }))
	summary.PeakCombatScore = round4(maxObservation(observations, func(o domain.FrameObservation) float64 { return o.CombatSignal }))

	classifyPhases(observations)
	windows := buildReviewWindows(observations, maxWindows)
	summary.ReviewWindows = windows
	summary.ReviewWindowCount = len(windows)
	summary.FrameObservations = observations

	return GameplayResult{
		Summary:  summary,
		Findings: buildGameplayFindings(request, summary),
		Timeline: buildGameplayTimeline(windows),
	}
}

type imageSignature []float64

type regionStats struct {
	brightness  float64
	contrast    float64
	edgeDensity float64
	redSignal   float64
}

func collectFrameObservations(ctx context.Context, frames []domain.Frame) ([]domain.FrameObservation, int) {
	observations := make([]domain.FrameObservation, 0, len(frames))
	skipped := 0
	var previous imageSignature

	for _, frame := range frames {
		if err := ctx.Err(); err != nil {
			break
		}

		observation, signature, err := analyzeFrame(frame, previous)
		if err != nil {
			skipped++
			continue
		}
		previous = signature
		observations = append(observations, observation)
	}

	return observations, skipped
}

func analyzeFrame(frame domain.Frame, previous imageSignature) (domain.FrameObservation, imageSignature, error) {
	file, err := os.Open(frame.Path)
	if err != nil {
		return domain.FrameObservation{}, nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return domain.FrameObservation{}, nil, err
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return domain.FrameObservation{}, nil, fmt.Errorf("empty image bounds")
	}

	global := measureRegion(img, bounds)
	center := measureRegion(img, relativeRect(bounds, 0.32, 0.25, 0.68, 0.76))
	minimap := measureRegion(img, relativeRect(bounds, 0.015, 0.025, 0.245, 0.31))
	topHUD := measureRegion(img, relativeRect(bounds, 0.34, 0.0, 0.66, 0.105))
	bottomHUD := measureRegion(img, relativeRect(bounds, 0.28, 0.82, 0.72, 0.995))

	signature := makeSignature(img, 48, 27)
	motion := motionScore(previous, signature)
	centerActivity := clamp01(center.contrast*0.48 + center.edgeDensity*0.38 + center.redSignal*0.14)
	minimapSignal := clamp01((minimap.contrast*0.56 + minimap.edgeDensity*0.44) * 1.18)
	hudSignal := clamp01(topHUD.contrast*0.34 + topHUD.edgeDensity*0.3 + bottomHUD.contrast*0.2 + bottomHUD.edgeDensity*0.16)
	redActivity := clamp01(global.redSignal*0.45 + center.redSignal*0.55)
	combatSignal := clamp01(motion*0.5 + centerActivity*0.34 + redActivity*0.16)

	return domain.FrameObservation{
		Index:            frame.Index,
		TimestampSeconds: round3(frame.TimestampSeconds),
		Path:             frame.Path,
		Brightness:       round4(global.brightness),
		Contrast:         round4(global.contrast),
		MotionScore:      round4(motion),
		CenterActivity:   round4(centerActivity),
		MinimapSignal:    round4(minimapSignal),
		HUDSignal:        round4(hudSignal),
		CombatSignal:     round4(combatSignal),
		Phase:            "unknown",
	}, signature, nil
}

func measureRegion(img image.Image, rect image.Rectangle) regionStats {
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return regionStats{}
	}

	step := max(1, min(rect.Dx(), rect.Dy())/72)
	var count, redCount int
	var sum, sumSq, edgeSum float64
	var edgePairs int

	for y := rect.Min.Y; y < rect.Max.Y; y += step {
		for x := rect.Min.X; x < rect.Max.X; x += step {
			r, g, b, _ := img.At(x, y).RGBA()
			r8 := float64(r >> 8)
			g8 := float64(g >> 8)
			b8 := float64(b >> 8)
			luma := 0.2126*r8 + 0.7152*g8 + 0.0722*b8
			sum += luma
			sumSq += luma * luma
			count++

			if r8 > 120 && r8 > g8*1.22 && r8 > b8*1.22 {
				redCount++
			}
			if x+step < rect.Max.X {
				edgeSum += math.Abs(luma-lumaAt(img, x+step, y)) / 255
				edgePairs++
			}
			if y+step < rect.Max.Y {
				edgeSum += math.Abs(luma-lumaAt(img, x, y+step)) / 255
				edgePairs++
			}
		}
	}

	if count == 0 {
		return regionStats{}
	}

	mean := sum / float64(count)
	variance := sumSq/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}

	edgeDensity := 0.0
	if edgePairs > 0 {
		edgeDensity = clamp01((edgeSum / float64(edgePairs)) * 3.2)
	}

	return regionStats{
		brightness:  clamp01(mean / 255),
		contrast:    clamp01(math.Sqrt(variance) / 92),
		edgeDensity: edgeDensity,
		redSignal:   float64(redCount) / float64(count),
	}
}

func makeSignature(img image.Image, cols, rows int) imageSignature {
	bounds := img.Bounds()
	signature := make(imageSignature, 0, cols*rows)
	for row := 0; row < rows; row++ {
		y := bounds.Min.Y + int((float64(row)+0.5)*float64(bounds.Dy())/float64(rows))
		if y >= bounds.Max.Y {
			y = bounds.Max.Y - 1
		}
		for col := 0; col < cols; col++ {
			x := bounds.Min.X + int((float64(col)+0.5)*float64(bounds.Dx())/float64(cols))
			if x >= bounds.Max.X {
				x = bounds.Max.X - 1
			}
			signature = append(signature, lumaAt(img, x, y))
		}
	}
	return signature
}

func motionScore(previous, current imageSignature) float64 {
	if len(previous) == 0 || len(previous) != len(current) {
		return 0
	}

	var diff float64
	for index := range current {
		diff += math.Abs(current[index] - previous[index])
	}
	return clamp01((diff / float64(len(current)) / 255) * 3.6)
}

func classifyPhases(observations []domain.FrameObservation) {
	avgMotion := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.MotionScore })
	avgCombat := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	stdCombat := stdObservation(observations, avgCombat, func(o domain.FrameObservation) float64 { return o.CombatSignal })

	fightThreshold := math.Max(0.32, avgCombat+stdCombat*0.72)
	rotateThreshold := math.Max(0.16, avgMotion*1.35)
	holdThreshold := math.Max(0.035, avgMotion*0.58)

	for index := range observations {
		switch {
		case observations[index].CombatSignal >= fightThreshold:
			observations[index].Phase = "fight"
		case observations[index].MotionScore >= rotateThreshold:
			observations[index].Phase = "rotate"
		case observations[index].MotionScore <= holdThreshold:
			observations[index].Phase = "hold"
		default:
			observations[index].Phase = "midround"
		}
	}
}

func buildReviewWindows(observations []domain.FrameObservation, maxWindows int) []domain.ReviewWindow {
	highImpact := buildHighImpactWindows(observations, maxWindows)
	remaining := maxWindows - len(highImpact)
	if remaining <= 0 {
		return sortReviewWindows(highImpact)
	}

	passive := buildPassiveWindows(observations, remaining)
	windows := append(highImpact, passive...)
	return sortReviewWindows(windows)
}

func buildHighImpactWindows(observations []domain.FrameObservation, maxWindows int) []domain.ReviewWindow {
	if len(observations) == 0 || maxWindows <= 0 {
		return nil
	}

	avgCombat := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	stdCombat := stdObservation(observations, avgCombat, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	threshold := math.Max(0.3, avgCombat+stdCombat*0.68)

	candidates := make([]domain.FrameObservation, 0)
	for _, observation := range observations {
		if observation.CombatSignal >= threshold {
			candidates = append(candidates, observation)
		}
	}
	if len(candidates) == 0 {
		best := observations[0]
		for _, observation := range observations[1:] {
			if observation.CombatSignal > best.CombatSignal {
				best = observation
			}
		}
		if best.CombatSignal < 0.18 {
			return nil
		}
		candidates = append(candidates, best)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CombatSignal > candidates[j].CombatSignal
	})

	windows := make([]domain.ReviewWindow, 0, maxWindows)
	for _, candidate := range candidates {
		start := math.Max(0, candidate.TimestampSeconds-8)
		end := candidate.TimestampSeconds + 10
		if overlapsAny(windows, start, end, 6) {
			continue
		}

		severity := domain.FindingSeverityMedium
		if candidate.CombatSignal >= 0.58 {
			severity = domain.FindingSeverityHigh
		}
		window := domain.ReviewWindow{
			ID:             fmt.Sprintf("combat_%03d", len(windows)+1),
			Kind:           "combat_spike",
			Severity:       severity,
			Title:          "High-impact fight window",
			Summary:        fmt.Sprintf("Visual intensity peaked at %s with motion %.2f and center activity %.2f.", formatClock(candidate.TimestampSeconds), candidate.MotionScore, candidate.CenterActivity),
			Recommendation: "Review crosshair height before contact, whether the duel was isolated or tradeable, and whether utility or repositioning was available before the fight.",
			StartSeconds:   round3(start),
			EndSeconds:     round3(end),
			PeakSeconds:    candidate.TimestampSeconds,
			Score:          round4(candidate.CombatSignal),
			Evidence:       []domain.EvidenceRef{evidenceForObservation(candidate)},
			Tags:           []string{"fight", "micro", "trade"},
		}
		windows = append(windows, window)
		if len(windows) >= maxWindows {
			break
		}
	}

	return windows
}

func buildPassiveWindows(observations []domain.FrameObservation, maxWindows int) []domain.ReviewWindow {
	if len(observations) < 2 || maxWindows <= 0 {
		return nil
	}

	avgMotion := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.MotionScore })
	threshold := math.Max(0.025, avgMotion*0.5)

	type segment struct {
		start int
		end   int
		score float64
	}
	var segments []segment
	start := -1
	for index, observation := range observations {
		isPassive := observation.MotionScore <= threshold && observation.CombatSignal < 0.32
		if isPassive && start == -1 {
			start = index
		}
		if (!isPassive || index == len(observations)-1) && start != -1 {
			end := index - 1
			if isPassive && index == len(observations)-1 {
				end = index
			}
			duration := observations[end].TimestampSeconds - observations[start].TimestampSeconds
			if duration >= 8 {
				segments = append(segments, segment{
					start: start,
					end:   end,
					score: clamp01(duration / 45),
				})
			}
			start = -1
		}
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].score > segments[j].score
	})

	windows := make([]domain.ReviewWindow, 0, min(maxWindows, len(segments)))
	for _, segment := range segments {
		first := observations[segment.start]
		last := observations[segment.end]
		peak := observations[(segment.start+segment.end)/2]
		window := domain.ReviewWindow{
			ID:             fmt.Sprintf("decision_%03d", len(windows)+1),
			Kind:           "low_activity",
			Severity:       domain.FindingSeverityLow,
			Title:          "Low-activity decision window",
			Summary:        fmt.Sprintf("The POV stayed visually stable from %s to %s.", formatClock(first.TimestampSeconds), formatClock(last.TimestampSeconds)),
			Recommendation: "Check whether the hold had a clear purpose: teammate spacing, minimap info, utility plan, fallback timing, and whether rotating would have created more value.",
			StartSeconds:   first.TimestampSeconds,
			EndSeconds:     last.TimestampSeconds,
			PeakSeconds:    peak.TimestampSeconds,
			Score:          round4(segment.score),
			Evidence:       []domain.EvidenceRef{evidenceForObservation(peak)},
			Tags:           []string{"decision", "pacing", "macro"},
		}
		windows = append(windows, window)
		if len(windows) >= maxWindows {
			break
		}
	}

	return windows
}

func buildGameplayFindings(request app.ObservationRequest, summary domain.GameplaySummary) []domain.Finding {
	findings := []domain.Finding{
		{
			ID:             "gameplay_review_ready",
			Severity:       domain.FindingSeverityInfo,
			Category:       "gameplay_review",
			Title:          "Gameplay review windows are ready",
			Detail:         fmt.Sprintf("Analyzed %d/%d sampled frames and selected %d gameplay review windows from visual motion, HUD, minimap, and center-screen signals.", summary.AnalyzedFrames, summary.SampledFrames, summary.ReviewWindowCount),
			Recommendation: "Start with the listed fight windows, then inspect low-activity windows for rotation timing, teammate spacing, and information usage.",
			Confidence:     confidenceFromCoverage(summary),
			Tags:           []string{"vision", "review-windows"},
		},
	}

	combatWindows := windowsByKind(summary.ReviewWindows, "combat_spike")
	if len(combatWindows) > 0 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_combat_windows_detected",
			Severity:       domain.FindingSeverityMedium,
			Category:       "fight_selection",
			Title:          "High-impact fight windows detected",
			Detail:         fmt.Sprintf("Detected %d high-intensity windows. Peaks: %s.", len(combatWindows), formatWindowPeaks(combatWindows, 4)),
			Recommendation: "For each peak, review the 8 seconds before contact: crosshair placement, first-shot readiness, escape route, trade setup, and whether utility should have been used before swinging.",
			Confidence:     windowConfidence(combatWindows),
			Evidence:       windowEvidence(combatWindows, 4),
			Tags:           []string{"fight", "micro"},
		})
	}

	passiveWindows := windowsByKind(summary.ReviewWindows, "low_activity")
	if len(passiveWindows) > 0 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_low_activity_windows_detected",
			Severity:       domain.FindingSeverityLow,
			Category:       "round_pacing",
			Title:          "Low-activity decision windows detected",
			Detail:         fmt.Sprintf("Detected %d stable POV windows. These are useful for checking whether the player was holding space with intent or losing tempo.", len(passiveWindows)),
			Recommendation: "Compare these windows against teammate positions and objective state: if no teammate can trade or the minimap gives new info, decide faster between holding, grouping, or rotating.",
			Confidence:     windowConfidence(passiveWindows),
			Evidence:       windowEvidence(passiveWindows, 3),
			Tags:           []string{"macro", "tempo"},
		})
	}

	if summary.AverageMinimapSignal > 0 && summary.AverageMinimapSignal < 0.08 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_minimap_signal_low",
			Severity:       domain.FindingSeverityMedium,
			Category:       "capture_quality",
			Title:          "Minimap signal is weak",
			Detail:         fmt.Sprintf("The top-left minimap region averaged %.2f signal strength. The analyzer may not reliably use map context from this VOD.", summary.AverageMinimapSignal),
			Recommendation: "Prefer uncropped 1080p gameplay with the minimap visible. For model review, minimap visibility is critical for rotation and spacing feedback.",
			Confidence:     0.82,
			Tags:           []string{"minimap", "capture"},
		})
	}

	if summary.AverageHUDSignal > 0 && summary.AverageHUDSignal < 0.06 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_hud_signal_low",
			Severity:       domain.FindingSeverityMedium,
			Category:       "capture_quality",
			Title:          "HUD signal is weak",
			Detail:         fmt.Sprintf("The top and bottom HUD regions averaged %.2f signal strength. Timer, score, weapon, and ability state may be hard to detect.", summary.AverageHUDSignal),
			Recommendation: "Use full-screen recordings without overlays that hide the timer, score, ammo, minimap, or ability bar.",
			Confidence:     0.82,
			Tags:           []string{"hud", "capture"},
		})
	}

	if request.Sample.FPSValue > 0 && request.Sample.FPSValue < 1 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_sampling_sparse_for_duels",
			Severity:       domain.FindingSeverityLow,
			Category:       "coverage",
			Title:          "Sampling is too sparse for duel mechanics",
			Detail:         fmt.Sprintf("The gameplay review ran at %.2f fps. It can find coarse windows, but it can miss short peeks, jiggle timing, and first-shot mechanics.", request.Sample.FPSValue),
			Recommendation: "Use 1 fps for full-match timeline discovery and 2 fps for focused 2-5 minute windows when evaluating duel mechanics.",
			Confidence:     1,
			Tags:           []string{"sampling", "micro"},
		})
	}

	return findings
}

func buildGameplayTimeline(windows []domain.ReviewWindow) []domain.TimelineEvent {
	timeline := make([]domain.TimelineEvent, 0, len(windows))
	for _, window := range windows {
		timeline = append(timeline, domain.TimelineEvent{
			TimestampSeconds: window.PeakSeconds,
			Type:             "gameplay_" + window.Kind,
			Title:            window.Title,
			Detail:           fmt.Sprintf("%s / score %.2f", formatClockRange(window.StartSeconds, window.EndSeconds), window.Score),
		})
	}
	return timeline
}

func (a LocalGameplayAnalyzer) writeArtifact(ctx context.Context, sample domain.FrameSampleSummary, summary domain.GameplaySummary) (domain.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return domain.Artifact{}, err
	}
	if sample.OutputDir == "" {
		return domain.Artifact{}, nil
	}

	name := strings.TrimSpace(a.ArtifactName)
	if name == "" {
		name = GameplayReviewArtifactName
	}
	path := filepath.Join(sample.OutputDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return domain.Artifact{}, err
	}

	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return domain.Artifact{}, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return domain.Artifact{}, err
	}

	return domain.Artifact{
		Type:   "gameplay_review",
		Format: "json",
		Path:   filepath.ToSlash(path),
	}, nil
}

func relativeRect(bounds image.Rectangle, left, top, right, bottom float64) image.Rectangle {
	width := bounds.Dx()
	height := bounds.Dy()
	minX := bounds.Min.X + int(left*float64(width))
	minY := bounds.Min.Y + int(top*float64(height))
	maxX := bounds.Min.X + int(right*float64(width))
	maxY := bounds.Min.Y + int(bottom*float64(height))
	if maxX <= minX {
		maxX = minX + 1
	}
	if maxY <= minY {
		maxY = minY + 1
	}
	return image.Rect(minX, minY, maxX, maxY).Intersect(bounds)
}

func lumaAt(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	r8 := float64(r >> 8)
	g8 := float64(g >> 8)
	b8 := float64(b >> 8)
	return 0.2126*r8 + 0.7152*g8 + 0.0722*b8
}

func avgObservation(observations []domain.FrameObservation, value func(domain.FrameObservation) float64) float64 {
	if len(observations) == 0 {
		return 0
	}
	var sum float64
	for _, observation := range observations {
		sum += value(observation)
	}
	return sum / float64(len(observations))
}

func maxObservation(observations []domain.FrameObservation, value func(domain.FrameObservation) float64) float64 {
	if len(observations) == 0 {
		return 0
	}
	best := value(observations[0])
	for _, observation := range observations[1:] {
		best = math.Max(best, value(observation))
	}
	return best
}

func stdObservation(observations []domain.FrameObservation, average float64, value func(domain.FrameObservation) float64) float64 {
	if len(observations) == 0 {
		return 0
	}
	var sum float64
	for _, observation := range observations {
		diff := value(observation) - average
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(observations)))
}

func overlapsAny(windows []domain.ReviewWindow, start, end, padding float64) bool {
	for _, window := range windows {
		if start <= window.EndSeconds+padding && end >= window.StartSeconds-padding {
			return true
		}
	}
	return false
}

func sortReviewWindows(windows []domain.ReviewWindow) []domain.ReviewWindow {
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].StartSeconds == windows[j].StartSeconds {
			return windows[i].Score > windows[j].Score
		}
		return windows[i].StartSeconds < windows[j].StartSeconds
	})
	for index := range windows {
		windows[index].ID = fmt.Sprintf("%s_%03d", compactKind(windows[index].Kind), index+1)
	}
	return windows
}

func windowsByKind(windows []domain.ReviewWindow, kind string) []domain.ReviewWindow {
	filtered := make([]domain.ReviewWindow, 0)
	for _, window := range windows {
		if window.Kind == kind {
			filtered = append(filtered, window)
		}
	}
	return filtered
}

func windowEvidence(windows []domain.ReviewWindow, limit int) []domain.EvidenceRef {
	var evidence []domain.EvidenceRef
	for _, window := range windows {
		evidence = append(evidence, window.Evidence...)
		if len(evidence) >= limit {
			return evidence[:limit]
		}
	}
	return evidence
}

func windowConfidence(windows []domain.ReviewWindow) float64 {
	if len(windows) == 0 {
		return 0
	}
	avgScore := 0.0
	for _, window := range windows {
		avgScore += window.Score
	}
	avgScore = avgScore / float64(len(windows))
	return round4(clamp01(0.52 + avgScore*0.42))
}

func confidenceFromCoverage(summary domain.GameplaySummary) float64 {
	if summary.SampledFrames <= 0 {
		return 0
	}
	coverage := float64(summary.AnalyzedFrames) / float64(summary.SampledFrames)
	return round4(clamp01(0.45 + coverage*0.45 + math.Min(float64(summary.ReviewWindowCount), 5)*0.02))
}

func evidenceForObservation(observation domain.FrameObservation) domain.EvidenceRef {
	return domain.EvidenceRef{
		ArtifactType:     "frame",
		Path:             observation.Path,
		TimestampSeconds: observation.TimestampSeconds,
		FrameIndex:       observation.Index,
	}
}

func formatWindowPeaks(windows []domain.ReviewWindow, limit int) string {
	values := make([]string, 0, min(limit, len(windows)))
	for index, window := range windows {
		if index >= limit {
			break
		}
		values = append(values, formatClock(window.PeakSeconds))
	}
	return strings.Join(values, ", ")
}

func formatClockRange(start, end float64) string {
	return formatClock(start) + "-" + formatClock(end)
}

func formatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(math.Round(seconds))
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func compactKind(kind string) string {
	kind = strings.TrimSpace(kind)
	kind = strings.ReplaceAll(kind, "_", "")
	if kind == "" {
		return "window"
	}
	return kind
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func round3(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func removeFindings(findings []domain.Finding, ids ...string) []domain.Finding {
	blocked := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		blocked[id] = struct{}{}
	}

	filtered := findings[:0]
	for _, finding := range findings {
		if _, ok := blocked[finding.ID]; ok {
			continue
		}
		filtered = append(filtered, finding)
	}
	return filtered
}
