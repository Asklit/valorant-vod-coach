package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	GameplayAnalyzerName       = "valorant-hud-cv-v2"
	GameplayReviewArtifactName = "gameplay_review.json"
	DefaultMaxReviewWindows    = 12
	MaxReviewWindowsLimit      = 20
	trustedBuyPhaseSignal      = 0.90
	trustedScoreboardSignal    = 0.80
	trustedCombatReportSignal  = 0.90
)

type LocalGameplayAnalyzer struct {
	Baseline         app.ObservationAnalyzer
	MaxReviewWindows int
	ArtifactName     string
	TesseractPath    string
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
		TesseractPath:    a.TesseractPath,
	})
	result.Gameplay = &review.Summary
	result.Findings = append(result.Findings, review.Findings...)
	result.Timeline = append(result.Timeline, review.Timeline...)
	result.Metadata = domain.AnalysisRunMetadata{
		Analyzer: GameplayAnalyzerName,
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
	TesseractPath    string
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
		Analyzer:       GameplayAnalyzerName,
		SampledFrames:  request.Sample.FrameCount,
		AnalyzedFrames: len(observations),
		SkippedFrames:  skipped,
		Notes: []string{
			"The local CPU analyzer validates the VALORANT HUD layout and uses corroborated killfeed, damage, death-state, scoreboard, and buy-phase evidence to select review windows.",
			"Candidate windows remain observations rather than coaching conclusions until the guided visible-context rubric is complete.",
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
	ocrStatus, ocrAnalyzedFrames := enrichFrameObservationsWithOCR(ctx, observations, options.TesseractPath)

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
	summary.Understanding = buildGameplayUnderstanding(observations, windows, roundSegments, ocrStatus, ocrAnalyzedFrames)
	summary.GameplayEvents = buildGameplayEvents(observations, windows, roundSegments, summary)
	summary.FrameObservations = observations

	return GameplayResult{
		Summary:  summary,
		Findings: buildGameplayFindings(request, summary),
		Timeline: buildGameplayTimeline(windows, roundSegments),
	}
}

func classifyPhases(observations []domain.FrameObservation) {
	avgMotion := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.MotionScore })
	avgCombat := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	stdCombat := stdObservation(observations, avgCombat, func(o domain.FrameObservation) float64 { return o.CombatSignal })

	fightThreshold := math.Max(0.24, avgCombat+stdCombat*0.72)
	rotateThreshold := math.Max(0.16, avgMotion*1.35)
	holdThreshold := math.Max(0.035, avgMotion*0.58)

	for index := range observations {
		switch {
		case observations[index].BuyPhaseSignal >= trustedBuyPhaseSignal:
			observations[index].Phase = "buy"
		case observations[index].CombatReportSignal >= trustedCombatReportSignal:
			observations[index].Phase = "death"
		case observations[index].ScoreboardSignal >= trustedScoreboardSignal:
			observations[index].Phase = "scoreboard"
		case observations[index].RoundEndSignal >= 0.90:
			observations[index].Phase = "round_end"
		case observations[index].CombatSignal >= fightThreshold && hasCombatCorroboration(observations[index]):
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

	order := []string{"buy", "fight", "death", "scoreboard", "round_end", "rotate", "midround", "hold"}
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
	buyAnchors := detectBuyPhaseAnchors(observations)
	if len(buyAnchors) >= 2 {
		boundaries := []float64{first}
		for _, anchor := range buyAnchors {
			if anchor.TimestampSeconds-first <= 12 || anchor.TimestampSeconds-boundaries[len(boundaries)-1] < 35 || last-anchor.TimestampSeconds < 20 {
				continue
			}
			boundaries = append(boundaries, anchor.TimestampSeconds)
		}
		boundaries = append(boundaries, last)
		if len(boundaries) >= 3 {
			anchorQuality := avgObservation(buyAnchors, func(o domain.FrameObservation) float64 { return o.BuyPhaseSignal })
			confidence := round4(math.Min(0.88, 0.61+anchorQuality*0.21+clamp01(float64(len(buyAnchors))/12)*0.06))
			return roundSegmentsFromBoundaries(observations, windows, boundaries, "buy_phase_visual_anchor", confidence)
		}
	}

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
	return roundSegmentsFromBoundaries(observations, windows, boundaries, "estimated_from_visual_timeline", confidence)
}

func roundSegmentsFromBoundaries(observations []domain.FrameObservation, windows []domain.ReviewWindow, boundaries []float64, method string, confidence float64) []domain.RoundSegment {
	segments := make([]domain.RoundSegment, 0, len(boundaries)-1)
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
		summary := fmt.Sprintf("Detected from %s visual frames using %s. Dominant phase: %s. Review windows: %d.", formatCoverage(end-start), strings.ReplaceAll(method, "_", " "), primaryPhase, len(windowIDs))
		segments = append(segments, domain.RoundSegment{
			RoundNumber:     index + 1,
			StartSeconds:    round3(start),
			EndSeconds:      round3(end),
			DurationSeconds: round3(end - start),
			DetectionMethod: method,
			Confidence:      confidence,
			PhaseProfile:    phaseProfile,
			ReviewWindowIDs: windowIDs,
			Summary:         summary,
		})
	}

	return segments
}

func detectBuyPhaseAnchors(observations []domain.FrameObservation) []domain.FrameObservation {
	anchors := make([]domain.FrameObservation, 0)
	clusterStart := -1
	flushCluster := func(clusterEnd int) {
		if clusterStart == -1 || clusterEnd < clusterStart {
			return
		}
		best := observations[clusterStart]
		for candidate := clusterStart + 1; candidate <= clusterEnd; candidate++ {
			if observations[candidate].BuyPhaseSignal > best.BuyPhaseSignal {
				best = observations[candidate]
			}
		}
		if clusterEnd-clusterStart >= 1 || best.BuyPhaseSignal >= 0.95 {
			anchor := observations[clusterStart]
			anchor.BuyPhaseSignal = best.BuyPhaseSignal
			anchors = append(anchors, anchor)
		}
		clusterStart = -1
	}
	for index, observation := range observations {
		isBuy := observation.BuyPhaseSignal >= trustedBuyPhaseSignal && observation.HUDLayoutConfidence >= 0.24
		if isBuy {
			if clusterStart != -1 && index > 0 && observation.TimestampSeconds-observations[index-1].TimestampSeconds > 12 {
				flushCluster(index - 1)
			}
			if clusterStart == -1 {
				clusterStart = index
			}
			continue
		}
		flushCluster(index - 1)
	}
	flushCluster(len(observations) - 1)
	return anchors
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
	deathBudget := max(1, int(math.Ceil(float64(maxWindows)*0.34)))
	combatBudget := max(1, int(math.Ceil(float64(maxWindows)*0.40)))
	decisionBudget := 1
	if maxWindows >= 8 {
		decisionBudget = max(2, int(math.Round(float64(maxWindows)*0.16)))
	}

	windows := buildDeathReviewWindows(observations, min(maxWindows, deathBudget))
	remaining := maxWindows - len(windows)
	if remaining > 0 {
		windows = append(windows, buildHighImpactWindows(observations, min(remaining, combatBudget), windows)...)
	}
	remaining = maxWindows - len(windows)
	if remaining > 0 {
		windows = append(windows, buildPassiveWindows(observations, min(remaining, decisionBudget), windows)...)
	}
	remaining = maxWindows - len(windows)
	if remaining > 0 {
		windows = append(windows, buildRotationWindows(observations, remaining, windows)...)
	}

	return sortReviewWindows(windows)
}

func buildDeathReviewWindows(observations []domain.FrameObservation, maxWindows int) []domain.ReviewWindow {
	if len(observations) == 0 || maxWindows <= 0 {
		return nil
	}

	type deathCluster struct {
		start int
		end   int
		peak  int
	}
	clusters := make([]deathCluster, 0)
	clusterStart := -1
	peak := -1
	for index, observation := range observations {
		visible := observation.CombatReportSignal >= trustedCombatReportSignal &&
			observation.HUDLayoutConfidence >= 0.22 &&
			observation.BuyPhaseSignal < trustedBuyPhaseSignal &&
			observation.ScoreboardSignal < trustedScoreboardSignal &&
			observation.RoundEndSignal < 0.90
		if visible {
			if clusterStart == -1 {
				clusterStart = index
				peak = index
			}
			if observation.CombatReportSignal > observations[peak].CombatReportSignal && observation.TimestampSeconds-observations[clusterStart].TimestampSeconds <= 8 {
				peak = index
			}
		}
		if (!visible || index == len(observations)-1) && clusterStart != -1 {
			end := index - 1
			if visible && index == len(observations)-1 {
				end = index
			}
			clusters = append(clusters, deathCluster{start: clusterStart, end: end, peak: peak})
			clusterStart = -1
			peak = -1
		}
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		return observations[clusters[i].peak].CombatReportSignal > observations[clusters[j].peak].CombatReportSignal
	})
	windows := make([]domain.ReviewWindow, 0, min(maxWindows, len(clusters)))
	for _, cluster := range clusters {
		candidate := observations[cluster.peak]
		start := math.Max(0, candidate.TimestampSeconds-10)
		end := candidate.TimestampSeconds + 8
		if overlapsAny(windows, start, end, 5) {
			continue
		}
		windows = append(windows, domain.ReviewWindow{
			ID:             fmt.Sprintf("death_%03d", len(windows)+1),
			Kind:           "death_review",
			Severity:       domain.FindingSeverityMedium,
			Title:          "Death context review",
			Summary:        fmt.Sprintf("The VALORANT combat-report panel became visible around %s. Review the decisions immediately before the death state.", formatClock(candidate.TimestampSeconds)),
			Recommendation: "Use the before/event/after evidence and confirm first-contact intent, tradeability, utility sequence, crosshair readiness, and escape options.",
			StartSeconds:   round3(start),
			EndSeconds:     round3(end),
			PeakSeconds:    candidate.TimestampSeconds,
			Score:          round4(candidate.CombatReportSignal),
			Evidence:       evidenceSequence(observations, cluster.peak, 5, 3),
			Tags:           []string{"death", "fight", "decision"},
		})
		if len(windows) >= maxWindows {
			break
		}
	}
	return windows
}

func buildHighImpactWindows(observations []domain.FrameObservation, maxWindows int, existing []domain.ReviewWindow) []domain.ReviewWindow {
	if len(observations) == 0 || maxWindows <= 0 {
		return nil
	}

	avgCombat := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	stdCombat := stdObservation(observations, avgCombat, func(o domain.FrameObservation) float64 { return o.CombatSignal })
	threshold := math.Max(0.20, avgCombat+stdCombat*0.68)

	type indexedObservation struct {
		index       int
		observation domain.FrameObservation
	}
	candidates := make([]indexedObservation, 0)
	for index, observation := range observations {
		if observation.CombatSignal >= threshold && observation.HUDLayoutConfidence >= 0.22 && hasCombatCorroboration(observation) && observation.ScoreboardSignal < 0.48 && observation.BuyPhaseSignal < 0.48 && observation.CombatReportSignal < 0.58 && observation.RoundEndSignal < 0.48 {
			candidates = append(candidates, indexedObservation{index: index, observation: observation})
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].observation.CombatSignal > candidates[j].observation.CombatSignal
	})

	windows := make([]domain.ReviewWindow, 0, maxWindows)
	for _, indexed := range candidates {
		candidate := indexed.observation
		start := math.Max(0, candidate.TimestampSeconds-8)
		end := candidate.TimestampSeconds + 10
		if overlapsAny(existing, start, end, 0) || overlapsAny(windows, start, end, 6) {
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
			Summary:        fmt.Sprintf("A corroborated fight event peaked at %s (killfeed %.2f, damage %.2f, center activity %.2f).", formatClock(candidate.TimestampSeconds), candidate.KillfeedEventSignal, candidate.DamageSignal, candidate.CenterActivity),
			Recommendation: "Confirm that a fight occurred, then complete the visible-context rubric before treating this window as a coaching finding.",
			StartSeconds:   round3(start),
			EndSeconds:     round3(end),
			PeakSeconds:    candidate.TimestampSeconds,
			Score:          round4(candidate.CombatSignal),
			Evidence:       evidenceSequence(observations, indexed.index, 4, 3),
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
		isPassive := observation.MotionScore <= threshold && observation.CombatSignal < 0.24 && observation.HUDLayoutConfidence >= 0.22 && observation.BuyPhaseSignal < 0.42 && observation.ScoreboardSignal < 0.42 && observation.CombatReportSignal < 0.42 && observation.RoundEndSignal < 0.42
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
		peakIndex := (segment.start + segment.end) / 2
		peak := observations[peakIndex]
		window := domain.ReviewWindow{
			ID:             fmt.Sprintf("decision_%03d", len(windows)+1),
			Kind:           "low_activity",
			Severity:       domain.FindingSeverityLow,
			Title:          "Low-activity decision window",
			Summary:        fmt.Sprintf("The POV stayed visually stable from %s to %s.", formatClock(first.TimestampSeconds), formatClock(last.TimestampSeconds)),
			Recommendation: "Confirm that this is active gameplay, then record the purpose, information gain, team timing, and available safe space.",
			StartSeconds:   first.TimestampSeconds,
			EndSeconds:     last.TimestampSeconds,
			PeakSeconds:    peak.TimestampSeconds,
			Score:          round4(segment.score),
			Evidence:       evidenceSequence(observations, peakIndex, 4, 4),
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

	type indexedObservation struct {
		index       int
		observation domain.FrameObservation
	}
	candidates := make([]indexedObservation, 0)
	for index, observation := range observations {
		if observation.MotionScore >= threshold && observation.CombatSignal <= avgCombat+0.12 && observation.HUDLayoutConfidence >= 0.22 && observation.BuyPhaseSignal < 0.42 && observation.ScoreboardSignal < 0.42 && observation.CombatReportSignal < 0.42 && observation.RoundEndSignal < 0.42 && !hasNearbyBlockingOverlay(observations, index, 4) {
			candidates = append(candidates, indexedObservation{index: index, observation: observation})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].observation.MotionScore > candidates[j].observation.MotionScore
	})

	windows := make([]domain.ReviewWindow, 0, maxWindows)
	for _, indexed := range candidates {
		candidate := indexed.observation
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
			Recommendation: "Confirm that this is a rotation, then record its information trigger, objective pressure, teammate spacing, and sound discipline.",
			StartSeconds:   round3(start),
			EndSeconds:     round3(end),
			PeakSeconds:    candidate.TimestampSeconds,
			Score:          round4(candidate.MotionScore),
			Evidence:       evidenceSequence(observations, indexed.index, 4, 3),
			Tags:           []string{"rotation", "macro", "timing"},
		})
		if len(windows) >= maxWindows {
			break
		}
	}
	return windows
}

func hasNearbyBlockingOverlay(observations []domain.FrameObservation, index int, radiusSeconds float64) bool {
	if index < 0 || index >= len(observations) {
		return false
	}
	timestamp := observations[index].TimestampSeconds
	for _, observation := range observations {
		if math.Abs(observation.TimestampSeconds-timestamp) > radiusSeconds {
			continue
		}
		if observation.BuyPhaseSignal >= trustedBuyPhaseSignal ||
			observation.ScoreboardSignal >= trustedScoreboardSignal ||
			observation.CombatReportSignal >= trustedCombatReportSignal ||
			observation.RoundEndSignal >= 0.90 {
			return true
		}
	}
	return false
}

func buildGameplayFindings(request app.ObservationRequest, summary domain.GameplaySummary) []domain.Finding {
	findings := []domain.Finding{
		{
			ID:             "gameplay_review_ready",
			Severity:       domain.FindingSeverityInfo,
			Category:       "gameplay_review",
			Title:          "Gameplay review windows are ready",
			Detail:         fmt.Sprintf("Analyzed %d/%d sampled frames and selected %d evidence windows from VALORANT HUD layout, temporal killfeed, damage, combat-report, scoreboard, and buy-phase signals.", summary.AnalyzedFrames, summary.SampledFrames, summary.ReviewWindowCount),
			Recommendation: "Open the guided review queue and confirm visible context. Candidate windows are not gameplay mistakes until the evidence rubric is complete.",
			Confidence:     confidenceFromCoverage(summary),
			Tags:           []string{"vision", "review-windows"},
		},
	}

	if len(summary.RoundSegments) > 0 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_round_segments_detected",
			Severity:       domain.FindingSeverityInfo,
			Category:       "round_timeline",
			Title:          "Round navigation segments generated",
			Detail:         fmt.Sprintf("Built %d round segments using %s. Buy-phase OCR anchors are preferred; cadence estimation is retained as an explicit fallback.", len(summary.RoundSegments), strings.ReplaceAll(summary.RoundSegments[0].DetectionMethod, "_", " ")),
			Recommendation: "Use round segments for navigation and review grouping. Treat cadence-based boundaries as estimates when buy-phase OCR anchors are unavailable.",
			Confidence:     roundSegmentConfidence(summary.RoundSegments),
			Tags:           []string{"rounds", "timeline", "estimated"},
		})
	}

	deathWindows := windowsByKind(summary.ReviewWindows, "death_review")
	if len(deathWindows) > 0 {
		findings = append(findings, domain.Finding{
			ID:             "gameplay_death_reviews_detected",
			Severity:       domain.FindingSeverityMedium,
			Category:       "death_review",
			Title:          "Death review moments detected",
			Detail:         fmt.Sprintf("Detected %d distinct combat-report transitions. Each window includes evidence before, at, and after the death state.", len(deathWindows)),
			Recommendation: "Complete the visible-context rubric for each death. Advice is generated only after tradeability, utility, crosshair, and fallback facts are confirmed.",
			Confidence:     windowConfidence(deathWindows),
			Evidence:       windowEvidence(deathWindows, 6),
			Tags:           []string{"death", "evidence-sequence"},
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
			Recommendation: "Use each peak as navigation only. Confirm the fight outcome and visible decision context in the guided rubric before generating advice.",
			Confidence:     windowConfidence(combatWindows),
			Evidence:       windowEvidence(combatWindows, 4),
			Tags:           []string{"fight", "micro"},
		})
	}

	if summary.Understanding != nil && summary.Understanding.CaptureCompatibility != "supported" {
		severity := domain.FindingSeverityMedium
		if summary.Understanding.CaptureCompatibility == "unsupported" {
			severity = domain.FindingSeverityHigh
		}
		findings = append(findings, domain.Finding{
			ID:             "gameplay_capture_compatibility_" + summary.Understanding.CaptureCompatibility,
			Severity:       severity,
			Category:       "capture_quality",
			Title:          "Recording compatibility is " + summary.Understanding.CaptureCompatibility,
			Detail:         fmt.Sprintf("VALORANT HUD layout confidence is %.0f%%. Cropping, overlays, resolution, or a non-gameplay video can reduce event detection quality.", summary.Understanding.CompatibilityConfidence*100),
			Recommendation: "Upload uncropped 16:9 gameplay with the minimap, team bar, timer, health, abilities, and ammo visible.",
			Confidence:     summary.Understanding.CompatibilityConfidence,
			Tags:           []string{"compatibility", "hud", summary.Understanding.CaptureCompatibility},
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
			Recommendation: "Use the guided rubric to exclude buy phases and edits, then confirm purpose, information gain, team alignment, and safer available actions.",
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
	case "death_review":
		return "death_state_confirmed", "death_review", "Death review", window.Summary + " The combat-report UI corroborates the death state; decision quality still depends on guided context."
	case "combat_spike":
		return "combat_candidate", "fight_selection", "Combat review candidate", window.Summary + " Killfeed or damage evidence corroborates contact; decision quality still depends on guided context."
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

func hasCombatCorroboration(observation domain.FrameObservation) bool {
	return observation.KillfeedEventSignal >= 0.18 || observation.DamageSignal >= 0.82
}

func buildGameplayUnderstanding(observations []domain.FrameObservation, windows []domain.ReviewWindow, segments []domain.RoundSegment, ocrStatus string, ocrAnalyzedFrames int) *domain.GameplayUnderstanding {
	if len(observations) == 0 {
		return nil
	}

	averageLayout := avgObservation(observations, func(o domain.FrameObservation) float64 { return o.HUDLayoutConfidence })
	compatibleFrames := 0
	understanding := &domain.GameplayUnderstanding{
		Game:                       "valorant",
		Method:                     "cpu_hud_layout_and_temporal_cv",
		AverageHUDLayoutConfidence: round4(averageLayout),
		OCRStatus:                  ocrStatus,
		OCRAnalyzedFrameCount:      ocrAnalyzedFrames,
	}
	for _, observation := range observations {
		if observation.HUDLayoutConfidence >= 0.22 {
			compatibleFrames++
		}
		if observation.BuyPhaseSignal >= trustedBuyPhaseSignal {
			understanding.BuyPhaseFrameCount++
		}
		if observation.ScoreboardSignal >= trustedScoreboardSignal {
			understanding.ScoreboardFrameCount++
		}
		if observation.CombatReportSignal >= trustedCombatReportSignal {
			understanding.CombatReportFrameCount++
		}
		if observation.RoundEndSignal >= 0.90 {
			understanding.RoundEndFrameCount++
		}
		if observation.KillfeedEventSignal >= 0.18 {
			understanding.KillfeedEventFrameCount++
		}
		if observation.DamageSignal >= 0.82 {
			understanding.DamageEventFrameCount++
		}
	}
	for _, window := range windows {
		switch window.Kind {
		case "death_review":
			understanding.DeathReviewCount++
		case "combat_spike":
			understanding.CorroboratedFightCount++
		}
	}
	if len(segments) > 0 {
		understanding.RoundDetectionMethod = segments[0].DetectionMethod
	}

	coverage := float64(compatibleFrames) / float64(len(observations))
	understanding.CompatibilityConfidence = round4(clamp01(averageLayout*0.56 + coverage*0.44))
	switch {
	case averageLayout >= 0.30 && coverage >= 0.50:
		understanding.CaptureCompatibility = "supported"
	case averageLayout >= 0.16 && coverage >= 0.22:
		understanding.CaptureCompatibility = "degraded"
	default:
		understanding.CaptureCompatibility = "unsupported"
	}
	return understanding
}

func evidenceForObservation(observation domain.FrameObservation) domain.EvidenceRef {
	return evidenceForObservationRole(observation, "")
}

func evidenceForObservationRole(observation domain.FrameObservation, role string) domain.EvidenceRef {
	return domain.EvidenceRef{
		ArtifactType:     "frame",
		Path:             observation.Path,
		Role:             role,
		TimestampSeconds: observation.TimestampSeconds,
		FrameIndex:       observation.Index,
	}
}

func evidenceSequence(observations []domain.FrameObservation, peakIndex int, beforeSeconds, afterSeconds float64) []domain.EvidenceRef {
	if peakIndex < 0 || peakIndex >= len(observations) {
		return nil
	}

	peak := observations[peakIndex]
	evidence := make([]domain.EvidenceRef, 0, 3)
	if before := nearestObservationIndex(observations, peakIndex, peak.TimestampSeconds-beforeSeconds, -1); before >= 0 {
		evidence = append(evidence, evidenceForObservationRole(observations[before], "before"))
	}
	evidence = append(evidence, evidenceForObservationRole(peak, "event"))
	if after := nearestObservationIndex(observations, peakIndex, peak.TimestampSeconds+afterSeconds, 1); after >= 0 {
		evidence = append(evidence, evidenceForObservationRole(observations[after], "after"))
	}
	return evidence
}

func nearestObservationIndex(observations []domain.FrameObservation, peakIndex int, target float64, direction int) int {
	best := -1
	bestDistance := math.MaxFloat64
	start, end, step := 0, len(observations), 1
	if direction < 0 {
		start, end, step = peakIndex-1, -1, -1
	} else if direction > 0 {
		start = peakIndex + 1
	}
	for index := start; index != end; index += step {
		distance := math.Abs(observations[index].TimestampSeconds - target)
		if distance < bestDistance {
			best = index
			bestDistance = distance
		}
		if direction < 0 && observations[index].TimestampSeconds < target {
			break
		}
		if direction > 0 && observations[index].TimestampSeconds > target {
			break
		}
	}
	return best
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
