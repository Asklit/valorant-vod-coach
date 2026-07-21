package visionservice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestClientReviewsModelTasks(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/model-review" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var payload modelReviewPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.RunID != "run_01" || len(payload.Tasks) != 1 || payload.Tasks[0].ID != "vlm_window_01" {
			t.Fatalf("unexpected payload: %+v", payload)
		}

		raw := marshalJSON(t, modelReviewResponse{
			Runs: []domain.ModelReviewRun{
				{
					ID:            "review 01",
					TaskID:        "vlm_window_01",
					WindowID:      "window_01",
					Status:        "completed",
					Model:         "stub-vlm",
					PromptVersion: "vlm-review-v1",
					Verdict:       "The player fought without a trade.",
					Findings: []domain.ModelReviewFinding{
						{
							Category:         "positioning",
							Severity:         domain.FindingSeverityMedium,
							TimestampSeconds: 42,
							Evidence:         "Wide swing before contact.",
							Recommendation:   "Hold the tighter angle and wait for spacing.",
							Confidence:       0.74,
						},
					},
				},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(raw)),
		}, nil
	})}

	result, err := (Client{BaseURL: "http://vision-service.test", HTTPClient: httpClient}).ReviewModelTasks(context.Background(), app.ModelReviewRequest{
		RunID: "run_01",
		VOD:   domain.VOD{Label: "vod_01", Rank: "diamond"},
		Tasks: []domain.ModelReviewTask{
			{
				ID:            "vlm_window_01",
				WindowID:      "window_01",
				PromptVersion: "vlm-review-v1",
				ClipPath:      "clips/window_01.mp4",
			},
		},
	})
	if err != nil {
		t.Fatalf("review model tasks: %v", err)
	}

	if len(result.Runs) != 1 || len(result.Findings) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	finding := result.Findings[0]
	if finding.ID != "model_review_review_01_positioning_01" {
		t.Fatalf("unexpected finding ID: %s", finding.ID)
	}
	if len(finding.Evidence) != 1 || finding.Evidence[0].Path != "clips/window_01.mp4" {
		t.Fatalf("unexpected finding evidence: %+v", finding.Evidence)
	}
}

func TestClientChecksHealth(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw := []byte(`{"status":"ok","model":"stub-heuristic-vlm","mode":"stub","runtime":"stdlib-http"}`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(raw)),
		}, nil
	})}

	status, err := (Client{BaseURL: "http://vision-service.test", HTTPClient: httpClient}).Health(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}

	if status.Status != "ok" || status.Model != "stub-heuristic-vlm" || status.Runtime != "stdlib-http" {
		t.Fatalf("unexpected health status: %+v", status)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func marshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return raw
}
