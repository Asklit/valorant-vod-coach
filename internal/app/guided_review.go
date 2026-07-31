package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const GuidedReviewsJSONName = "guided_reviews.json"

type SavedGuidedReviews struct {
	JSONPath string
}

type SaveGuidedAssessmentRequest struct {
	Report   domain.AnalysisReport
	WindowID string
	Answers  map[string]string
	Author   string
	Now      time.Time
}

type SaveCoachFeedbackRequest struct {
	VODLabel    string
	ReportRunID string
	WindowID    string
	Verdict     string
	Comment     string
	Author      string
	Now         time.Time
}

func LoadGuidedReviews(root string, vodLabel string, reportRunID string) (domain.GuidedReviewSet, SavedGuidedReviews, error) {
	path := guidedReviewsPath(root, vodLabel, reportRunID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyGuidedReviewSet(vodLabel, reportRunID), SavedGuidedReviews{JSONPath: path}, nil
		}
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}

	var set domain.GuidedReviewSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, fmt.Errorf("decode guided reviews: %w", err)
	}
	if set.SchemaVersion == 0 {
		set.SchemaVersion = domain.GuidedReviewSetSchemaVersion
	}
	if set.VODLabel == "" {
		set.VODLabel = strings.TrimSpace(vodLabel)
	}
	if set.ReportRunID == "" {
		set.ReportRunID = strings.TrimSpace(reportRunID)
	}
	if set.Assessments == nil {
		set.Assessments = []domain.GuidedReviewAssessment{}
	}
	return set, SavedGuidedReviews{JSONPath: path}, nil
}

func SaveGuidedAssessment(ctx context.Context, root string, engine CoachEngine, request SaveGuidedAssessmentRequest) (domain.GuidedReviewSet, SavedGuidedReviews, error) {
	if err := ctx.Err(); err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}
	if request.Report.Gameplay == nil || request.Report.Gameplay.CoachReview == nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, errors.New("report does not contain coach_review")
	}
	windowID := strings.TrimSpace(request.WindowID)
	if windowID == "" {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, errors.New("window_id is required")
	}
	decision, ok := findCoachDecision(request.Report.Gameplay.CoachReview.Decisions, windowID)
	if !ok {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, fmt.Errorf("coach decision for window %q was not found", windowID)
	}
	if engine == nil {
		engine = EvidenceCoachEngine{}
	}
	result, err := engine.AssessDecision(ctx, CoachAssessmentRequest{Decision: decision, Answers: request.Answers})
	if err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}

	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	set, saved, err := LoadGuidedReviews(root, request.Report.VOD.Label, request.Report.RunID)
	if err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}
	assessment := domain.GuidedReviewAssessment{
		ID: "assessment_" + safeEvalName(windowID), DecisionID: decision.ID, WindowID: windowID,
		Answers: normalizeAnswers(request.Answers), Result: result,
		Author: strings.TrimSpace(request.Author), CreatedAt: now, UpdatedAt: now,
	}
	updated := false
	for index := range set.Assessments {
		if set.Assessments[index].WindowID != windowID {
			continue
		}
		assessment.ID = set.Assessments[index].ID
		assessment.CreatedAt = set.Assessments[index].CreatedAt
		assessment.Feedback = set.Assessments[index].Feedback
		set.Assessments[index] = assessment
		updated = true
		break
	}
	if !updated {
		set.Assessments = append(set.Assessments, assessment)
	}
	set.SchemaVersion = domain.GuidedReviewSetSchemaVersion
	set.VODLabel = request.Report.VOD.Label
	set.ReportRunID = request.Report.RunID
	set.UpdatedAt = now
	if err := writeGuidedReviews(saved.JSONPath, set); err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}
	return set, saved, nil
}

func SaveCoachFeedback(ctx context.Context, root string, request SaveCoachFeedbackRequest) (domain.GuidedReviewSet, SavedGuidedReviews, error) {
	if err := ctx.Err(); err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}
	verdict := strings.ToLower(strings.TrimSpace(request.Verdict))
	if verdict != "useful" && verdict != "not_useful" && verdict != "incorrect" {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, errors.New("verdict must be useful, not_useful, or incorrect")
	}
	set, saved, err := LoadGuidedReviews(root, request.VODLabel, request.ReportRunID)
	if err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	found := false
	for index := range set.Assessments {
		if set.Assessments[index].WindowID != strings.TrimSpace(request.WindowID) {
			continue
		}
		set.Assessments[index].Feedback = &domain.CoachRecommendationFeedback{
			Verdict: verdict, Comment: strings.TrimSpace(request.Comment), Author: strings.TrimSpace(request.Author), UpdatedAt: now,
		}
		set.Assessments[index].UpdatedAt = now
		found = true
		break
	}
	if !found {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, errors.New("complete the guided assessment before rating its recommendation")
	}
	set.UpdatedAt = now
	if err := writeGuidedReviews(saved.JSONPath, set); err != nil {
		return domain.GuidedReviewSet{}, SavedGuidedReviews{}, err
	}
	return set, saved, nil
}

func findCoachDecision(decisions []domain.CoachDecision, windowID string) (domain.CoachDecision, bool) {
	for _, decision := range decisions {
		if decision.WindowID == windowID {
			return decision, true
		}
	}
	return domain.CoachDecision{}, false
}

func emptyGuidedReviewSet(vodLabel string, reportRunID string) domain.GuidedReviewSet {
	return domain.GuidedReviewSet{
		SchemaVersion: domain.GuidedReviewSetSchemaVersion,
		VODLabel:      strings.TrimSpace(vodLabel), ReportRunID: strings.TrimSpace(reportRunID),
		Assessments: []domain.GuidedReviewAssessment{},
	}
}

func guidedReviewsPath(root string, vodLabel string, reportRunID string) string {
	return filepath.Join(root, safeEvalName(vodLabel), safeEvalName(reportRunID), GuidedReviewsJSONName)
}

func writeGuidedReviews(path string, set domain.GuidedReviewSet) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".guided-reviews-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
