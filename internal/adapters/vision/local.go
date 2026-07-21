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
	DefaultMaxReviewWindows    = 12
	MaxReviewWindowsLimit      = 20
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
		maxWindows = defaultMaxReviewWindows(request)
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
	phaseProfile := buildPhaseProfile(observations)
	roundSegments := buildRoundSegments(observations, windows, request)
	windows = assignWindowRoundNumbers(windows, roundSegments)
	summary.ReviewWindows = windows
	summary.ReviewWindowCount = len(windows)
	summary.PhaseProfile = phaseProfile
	summary.RoundSegments = roundSegments
	summary.RoundSegmentCount = len(roundSegments)
	summary.GameplayEvents = buildGameplayEvents(observations, windows, roundSegments, summary)
	summary.Coach = buildCoachSummary(request, summary, phaseProfile, windows)
	summary.FrameObservations = observations

	return GameplayResult{
		Summary:  summary,
		Findings: buildGameplayFindings(request, summary),
		Timeline: buildGameplayTimeline(windows, roundSegments),
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

func defaultMaxReviewWindows(request app.ObservationRequest) int {
	coverage := requestedCoverageSeconds(request)
	switch {
	case coverage >= 20*60:
		return 18
	case coverage >= 10*60:
		return 14
	case coverage >= 3*60:
		return 10
	default:
		return DefaultMaxReviewWindows
	}
}

func requestedCoverageSeconds(request app.ObservationRequest) float64 {
	if request.Sample.DurationSeconds > 0 {
		return request.Sample.DurationSeconds
	}
	if len(request.Sample.Frames) > 0 {
		frames := request.Sample.Frames
		return math.Max(0, frames[len(frames)-1].TimestampSeconds-frames[0].TimestampSeconds)
	}
	if request.Media.HasDuration {
		return request.Media.DurationSeconds
	}
	return 0
}

func buildPhaseProfile(observations []domain.FrameObservation) []domain.PhaseStat {
	if len(observations) == 0 {
		return nil
	}

	counts := map[string]int{}
	for _, observation := range observations {
		counts[observation.Phase]++
	}

	order := []string{"fight", "rotate", "midround", "hold"}
	stats := make([]domain.PhaseStat, 0, len(order))
	for _, phase := range order {
		count := counts[phase]
		if count == 0 {
			continue
		}
		stats = append(stats, domain.PhaseStat{
			Phase: phase,
			Count: count,
			Ratio: round4(float64(count) / float64(len(observations))),
		})
	}
	return stats
}

func buildRoundSegments(observations []domain.FrameObservation, windows []domain.ReviewWindow, request app.ObservationRequest) []domain.RoundSegment {
	if len(observations) == 0 {
		return nil
	}

	first := observations[0].TimestampSeconds
	last := observations[len(observations)-1].TimestampSeconds
	total := math.Max(0, last-first)
	roundCount := estimateRoundCount(total)
	if roundCount <= 0 {
		roundCount = 1
	}

	boundaries := make([]float64, 0, roundCount+1)
	boundaries = append(boundaries, first)

	snapQualityTotal := 0.55
	snapCount := 1
	if roundCount > 1 {
		cadence := total / float64(roundCount)
		for index := 1; index < roundCount; index++ {
			target := first + cadence*float64(index)
			boundary, quality := snapRoundBoundary(observations, target, math.Min(22, cadence*0.24))
			minBoundary := boundaries[len(boundaries)-1] + math.Min(45, cadence*0.55)
			maxBoundary := last - math.Min(30, cadence*0.38)
			if boundary < minBoundary || boundary > maxBoundary {
				boundary = target
				quality = 0.35
			}
			boundaries = append(boundaries, round3(boundary))
			snapQualityTotal += quality
			snapCount++
		}
	}
	boundaries = append(boundaries, last)

	confidence := estimatedRoundConfidence(total, request.Sample.FPSValue, snapQualityTotal/float64(snapCount))
	segments := make([]domain.RoundSegment, 0, roundCount)
	for index := 0; index < len(boundaries)-1; index++ {
		start := boundaries[index]
		end := boundaries[index+1]
		if end < start {
			end = start
		}
		segmentFrames := observationsInRange(observations, start, end, index == len(boundaries)-2)
		phaseProfile := buildPhaseProfile(segmentFrames)
		windowIDs := reviewWindowIDsInRange(windows, start, end, index == len(boundaries)-2)
		primaryPhase := dominantPhase(phaseProfile)
		summary := fmt.Sprintf("Estimated from %s visual frames. Dominant phase: %s. Review windows: %d.", formatCoverage(end-start), primaryPhase, len(windowIDs))
		segments = append(segments, domain.RoundSegment{
			RoundNumber:     index + 1,
			StartSeconds:    round3(start),
			EndSeconds:      round3(end),
			DurationSeconds: round3(end - start),
			DetectionMethod: "estimated_from_visual_timeline",
			Confidence:      confidence,
			PhaseProfile:    phaseProfile,
			ReviewWindowIDs: windowIDs,
			Summary:         summary,
		})
	}

	return segments
}

func estimateRoundCount(totalSeconds float64) int {
	switch {
	case totalSeconds <= 0:
		return 1
	case totalSeconds < 95:
		return 1
	case totalSeconds < 180:
		return 2
	}
	return min(26, max(2, int(math.Round(totalSeconds/105))))
}

func snapRoundBoundary(observations []domain.FrameObservation, target, radius float64) (float64, float64) {
	bestTimestamp := target
	bestScore := -1.0
	for _, observation := range observations {
		if math.Abs(observation.TimestampSeconds-target) > radius {
			continue
		}
		score := clamp01(1 - (observation.CombatSignal*0.62 + observation.MotionScore*0.38))
		switch observation.Phase {
		case "hold":
			score = clamp01(score + 0.08)
		case "midround":
			score = clamp01(score + 0.03)
		case "fight":
			score = clamp01(score - 0.12)
		}
		if score > bestScore {
			bestScore = score
			bestTimestamp = observation.TimestampSeconds
		}
	}
	if bestScore < 0 {
		return target, 0.35
	}
	return bestTimestamp, bestScore
}

func estimatedRoundConfidence(totalSeconds, fpsValue, snapQuality float64) float64 {
	coverageScore := clamp01(totalSeconds / (20 * 60))
	fpsScore := clamp01(fpsValue)
	confidence := 0.42 + coverageScore*0.16 + fpsScore*0.1 + clamp01(snapQuality)*0.2
	return round4(math.Min(0.72, confidence))
}

func observationsInRange(observations []domain.FrameObservation, start, end float64, includeEnd bool) []domain.FrameObservation {
	segment := make([]domain.FrameObservation, 0)
	for _, observation := range observations {
		if observation.TimestampSeconds < start {
			continue
		}
		if observation.TimestampSeconds > end || (!includeEnd && observation.TimestampSeconds == end) {
			continue
		}
		segment = append(segment, observation)
	}
	return segment
}

func reviewWindowIDsInRange(windows []domain.ReviewWindow, start, end float64, includeEnd bool) []string {
	ids := make([]string, 0)
	for _, window := range windows {
		if window.PeakSeconds < start {
			continue
		}
		if window.PeakSeconds > end || (!includeEnd && window.PeakSeconds == end) {
			continue
		}
		ids = append(ids, window.ID)
	}
	return ids
}

func dominantPhase(phases []domain.PhaseStat) string {
	if len(phases) == 0 {
		return "unknown"
	}
	best := phases[0]
	for _, phase := range phases[1:] {
		if phase.Ratio > best.Ratio {
			best = phase
		}
	}
	return best.Phase
}

func assignWindowRoundNumbers(windows []domain.ReviewWindow, segments []domain.RoundSegment) []domain.ReviewWindow {
	for windowIndex := range windows {
		for segmentIndex, segment := range segments {
			includeEnd := segmentIndex == len(segments)-1
			if windows[windowIndex].PeakSeconds < segment.StartSeconds {
				continue
			}
			if windows[windowIndex].PeakSeconds > segment.EndSeconds || (!includeEnd && windows[windowIndex].PeakSeconds == segment.EndSeconds) {
				continue
			}
			windows[windowIndex].RoundNumber = segment.RoundNumber
			break
		}
	}
	return windows
}

func buildReviewWindows(observations []domain.FrameObservation, maxWindows int) []domain.ReviewWindow {
	maxWindows = min(max(1, maxWindows), MaxReviewWindowsLimit)

	combatBudget := max(1, int(math.Ceil(float64(maxWindows)*0.5)))
	decisionBudget := 1
	rotationBudget := 0
	if maxWindows >= 8 {
		decisionBudget = max(2, int(math.Round(float64(maxWindows)*0.25)))
		rotationBudget = max(1, maxWindows-combatBudget-decisionBudget)
	}

	combat := buildHighImpactWindows(observations, combatBudget, nil)
	decision := buildPassiveWindows(observations, decisionBudget, combat)
	used := append([]domain.ReviewWindow{}, combat...)
	used = append(used, decision...)

	rotation := buildRotationWindows(observations, rotationBudget, used)
	windows := append(used, rotation...)
	if len(windows) < maxWindows {
		windows = append(windows, buildHighImpactWindows(observations, maxWindows-len(windows), windows)...)
	}

	return sortReviewWindows(windows)
}

func buildHighImpactWindows(observations []domain.FrameObservation, maxWindows int, existing []domain.ReviewWindow) []domain.ReviewWindow {
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
		if overlapsAny(existing, start, end, 6) || overlapsAny(windows, start, end, 6) {
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

func buildPassiveWindows(observations []domain.FrameObservation, maxWindows int, existing []domain.ReviewWindow) []domain.ReviewWindow {
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
		if overlapsAny(existing, first.TimestampSeconds, last.TimestampSeconds, 4) || overlapsAny(windows, first.TimestampSeconds, last.TimestampSeconds, 4) {
			continue
		}
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

func buildRotationWindows(observations []domain.FrameObservation, maxWindows int, existing []domain.ReviewWindow) []domain.ReviewWindow {
	if len(observations) == 0 || maxWindows <= 0 {
		return nil
	}

	avgMotion := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.MotionScore })
	stdMotion := stdObservation(observations, avgMotion, func(o domain.FrameObservation) float64 { return o.MotionScore })
	avgCombat := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	threshold := math.Max(0.22, avgMotion+stdMotion*0.55)

	candidates := make([]domain.FrameObservation, 0)
	for _, observation := range observations {
		if observation.MotionScore >= threshold && observation.CombatSignal <= avgCombat+0.12 {
			candidates = append(candidates, observation)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].MotionScore > candidates[j].MotionScore
	})

	windows := make([]domain.ReviewWindow, 0, maxWindows)
	for _, candidate := range candidates {
		start := math.Max(0, candidate.TimestampSeconds-6)
		end := candidate.TimestampSeconds + 8
		if overlapsAny(existing, start, end, 5) || overlapsAny(windows, start, end, 5) {
			continue
		}

		windows = append(windows, domain.ReviewWindow{
			ID:             fmt.Sprintf("rotation_%03d", len(windows)+1),
			Kind:           "rotation_spike",
			Severity:       domain.FindingSeverityLow,
			Title:          "Rotation or reposition window",
			Summary:        fmt.Sprintf("POV movement spiked at %s without matching combat intensity.", formatClock(candidate.TimestampSeconds)),
			Recommendation: "Check whether the movement was based on minimap information and whether the route preserved trade distance, sound discipline, and timing with teammates.",
			StartSeconds:   round3(start),
			EndSeconds:     round3(end),
			PeakSeconds:    candidate.TimestampSeconds,
			Score:          round4(candidate.MotionScore),
			Evidence:       []domain.EvidenceRef{evidenceForObservation(candidate)},
			Tags:           []string{"rotation", "macro", "timing"},
		})
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

	if summary.Coach != nil && len(summary.Coach.FocusAreas) > 0 {
		primary := summary.Coach.FocusAreas[0]
		findings = append(findings, domain.Finding{
			ID:             "gameplay_coach_priorities_ready",
			Severity:       domain.FindingSeverityInfo,
			Category:       "coach_summary",
			Title:          "Coach priorities generated",
			Detail:         fmt.Sprintf("Primary focus: %s. %s", primary.Title, primary.Detail),
			Recommendation: firstPracticeRecommendation(summary.Coach.PracticePlan),
			Confidence:     summary.Coach.Confidence,
			Tags:           []string{"coach", "practice-plan"},
		})
	}

	if len(summary.RoundSegments) > 0 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_round_segments_estimated",
			Severity:       domain.FindingSeverityInfo,
			Category:       "round_timeline",
			Title:          "Estimated round segments generated",
			Detail:         fmt.Sprintf("Built %d estimated round segments from sampled visual activity. These segments are for navigation and review grouping, not OCR-confirmed score or timer state.", len(summary.RoundSegments)),
			Recommendation: "Use round segments to review the match in order, then validate boundaries manually in the video player. The OCR stage should replace these estimates with timer and scoreboard-confirmed rounds.",
			Confidence:     roundSegmentConfidence(summary.RoundSegments),
			Tags:           []string{"rounds", "timeline", "estimated"},
		})
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

func buildCoachSummary(request app.ObservationRequest, summary domain.GameplaySummary, phases []domain.PhaseStat, windows []domain.ReviewWindow) *domain.CoachSummary {
	coverage := requestedCoverageSeconds(request)
	combatWindows := windowsByKind(windows, "combat_spike")
	decisionWindows := windowsByKind(windows, "low_activity")
	rotationWindows := windowsByKind(windows, "rotation_spike")

	focus := make([]domain.CoachFocusArea, 0, 5)
	if len(combatWindows) > 0 {
		focus = append(focus, domain.CoachFocusArea{
			ID:        "fight_selection",
			Priority:  priorityFromScore(maxWindowScore(combatWindows)),
			Category:  "micro",
			Title:     "Fight selection and first-contact discipline",
			Detail:    fmt.Sprintf("%d fight windows need review around crosshair placement, isolation, tradeability, and utility before contact.", len(combatWindows)),
			Score:     round4(maxWindowScore(combatWindows)),
			WindowIDs: windowIDs(combatWindows, 5),
		})
	}
	if len(decisionWindows) > 0 {
		focus = append(focus, domain.CoachFocusArea{
			ID:        "tempo_decisions",
			Priority:  priorityFromScore(0.46 + phaseRatio(phases, "hold")*0.45),
			Category:  "macro",
			Title:     "Tempo and low-activity decisions",
			Detail:    fmt.Sprintf("%d stable POV windows are useful for checking whether holds, waits, and rotations had a clear information reason.", len(decisionWindows)),
			Score:     round4(0.46 + phaseRatio(phases, "hold")*0.45),
			WindowIDs: windowIDs(decisionWindows, 4),
		})
	}
	if len(rotationWindows) > 0 {
		focus = append(focus, domain.CoachFocusArea{
			ID:        "rotation_timing",
			Priority:  priorityFromScore(0.42 + phaseRatio(phases, "rotate")*0.5),
			Category:  "macro",
			Title:     "Rotation timing and pathing",
			Detail:    fmt.Sprintf("%d movement spikes should be checked against minimap info, teammate spacing, and sound discipline.", len(rotationWindows)),
			Score:     round4(0.42 + phaseRatio(phases, "rotate")*0.5),
			WindowIDs: windowIDs(rotationWindows, 4),
		})
	}
	if summary.AverageMinimapSignal > 0 && summary.AverageMinimapSignal < 0.12 {
		focus = append(focus, domain.CoachFocusArea{
			ID:       "minimap_review_quality",
			Priority: "medium",
			Category: "capture_quality",
			Title:    "Minimap-dependent coaching is limited",
			Detail:   "The minimap region has weak signal, so macro feedback should be manually verified from the video player and contact sheet.",
			Score:    round4(1 - summary.AverageMinimapSignal),
		})
	}
	if coverage > 0 && coverage < 120 {
		focus = append(focus, domain.CoachFocusArea{
			ID:       "coverage_too_short",
			Priority: "medium",
			Category: "coverage",
			Title:    "Sample is short for full coaching",
			Detail:   "This run is useful for pipeline validation, but full-match priorities need a longer sample or full VOD mode.",
			Score:    0.7,
		})
	}

	sort.SliceStable(focus, func(i, j int) bool {
		left := priorityRank(focus[i].Priority)
		right := priorityRank(focus[j].Priority)
		if left == right {
			return focus[i].Score > focus[j].Score
		}
		return left < right
	})

	return &domain.CoachSummary{
		Verdict:         coachVerdict(coverage, summary, focus),
		Confidence:      confidenceFromCoverage(summary),
		CoverageSeconds: round3(coverage),
		FocusAreas:      focus,
		PracticePlan:    buildPracticePlan(focus),
	}
}

func coachVerdict(coverage float64, summary domain.GameplaySummary, focus []domain.CoachFocusArea) string {
	if summary.AnalyzedFrames == 0 {
		return "No visual gameplay review could be produced because the sampled frames were unreadable."
	}
	if len(focus) == 0 {
		return fmt.Sprintf("Reviewed %s of footage and found no dominant risk pattern; use selected windows for manual validation.", formatCoverage(coverage))
	}
	return fmt.Sprintf("Reviewed %s of footage. Start with %s, then validate the selected evidence windows in the video player.", formatCoverage(coverage), strings.ToLower(focus[0].Title))
}

func buildPracticePlan(focus []domain.CoachFocusArea) []domain.PracticeTask {
	tasks := make([]domain.PracticeTask, 0, min(3, len(focus)))
	for _, area := range focus {
		switch area.ID {
		case "fight_selection":
			tasks = append(tasks, domain.PracticeTask{
				ID:      "duel_review_loop",
				Title:   "Duel review loop",
				Detail:  "For each fight window, pause 3 seconds before contact and write whether the duel was isolated, tradeable, utility-supported, or avoidable.",
				Cadence: "after each VOD",
				Tags:    []string{"fight", "micro"},
			})
		case "tempo_decisions":
			tasks = append(tasks, domain.PracticeTask{
				ID:      "tempo_checkpoint",
				Title:   "Tempo checkpoint",
				Detail:  "For each low-activity window, identify the exact information that justified waiting; if none exists, choose a faster rotate, regroup, or space-taking option.",
				Cadence: "3 windows per review",
				Tags:    []string{"macro", "tempo"},
			})
		case "rotation_timing":
			tasks = append(tasks, domain.PracticeTask{
				ID:      "rotation_pathing_check",
				Title:   "Rotation pathing check",
				Detail:  "For each movement spike, compare route timing with minimap state and teammate distance; flag routes that create untradeable solo timing.",
				Cadence: "after each map side",
				Tags:    []string{"rotation", "timing"},
			})
		case "minimap_review_quality":
			tasks = append(tasks, domain.PracticeTask{
				ID:      "capture_quality_check",
				Title:   "Capture quality check",
				Detail:  "Use uncropped 1080p recordings with visible minimap, timer, score, ammo, and abilities before trusting macro or economy feedback.",
				Cadence: "before dataset runs",
				Tags:    []string{"capture", "minimap"},
			})
		case "coverage_too_short":
			tasks = append(tasks, domain.PracticeTask{
				ID:      "full_vod_pass",
				Title:   "Full VOD pass",
				Detail:  "Run a 1 fps full VOD pass before treating priorities as stable across the match.",
				Cadence: "once per VOD",
				Tags:    []string{"coverage"},
			})
		}
		if len(tasks) >= 3 {
			break
		}
	}
	return tasks
}

func buildGameplayEvents(observations []domain.FrameObservation, windows []domain.ReviewWindow, segments []domain.RoundSegment, summary domain.GameplaySummary) []domain.GameplayEvent {
	events := make([]domain.GameplayEvent, 0, len(windows)+len(segments)+3)

	for _, segment := range segments {
		events = append(events, domain.GameplayEvent{
			ID:               fmt.Sprintf("event_round_%03d", segment.RoundNumber),
			Type:             "round_estimate",
			Category:         "round_timeline",
			Severity:         domain.FindingSeverityInfo,
			Title:            fmt.Sprintf("Estimated round %d", segment.RoundNumber),
			Detail:           segment.Summary,
			Recommendation:   "Use this as navigation only until OCR confirms timer, score, and round transition state.",
			TimestampSeconds: segment.StartSeconds,
			StartSeconds:     segment.StartSeconds,
			EndSeconds:       segment.EndSeconds,
			RoundNumber:      segment.RoundNumber,
			Score:            segment.Confidence,
			Confidence:       segment.Confidence,
			Tags:             compactStrings("round", "estimated", dominantPhase(segment.PhaseProfile)),
		})
	}

	for _, window := range windows {
		eventType, category, title, detail := eventCopyForWindow(window)
		events = append(events, domain.GameplayEvent{
			ID:               "event_" + window.ID,
			Type:             eventType,
			Category:         category,
			Severity:         window.Severity,
			Title:            title,
			Detail:           detail,
			Recommendation:   window.Recommendation,
			TimestampSeconds: window.PeakSeconds,
			StartSeconds:     window.StartSeconds,
			EndSeconds:       window.EndSeconds,
			RoundNumber:      window.RoundNumber,
			Score:            window.Score,
			Confidence:       eventConfidence(window),
			Evidence:         window.Evidence,
			WindowID:         window.ID,
			Tags:             compactStrings(append([]string{"review-window", "candidate"}, window.Tags...)...),
		})
	}

	if summary.AverageMinimapSignal > 0 && summary.AverageMinimapSignal < 0.08 {
		events = append(events, domain.GameplayEvent{
			ID:               "event_capture_minimap_low",
			Type:             "capture_quality",
			Category:         "capture_quality",
			Severity:         domain.FindingSeverityMedium,
			Title:            "Minimap signal weak",
			Detail:           fmt.Sprintf("Average minimap signal is %.2f; rotation and spacing coaching should be manually verified.", summary.AverageMinimapSignal),
			Recommendation:   "Use uncropped VODs with a visible minimap before trusting macro conclusions.",
			TimestampSeconds: firstObservationTimestamp(observations),
			Score:            1 - summary.AverageMinimapSignal,
			Confidence:       0.82,
			Tags:             []string{"capture", "minimap"},
		})
	}

	if summary.AverageHUDSignal > 0 && summary.AverageHUDSignal < 0.06 {
		events = append(events, domain.GameplayEvent{
			ID:               "event_capture_hud_low",
			Type:             "capture_quality",
			Category:         "capture_quality",
			Severity:         domain.FindingSeverityMedium,
			Title:            "HUD signal weak",
			Detail:           fmt.Sprintf("Average HUD signal is %.2f; timer, score, ammo, and ability state may be hard to detect.", summary.AverageHUDSignal),
			Recommendation:   "Use full-screen recordings without overlays that cover the timer, score, ammo, minimap, or ability bar.",
			TimestampSeconds: firstObservationTimestamp(observations),
			Score:            1 - summary.AverageHUDSignal,
			Confidence:       0.82,
			Tags:             []string{"capture", "hud"},
		})
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].TimestampSeconds == events[j].TimestampSeconds {
			return events[i].ID < events[j].ID
		}
		return events[i].TimestampSeconds < events[j].TimestampSeconds
	})

	return events
}

func eventCopyForWindow(window domain.ReviewWindow) (string, string, string, string) {
	switch window.Kind {
	case "combat_spike":
		return "combat_candidate", "fight_selection", "Combat review candidate", window.Summary + " This is a fight/death candidate, not an OCR-confirmed killfeed event."
	case "rotation_spike":
		return "rotation_candidate", "rotation_timing", "Rotation review candidate", window.Summary + " Validate visible minimap and teammate spacing before treating it as a macro mistake."
	case "low_activity":
		return "tempo_candidate", "round_pacing", "Tempo review candidate", window.Summary + " Validate whether the hold gained information or only lost tempo."
	default:
		return "review_candidate", "gameplay_review", window.Title, window.Summary
	}
}

func eventConfidence(window domain.ReviewWindow) float64 {
	return round4(clamp01(0.48 + window.Score*0.45))
}

func firstObservationTimestamp(observations []domain.FrameObservation) float64 {
	if len(observations) == 0 {
		return 0
	}
	return observations[0].TimestampSeconds
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "unknown" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func buildGameplayTimeline(windows []domain.ReviewWindow, segments []domain.RoundSegment) []domain.TimelineEvent {
	timeline := make([]domain.TimelineEvent, 0, len(windows)+len(segments))
	for _, segment := range segments {
		detail := fmt.Sprintf("%s / confidence %.0f%%", formatClockRange(segment.StartSeconds, segment.EndSeconds), segment.Confidence*100)
		if len(segment.ReviewWindowIDs) > 0 {
			detail = fmt.Sprintf("%s / windows %s", detail, strings.Join(segment.ReviewWindowIDs, ", "))
		}
		timeline = append(timeline, domain.TimelineEvent{
			TimestampSeconds: segment.StartSeconds,
			Type:             "estimated_round_segment",
			Title:            fmt.Sprintf("Estimated round %d", segment.RoundNumber),
			Detail:           detail,
		})
	}
	for _, window := range windows {
		detail := fmt.Sprintf("%s / score %.2f", formatClockRange(window.StartSeconds, window.EndSeconds), window.Score)
		if window.RoundNumber > 0 {
			detail = fmt.Sprintf("round %d / %s", window.RoundNumber, detail)
		}
		timeline = append(timeline, domain.TimelineEvent{
			TimestampSeconds: window.PeakSeconds,
			Type:             "gameplay_" + window.Kind,
			Title:            window.Title,
			Detail:           detail,
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

func roundSegmentConfidence(segments []domain.RoundSegment) float64 {
	if len(segments) == 0 {
		return 0
	}
	var total float64
	for _, segment := range segments {
		total += segment.Confidence
	}
	return round4(total / float64(len(segments)))
}

func maxWindowScore(windows []domain.ReviewWindow) float64 {
	if len(windows) == 0 {
		return 0
	}
	best := windows[0].Score
	for _, window := range windows[1:] {
		best = math.Max(best, window.Score)
	}
	return best
}

func windowIDs(windows []domain.ReviewWindow, limit int) []string {
	ids := make([]string, 0, min(limit, len(windows)))
	for index, window := range windows {
		if index >= limit {
			break
		}
		ids = append(ids, window.ID)
	}
	return ids
}

func phaseRatio(phases []domain.PhaseStat, phase string) float64 {
	for _, stat := range phases {
		if stat.Phase == phase {
			return stat.Ratio
		}
	}
	return 0
}

func priorityFromScore(score float64) string {
	switch {
	case score >= 0.64:
		return "high"
	case score >= 0.42:
		return "medium"
	default:
		return "low"
	}
}

func priorityRank(priority string) int {
	switch priority {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

func firstPracticeRecommendation(tasks []domain.PracticeTask) string {
	if len(tasks) == 0 {
		return "Review the highest scoring gameplay windows and add manual notes for false positives before comparing another VOD."
	}
	return tasks[0].Detail
}

func formatCoverage(seconds float64) string {
	if seconds <= 0 {
		return "the available sample"
	}
	if seconds >= 60 {
		return fmt.Sprintf("%.1f minutes", seconds/60)
	}
	return fmt.Sprintf("%.0f seconds", seconds)
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
