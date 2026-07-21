package visionservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type modelReviewPayload struct {
	RunID string                   `json:"run_id"`
	VOD   domain.VOD               `json:"vod"`
	Tasks []domain.ModelReviewTask `json:"tasks"`
}

type modelReviewResponse struct {
	Runs []domain.ModelReviewRun `json:"runs"`
}

type HealthStatus struct {
	Status  string `json:"status"`
	Model   string `json:"model,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Runtime string `json:"runtime,omitempty"`
}

func (c Client) Health(ctx context.Context) (HealthStatus, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return HealthStatus{}, fmt.Errorf("vision service base URL is required")
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return HealthStatus{}, err
	}

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return HealthStatus{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return HealthStatus{}, fmt.Errorf("vision service health failed: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var status HealthStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return HealthStatus{}, err
	}
	if strings.TrimSpace(status.Status) == "" {
		return HealthStatus{}, fmt.Errorf("vision service health response missing status")
	}
	return status, nil
}

func (c Client) ReviewModelTasks(ctx context.Context, request app.ModelReviewRequest) (app.ModelReviewResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return app.ModelReviewResult{}, fmt.Errorf("vision service base URL is required")
	}
	if len(request.Tasks) == 0 {
		return app.ModelReviewResult{}, nil
	}

	raw, err := json.Marshal(modelReviewPayload{
		RunID: request.RunID,
		VOD:   request.VOD,
		Tasks: request.Tasks,
	})
	if err != nil {
		return app.ModelReviewResult{}, err
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/model-review", bytes.NewReader(raw))
	if err != nil {
		return app.ModelReviewResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return app.ModelReviewResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return app.ModelReviewResult{}, fmt.Errorf("vision service model review failed: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded modelReviewResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return app.ModelReviewResult{}, err
	}

	return app.ModelReviewResult{
		Runs:     decoded.Runs,
		Findings: modelFindings(request.Tasks, decoded.Runs),
	}, nil
}

func modelFindings(tasks []domain.ModelReviewTask, runs []domain.ModelReviewRun) []domain.Finding {
	taskByID := make(map[string]domain.ModelReviewTask, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID] = task
	}

	findings := make([]domain.Finding, 0)
	for _, run := range runs {
		if run.Status != "completed" {
			continue
		}
		task := taskByID[run.TaskID]
		for index, finding := range run.Findings {
			if strings.TrimSpace(finding.Category) == "" {
				finding.Category = "model_review"
			}
			severity := finding.Severity
			if severity == "" {
				severity = domain.FindingSeverityMedium
			}
			detail := finding.Evidence
			if run.Verdict != "" {
				detail = run.Verdict + " " + detail
			}
			modelFinding := domain.Finding{
				ID:             fmt.Sprintf("model_review_%s_%s_%02d", safeID(run.ID), safeID(finding.Category), index+1),
				Severity:       severity,
				Category:       finding.Category,
				Title:          "Model review: " + strings.ReplaceAll(finding.Category, "_", " "),
				Detail:         strings.TrimSpace(detail),
				Recommendation: finding.Recommendation,
				Confidence:     finding.Confidence,
				Tags:           compactTags("model-review", run.Model, run.PromptVersion),
			}
			if task.ClipPath != "" {
				modelFinding.Evidence = append(modelFinding.Evidence, domain.EvidenceRef{
					ArtifactType:     "review_clip",
					Path:             task.ClipPath,
					TimestampSeconds: finding.TimestampSeconds,
				})
			}
			findings = append(findings, modelFinding)
		}
	}
	return findings
}

func compactTags(values ...string) []string {
	tags := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			tags = append(tags, value)
		}
	}
	return tags
}

func safeID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}
