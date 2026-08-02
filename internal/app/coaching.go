package app

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const EvidenceCoachEngineVersion = "evidence-coach-v1"

type CoachEngine interface {
	BuildReview(ctx context.Context, request CoachReviewRequest) (*domain.CoachReview, error)
	AssessDecision(ctx context.Context, request CoachAssessmentRequest) (domain.CoachDecision, error)
}

type CoachReviewRequest struct {
	VOD      domain.VOD
	Media    domain.MediaSummary
	Sample   domain.FrameSampleSummary
	Gameplay domain.GameplaySummary
}

type CoachAssessmentRequest struct {
	Decision domain.CoachDecision
	Answers  map[string]string
}

type EvidenceCoachEngine struct{}

func (EvidenceCoachEngine) BuildReview(ctx context.Context, request CoachReviewRequest) (*domain.CoachReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	quality := coachEvidenceQuality(request)
	decisions := make([]domain.CoachDecision, 0, len(request.Gameplay.ReviewWindows))
	for _, window := range request.Gameplay.ReviewWindows {
		decision := decisionForWindow(window, quality)
		if decision.ID != "" {
			decisions = append(decisions, decision)
		}
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].TimestampSeconds == decisions[j].TimestampSeconds {
			return decisions[i].ID < decisions[j].ID
		}
		return decisions[i].TimestampSeconds < decisions[j].TimestampSeconds
	})

	status := "no_review_windows"
	summary := "No observable gameplay windows were selected for guided review."
	if len(decisions) > 0 {
		status = "guided_review_required"
		summary = fmt.Sprintf("%d observable moments are ready for guided review. No gameplay mistake is claimed until the visible context is confirmed.", len(decisions))
	}

	return &domain.CoachReview{
		SchemaVersion:   domain.CoachReviewSchemaVersion,
		Engine:          EvidenceCoachEngineVersion,
		Method:          "automatic evidence selection plus deterministic guided assessment",
		Status:          status,
		Summary:         summary,
		EvidenceQuality: quality,
		Decisions:       decisions,
		Limitations: []string{
			"Visual intensity does not prove a kill, death, bad peek, or tactical mistake.",
			"Crosshair placement, tradeability, utility intent, hidden enemies, and team communication require visible confirmation.",
			"Round numbers are navigation estimates until timer and score OCR are confirmed.",
		},
	}, nil
}

func (EvidenceCoachEngine) AssessDecision(ctx context.Context, request CoachAssessmentRequest) (domain.CoachDecision, error) {
	if err := ctx.Err(); err != nil {
		return domain.CoachDecision{}, err
	}
	if strings.TrimSpace(request.Decision.ID) == "" {
		return domain.CoachDecision{}, fmt.Errorf("coach decision is required")
	}

	answers := normalizeAnswers(request.Answers)
	decision := request.Decision
	decision.Questions = request.Decision.Questions
	decision.Requirements = assessedRequirements(decision.Requirements, answers)

	switch decision.Kind {
	case "combat_spike":
		return assessCombat(decision, answers), nil
	case "death_review":
		return assessDeath(decision, answers), nil
	case "rotation_spike":
		return assessRotation(decision, answers), nil
	case "low_activity":
		return assessTempo(decision, answers), nil
	default:
		return domain.CoachDecision{}, fmt.Errorf("unsupported coach decision kind %q", decision.Kind)
	}
}

func decisionForWindow(window domain.ReviewWindow, quality domain.CoachEvidenceQuality) domain.CoachDecision {
	base := domain.CoachDecision{
		ID:                  "coach_" + window.ID,
		RuleID:              "candidate_" + window.Kind,
		WindowID:            window.ID,
		Kind:                window.Kind,
		Assessment:          "needs_confirmation",
		Severity:            domain.FindingSeverityInfo,
		Confidence:          candidateConfidence(window.Score, quality.Score),
		TimestampSeconds:    window.PeakSeconds,
		StartSeconds:        window.StartSeconds,
		EndSeconds:          window.EndSeconds,
		RoundNumber:         window.RoundNumber,
		ClipPath:            window.ClipPath,
		ClipDurationSeconds: window.ClipDurationSeconds,
		Evidence:            append([]domain.EvidenceRef(nil), window.Evidence...),
		Tags:                append([]string{"guided-review", "candidate"}, window.Tags...),
	}

	switch window.Kind {
	case "death_review":
		base.Title = "Review the decisions before this death"
		base.Observation = window.Summary
		base.WhyReview = "The combat report contains an OCR-confirmed POV death marker. The evidence sequence can locate the preceding decision, but tradeability, intent, utility, and mechanics still require visible confirmation."
		base.Requirements = requirements(
			requirement("death_confirmed", "The combat report belongs to this POV death"),
			requirement("tradeable", "Trade context is visible"),
			requirement("utility_used", "Utility sequence is visible"),
			requirement("crosshair_ready", "Crosshair readiness is visible"),
			requirement("escape_route", "Cover or fallback options are visible"),
		)
		base.Questions = deathQuestions()
	case "combat_spike":
		base.Title = "Confirm the fight context"
		base.Observation = window.Summary
		base.WhyReview = "The visual spike is useful for locating first contact, but it cannot identify the outcome or quality of the decision by itself."
		base.Requirements = requirements(
			requirement("fight_occurred", "A fight actually occurred"),
			requirement("outcome", "Fight outcome is visible"),
			requirement("tradeable", "Trade context is visible"),
			requirement("utility_used", "Utility sequence is visible"),
			requirement("crosshair_ready", "Crosshair readiness is visible"),
		)
		base.Questions = combatQuestions()
	case "rotation_spike":
		base.Title = "Confirm the rotation decision"
		base.Observation = window.Summary
		base.WhyReview = "Fast POV movement can be a rotation, reposition, ability animation, or edit; macro quality depends on visible information and teammate spacing."
		base.Requirements = requirements(
			requirement("movement_was_rotation", "Movement is a rotation or reposition"),
			requirement("new_information", "Information trigger is visible"),
			requirement("teammate_spacing", "Teammate spacing is visible"),
			requirement("sound_safe", "Sound discipline can be judged"),
		)
		base.Questions = rotationQuestions()
	case "low_activity":
		base.Title = "Confirm the hold or tempo decision"
		base.Observation = window.Summary
		base.WhyReview = "A stable POV can be a correct angle hold, buy phase, menu, post-plant discipline, or lost tempo. Intent and information gain must be confirmed."
		base.Requirements = requirements(
			requirement("window_is_hold", "Window is active gameplay"),
			requirement("purpose_clear", "Purpose of waiting is known"),
			requirement("information_gained", "Information gain is visible"),
			requirement("team_aligned", "Team timing is visible"),
		)
		base.Questions = tempoQuestions()
	default:
		return domain.CoachDecision{}
	}
	return base
}

func SummarizeCoachReview(review *domain.CoachReview) *domain.CoachSummary {
	if review == nil {
		return nil
	}

	type group struct {
		kind      string
		category  string
		title     string
		decisions []domain.CoachDecision
	}
	groups := []group{
		{kind: "death_review", category: "death_review", title: "Deaths to review"},
		{kind: "combat_spike", category: "combat_review", title: "Fight contexts to validate"},
		{kind: "rotation_spike", category: "macro_review", title: "Rotation contexts to validate"},
		{kind: "low_activity", category: "tempo_review", title: "Hold and tempo contexts to validate"},
	}
	for _, decision := range review.Decisions {
		for index := range groups {
			if groups[index].kind == decision.Kind {
				groups[index].decisions = append(groups[index].decisions, decision)
			}
		}
	}

	focus := make([]domain.CoachFocusArea, 0, len(groups))
	for _, item := range groups {
		if len(item.decisions) == 0 {
			continue
		}
		maxConfidence := 0.0
		windowIDs := make([]string, 0, len(item.decisions))
		for _, decision := range item.decisions {
			maxConfidence = math.Max(maxConfidence, decision.Confidence)
			windowIDs = append(windowIDs, decision.WindowID)
		}
		priority := "low"
		if len(item.decisions) >= 5 {
			priority = "high"
		} else if len(item.decisions) >= 2 {
			priority = "medium"
		}
		focus = append(focus, domain.CoachFocusArea{
			ID: item.kind, Priority: priority, Category: item.category, Title: item.title,
			Detail: fmt.Sprintf("%d automatically selected moments require visible-context confirmation before they can become coaching findings.", len(item.decisions)),
			Score:  roundCoach(maxConfidence), WindowIDs: windowIDs,
		})
	}
	sort.SliceStable(focus, func(i, j int) bool {
		if len(focus[i].WindowIDs) == len(focus[j].WindowIDs) {
			return focus[i].ID < focus[j].ID
		}
		return len(focus[i].WindowIDs) > len(focus[j].WindowIDs)
	})

	practice := []domain.PracticeTask{}
	if len(review.Decisions) > 0 {
		practice = append(practice, domain.PracticeTask{
			ID: "guided_review_pass", Title: "Complete the guided evidence pass",
			Detail:  "Open the selected moments in chronological order and confirm only facts visible in the video. The rules engine will generate advice from those answers.",
			Cadence: "once per analyzed VOD", Tags: []string{"evidence", "guided-review"},
		})
	}

	return &domain.CoachSummary{
		Verdict:      review.Summary,
		Confidence:   review.EvidenceQuality.Score,
		FocusAreas:   focus,
		PracticePlan: practice,
	}
}

func assessDeath(decision domain.CoachDecision, answers map[string]string) domain.CoachDecision {
	if answers["death_confirmed"] == "no" {
		return conclude(decision, "not_applicable", "death_false_positive", domain.FindingSeverityInfo, "Not this POV's death", "The selected combat-report panel did not represent a death by the reviewed player.", nil, 0.98)
	}
	if missingRequired(decision, answers) {
		return pending(decision, "Confirm the death, trade setup, utility sequence, crosshair readiness, and fallback options before drawing a conclusion.")
	}

	mapped := make(map[string]string, len(answers)+2)
	for key, value := range answers {
		mapped[key] = value
	}
	mapped["fight_occurred"] = "yes"
	mapped["outcome"] = "death"
	return assessCombat(decision, mapped)
}

func assessCombat(decision domain.CoachDecision, answers map[string]string) domain.CoachDecision {
	if answers["fight_occurred"] == "no" {
		return conclude(decision, "not_applicable", "combat_false_positive", domain.FindingSeverityInfo, "Not a fight", "The selected visual spike was not a gameplay fight.", nil, 0.98)
	}
	if missingRequired(decision, answers) {
		return pending(decision, "Confirm the fight outcome, trade setup, utility sequence, and crosshair readiness before drawing a conclusion.")
	}

	outcome := answers["outcome"]
	if answers["tradeable"] == "no" {
		severity := domain.FindingSeverityMedium
		if outcome == "death" {
			severity = domain.FindingSeverityHigh
		}
		return conclude(decision, "validated_risk", "combat_untradeable_contact", severity, "Untradeable first contact", "The confirmed fight happened without a teammate able to trade the contact.", &domain.CoachRecommendation{
			Summary:      "Avoid isolated contact unless it creates a deliberate high-value advantage.",
			WhyItMatters: "An untradeable death gives the opponent a clean numbers advantage; even a won duel can reinforce a low-percentage process.",
			BetterAction: "Delay the swing, change the angle, or call/wait for teammate spacing so the next contact can be traded within roughly two seconds.",
			Drill:        "Review five first-contact clips. Pause three seconds before contact and mark the nearest teammate, trade route, and escape route.",
			Checkpoint:   "Before committing: who trades me, what forces the duel, and where do I leave?",
		}, assessedConfidence(decision, answers))
	}
	if answers["utility_available"] == "yes" && answers["utility_used"] == "no" {
		return conclude(decision, "validated_mistake", "combat_utility_sequence", domain.FindingSeverityMedium, "Utility was available before contact", "The fight was confirmed and useful utility was available but not used before committing.", &domain.CoachRecommendation{
			Summary:      "Sequence utility before the duel when it removes an angle or creates a timing advantage.",
			WhyItMatters: "Dry contact gives the opponent full vision, movement, and first-shot readiness while preserving your unused resources.",
			BetterAction: "Choose one purpose before peeking: clear the close angle, displace the holder, deny vision, or create entry timing; then swing on that utility.",
			Drill:        "For one map, prepare one utility-assisted first-contact pattern for each common site entry and rehearse it in a custom lobby.",
			Checkpoint:   "What exact defender option does this utility remove before I expose myself?",
		}, assessedConfidence(decision, answers))
	}
	if answers["crosshair_ready"] == "no" {
		return conclude(decision, "validated_mistake", "combat_crosshair_preparation", domain.FindingSeverityMedium, "Crosshair was not ready for first contact", "The visible pre-contact crosshair was not prepared for the angle where the fight occurred.", &domain.CoachRecommendation{
			Summary:      "Finish crosshair placement before exposing the next angle.",
			WhyItMatters: "A correction after the enemy appears adds avoidable time to first accurate damage and reduces movement discipline.",
			BetterAction: "Clear one threat at a time, place the crosshair at expected head height, stop the movement, then expose the next slice.",
			Drill:        "Run two deathmatch games with score ignored. Before every corner, name the next angle and forbid firing until the crosshair is placed.",
			Checkpoint:   "If an enemy appears on the next pixel, is the first accurate bullet already aligned?",
		}, assessedConfidence(decision, answers))
	}
	if answers["escape_route"] == "no" && outcome == "death" {
		return conclude(decision, "validated_mistake", "combat_no_exit_plan", domain.FindingSeverityMedium, "No exit after committing", "The confirmed death followed contact without a visible fallback or reposition route.", &domain.CoachRecommendation{
			Summary:      "Take contact from a position that supports a second decision.",
			WhyItMatters: "A one-way commitment turns any missed first burst into a forced fight and removes the option to reset around utility or teammates.",
			BetterAction: "Use tighter exposure, play near cover, or choose a path that lets you break line of sight after the first burst.",
			Drill:        "In every review clip, draw the intended cover and fallback path before first contact. Flag fights with neither.",
			Checkpoint:   "Where am I after the first burst if nobody dies?",
		}, assessedConfidence(decision, answers))
	}

	return conclude(decision, "validated_neutral", "combat_no_clear_process_error", domain.FindingSeverityInfo, "No clear fight-process error", "The confirmed answers do not establish an isolation, utility-sequencing, crosshair-preparation, or exit-plan error.", nil, assessedConfidence(decision, answers))
}

func assessRotation(decision domain.CoachDecision, answers map[string]string) domain.CoachDecision {
	if answers["movement_was_rotation"] == "no" {
		return conclude(decision, "not_applicable", "rotation_false_positive", domain.FindingSeverityInfo, "Not a rotation", "The movement spike was not a rotation or meaningful reposition.", nil, 0.98)
	}
	if missingRequired(decision, answers) {
		return pending(decision, "Confirm the information trigger, teammate spacing, objective pressure, and sound discipline before evaluating the rotation.")
	}
	if answers["teammate_spacing"] == "no" {
		return conclude(decision, "validated_risk", "rotation_isolated_timing", domain.FindingSeverityMedium, "Rotation broke team spacing", "The confirmed rotation created isolated timing without a teammate able to trade or support the route.", &domain.CoachRecommendation{
			Summary:      "Rotate on a timing that preserves support or creates a deliberate lurk advantage.",
			WhyItMatters: "Arriving alone creates sequential fights and lets the opponent deal with the team one player at a time.",
			BetterAction: "Slow down to pair with the nearest teammate, choose a safer connector, or explicitly hold the lurk until the team can pressure elsewhere.",
			Drill:        "Review one attacking half and pause at every connector. Record nearest teammate distance and whether contact can be traded within two seconds.",
			Checkpoint:   "Am I creating simultaneous pressure or merely arriving alone?",
		}, assessedConfidence(decision, answers))
	}
	if answers["new_information"] == "no" && answers["objective_pressure"] == "no" {
		return conclude(decision, "validated_risk", "rotation_without_trigger", domain.FindingSeverityMedium, "Rotation lacked an information trigger", "No new information or objective pressure was confirmed before leaving the previous area.", &domain.CoachRecommendation{
			Summary:      "Attach rotations to a visible trigger instead of discomfort or elapsed time alone.",
			WhyItMatters: "Unsupported movement can give up controlled space, arrive after the useful timing, and make the team vulnerable to fakes.",
			BetterAction: "Hold until a trigger appears: committed utility, spike information, teammate contact, objective timer, or a planned team call.",
			Drill:        "For ten rotation clips, write the exact trigger in one sentence. Mark every clip where the trigger cannot be named.",
			Checkpoint:   "What changed on this frame that makes rotating better than holding?",
		}, assessedConfidence(decision, answers))
	}
	if answers["sound_safe"] == "no" {
		return conclude(decision, "validated_mistake", "rotation_sound_exposure", domain.FindingSeverityLow, "Rotation exposed timing through sound", "The confirmed route made avoidable sound while the opponent could use that cue.", &domain.CoachRecommendation{
			Summary:      "Choose deliberately between speed and information denial.",
			WhyItMatters: "Unnecessary footsteps reveal route and timing, allowing opponents to stack, flank, or hold the next contact ready.",
			BetterAction: "Walk through the information-sensitive segment, then run only after the route is already known or the objective timer demands speed.",
			Drill:        "Review three rotations with audio. Mark the first audible footstep and ask what the opponent can infer at that moment.",
			Checkpoint:   "Is arriving earlier worth revealing where and when I arrive?",
		}, assessedConfidence(decision, answers))
	}
	return conclude(decision, "validated_neutral", "rotation_no_clear_process_error", domain.FindingSeverityInfo, "Rotation has a defensible process", "The confirmed rotation had an information or objective trigger, preserved team spacing, and did not expose avoidable sound.", nil, assessedConfidence(decision, answers))
}

func assessTempo(decision domain.CoachDecision, answers map[string]string) domain.CoachDecision {
	if answers["window_is_hold"] == "no" {
		return conclude(decision, "not_applicable", "tempo_false_positive", domain.FindingSeverityInfo, "Not an active hold", "The stable window was not an active gameplay hold or wait decision.", nil, 0.98)
	}
	if missingRequired(decision, answers) {
		return pending(decision, "Confirm the purpose, information gain, team timing, and availability of safe space before judging the hold.")
	}
	if answers["team_aligned"] == "no" {
		return conclude(decision, "validated_risk", "tempo_desynced_hold", domain.FindingSeverityMedium, "Hold was disconnected from team timing", "The confirmed wait did not support the timing or pressure created by teammates.", &domain.CoachRecommendation{
			Summary:      "Make the hold interact with team pressure instead of creating a separate, late fight.",
			WhyItMatters: "Disconnected timing lets opponents focus on one threat at a time and reduces the value of teammate utility and contact.",
			BetterAction: "Move into trade range, hold the opponent's escape route, or communicate a delayed timing that lands while teammates still apply pressure.",
			Drill:        "Review five low-activity clips. At each one, identify the teammate creating pressure and the exact way your position helps within the next five seconds.",
			Checkpoint:   "What teammate action becomes stronger because I am waiting here?",
		}, assessedConfidence(decision, answers))
	}
	if answers["purpose_clear"] == "no" && answers["safe_space_available"] == "yes" {
		return conclude(decision, "validated_mistake", "tempo_unproductive_wait", domain.FindingSeverityMedium, "Waiting had no clear value", "No purpose or information gain was confirmed while safer value-producing space was available.", &domain.CoachRecommendation{
			Summary:      "Replace passive time with a low-risk action that gains space, information, or teammate support.",
			WhyItMatters: "Unused time allows defenders to rotate, recover utility, improve crossfires, and isolate later contact.",
			BetterAction: "Take the next safe slice of space, regroup into trade distance, clear a flank timing, or use utility to force new information.",
			Drill:        "During one VOD review, pause every ten seconds without contact and name one value-producing action. Compare it with the action actually taken.",
			Checkpoint:   "What new space, information, or support do I create in the next five seconds?",
		}, assessedConfidence(decision, answers))
	}
	if answers["purpose_clear"] == "no" && answers["information_gained"] == "no" {
		return conclude(decision, "validated_risk", "tempo_no_information_value", domain.FindingSeverityLow, "Hold produced no information", "The confirmed hold had no clear purpose and did not produce useful information.", &domain.CoachRecommendation{
			Summary:      "Set a condition and expiry time before holding an angle.",
			WhyItMatters: "A hold without a trigger or expiry can consume the round while the map state changes elsewhere.",
			BetterAction: "Define what you are waiting for and when you leave; if neither condition is met, regroup or take information with support.",
			Drill:        "For each hold clip, write: trigger, expected reward, and expiry. Any missing field becomes the next decision habit.",
			Checkpoint:   "What am I waiting for, what do I gain, and when does this hold expire?",
		}, assessedConfidence(decision, answers))
	}
	return conclude(decision, "validated_neutral", "tempo_defensible_hold", domain.FindingSeverityInfo, "Hold has a defensible purpose", "The confirmed hold supported team timing or gained information without a clearly better safe-space action.", nil, assessedConfidence(decision, answers))
}

func conclude(decision domain.CoachDecision, assessment string, ruleID string, severity domain.FindingSeverity, title string, observation string, recommendation *domain.CoachRecommendation, confidence float64) domain.CoachDecision {
	decision.Assessment = assessment
	decision.RuleID = ruleID
	decision.Severity = severity
	decision.Title = title
	decision.Observation = observation
	decision.WhyReview = "Guided review completed from visible context confirmed by the user."
	decision.Recommendation = recommendation
	decision.Confidence = roundCoach(confidence)
	return decision
}

func pending(decision domain.CoachDecision, why string) domain.CoachDecision {
	decision.Assessment = "needs_confirmation"
	decision.RuleID = "candidate_" + decision.Kind
	decision.Severity = domain.FindingSeverityInfo
	decision.WhyReview = why
	decision.Recommendation = nil
	return decision
}

func missingRequired(decision domain.CoachDecision, answers map[string]string) bool {
	for _, question := range decision.Questions {
		if !question.Required {
			continue
		}
		value := answers[question.ID]
		if value == "" || value == "unknown" {
			return true
		}
	}
	return false
}

func assessedRequirements(requirements []domain.EvidenceRequirement, answers map[string]string) []domain.EvidenceRequirement {
	out := append([]domain.EvidenceRequirement(nil), requirements...)
	for index := range out {
		value := answers[out[index].ID]
		switch value {
		case "", "unknown":
			out[index].Status = "needs_confirmation"
		default:
			out[index].Status = "met"
			out[index].Detail = "confirmed: " + value
		}
	}
	return out
}

func requirements(items ...domain.EvidenceRequirement) []domain.EvidenceRequirement { return items }

func requirement(id string, label string) domain.EvidenceRequirement {
	return domain.EvidenceRequirement{ID: id, Label: label, Status: "needs_confirmation"}
}

func combatQuestions() []domain.CoachQuestion {
	return []domain.CoachQuestion{
		question("fight_occurred", "Did a real fight occur in this window?", true, yesNoUnknown()...),
		question("outcome", "What was the visible outcome?", true, option("death", "Death"), option("kill", "Kill"), option("survived", "Survived / disengaged"), option("unknown", "Cannot tell")),
		question("tradeable", "Could a teammate trade the contact quickly?", true, yesNoUnknown()...),
		question("utility_available", "Was useful utility available before contact?", true, yesNoUnknown()...),
		question("utility_used", "Was utility used to improve the contact?", true, option("yes", "Yes"), option("no", "No"), option("not_available", "Not available"), option("unknown", "Cannot tell")),
		question("crosshair_ready", "Was the crosshair ready on the likely contact angle?", true, yesNoUnknown()...),
		question("escape_route", "Was cover or a fallback route available after the first burst?", true, yesNoUnknown()...),
	}
}

func deathQuestions() []domain.CoachQuestion {
	return []domain.CoachQuestion{
		question("death_confirmed", "Does this combat report belong to the reviewed player's death?", true, yesNoUnknown()...),
		question("contact_intent", "What was the visible purpose of the contact?", false, option("entry", "Entry / take space"), option("trade", "Trade teammate"), option("hold", "Hold controlled space"), option("retake", "Retake / defend objective"), option("forced", "Forced by opponent"), option("unknown", "Cannot tell")),
		question("tradeable", "Could a teammate trade this death quickly?", true, yesNoUnknown()...),
		question("utility_available", "Was useful utility available before contact?", true, yesNoUnknown()...),
		question("utility_used", "Was utility used to improve the contact?", true, option("yes", "Yes"), option("no", "No"), option("not_available", "Not available"), option("unknown", "Cannot tell")),
		question("crosshair_ready", "Was the crosshair ready on the likely contact angle?", true, yesNoUnknown()...),
		question("escape_route", "Was cover or a fallback route available after the first burst?", true, yesNoUnknown()...),
	}
}

func rotationQuestions() []domain.CoachQuestion {
	return []domain.CoachQuestion{
		question("movement_was_rotation", "Was this a real rotation or meaningful reposition?", true, yesNoUnknown()...),
		question("new_information", "Was the move triggered by new visible information?", true, yesNoUnknown()...),
		question("objective_pressure", "Did spike state or round time require the move?", true, yesNoUnknown()...),
		question("teammate_spacing", "Did the route preserve support or trade spacing?", true, yesNoUnknown()...),
		question("sound_safe", "Was the sound level appropriate for the information risk?", true, yesNoUnknown()...),
	}
}

func tempoQuestions() []domain.CoachQuestion {
	return []domain.CoachQuestion{
		question("window_is_hold", "Was this active gameplay rather than buy phase, menu, or edit?", true, yesNoUnknown()...),
		question("purpose_clear", "Did the hold have a clear trigger or objective?", true, yesNoUnknown()...),
		question("information_gained", "Did waiting gain or deny useful information?", true, yesNoUnknown()...),
		question("team_aligned", "Did the timing support teammates or objective pressure?", true, yesNoUnknown()...),
		question("safe_space_available", "Was safer value-producing space available?", true, yesNoUnknown()...),
	}
}

func question(id string, prompt string, required bool, options ...domain.CoachQuestionOption) domain.CoachQuestion {
	return domain.CoachQuestion{ID: id, Prompt: prompt, Required: required, Options: options}
}

func yesNoUnknown() []domain.CoachQuestionOption {
	return []domain.CoachQuestionOption{option("yes", "Yes"), option("no", "No"), option("unknown", "Cannot tell")}
}

func option(value string, label string) domain.CoachQuestionOption {
	return domain.CoachQuestionOption{Value: value, Label: label}
}

func coachEvidenceQuality(request CoachReviewRequest) domain.CoachEvidenceQuality {
	coverage := ratioCoach(request.Gameplay.AnalyzedFrames, request.Gameplay.SampledFrames)
	hud := clampCoach(request.Gameplay.AverageHUDSignal)
	minimap := clampCoach(request.Gameplay.AverageMinimapSignal)
	fps := clampCoach(request.Sample.FPSValue / 2)
	score := clampCoach(coverage*0.44 + hud*0.18 + minimap*0.2 + fps*0.12 + boolScore(request.Media.HasAudio)*0.06)
	level := "poor"
	if score >= 0.72 {
		level = "good"
	} else if score >= 0.48 {
		level = "fair"
	}
	return domain.CoachEvidenceQuality{
		Score:            roundCoach(score),
		Level:            level,
		FrameCoverage:    roundCoach(coverage),
		HUDSignal:        roundCoach(hud),
		MinimapSignal:    roundCoach(minimap),
		HasAudio:         request.Media.HasAudio,
		MacroReviewReady: coverage >= 0.9 && minimap >= 0.12,
		MicroReviewReady: coverage >= 0.9 && request.Sample.FPSValue >= 1,
	}
}

func candidateConfidence(windowScore float64, qualityScore float64) float64 {
	return roundCoach(math.Min(0.64, 0.28+clampCoach(windowScore)*0.22+clampCoach(qualityScore)*0.14))
}

func assessedConfidence(decision domain.CoachDecision, answers map[string]string) float64 {
	confirmed := 0
	total := 0
	for _, question := range decision.Questions {
		if !question.Required {
			continue
		}
		total++
		if value := answers[question.ID]; value != "" && value != "unknown" {
			confirmed++
		}
	}
	coverage := ratioCoach(confirmed, total)
	return math.Min(0.96, 0.62+coverage*0.26+decision.Confidence*0.08)
}

func normalizeAnswers(answers map[string]string) map[string]string {
	out := make(map[string]string, len(answers))
	for key, value := range answers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.ToLower(strings.TrimSpace(value))
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func ratioCoach(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return clampCoach(float64(numerator) / float64(denominator))
}

func clampCoach(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func roundCoach(value float64) float64 {
	return math.Round(value*10000) / 10000
}
