package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

func TestAuthUsesHTTPOnlyCookieAndSessionBoundCSRF(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret-pass","display_name":"Admin","setup_token":"test-bootstrap-token"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("register expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode auth response: %v", err)
	}
	if _, exposed := raw["token"]; exposed {
		t.Fatalf("session token must never be exposed to JavaScript: %s", response.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &auth); err != nil || auth.CSRFToken == "" {
		t.Fatalf("expected CSRF token, auth=%+v err=%v", auth, err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("expected one HTTP-only session cookie: %+v", cookies)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), auth.CSRFToken) {
		t.Fatalf("session must return its CSRF token, got %d: %s", response.Code, response.Body.String())
	}
}

func TestFirstRegistrationRequiresConfiguredSetupToken(t *testing.T) {
	fixture := newFixture(t)
	fixture.config.BootstrapAdminToken = ""
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var session AuthSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode setup session: %v", err)
	}
	if response.Code != http.StatusOK || !session.SetupRequired {
		t.Fatalf("fresh installation must advertise setup requirement, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret-pass"}`))
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured setup expected 503, got %d: %s", response.Code, response.Body.String())
	}

	fixture.config.BootstrapAdminToken = "expected-token"
	server = NewServer(fixture.config)
	request = httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret-pass","setup_token":"wrong-token"}`))
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid setup token expected 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAuthenticationEndpointsAreRateLimitedByClient(t *testing.T) {
	fixture := newFixture(t)
	fixture.config.AuthRequestsPerMinute = 1
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"missing@example.com","password":"secret-pass"}`))
	request.RemoteAddr = "203.0.113.10:1234"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("first login expected 401, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"missing@example.com","password":"secret-pass"}`))
	request.RemoteAddr = "203.0.113.10:4321"
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("second login expected 429 with Retry-After, got %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestAuthRateLimiterExpiresOldClientEntries(t *testing.T) {
	started := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := started
	limiter := newAuthRateLimiter()
	limiter.clock = func() time.Time { return now }
	if allowed, _, _ := limiter.Allow(context.Background(), "login:old-client", 1, time.Minute); !allowed {
		t.Fatal("first request should be allowed")
	}
	now = started.Add(2 * time.Minute)
	if allowed, _, _ := limiter.Allow(context.Background(), "login:new-client", 1, time.Minute); !allowed {
		t.Fatal("new client request should be allowed")
	}
	if _, retained := limiter.entries["login:old-client"]; retained {
		t.Fatal("expired client entry should be evicted")
	}
}

func TestUnsafeRequestRequiresCSRFAndTrustedOrigin(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	auth := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.AddCookie(auth.cookie)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF expected 403, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.AddCookie(auth.cookie)
	request.Header.Set(csrfHeaderName, auth.csrfToken)
	request.Header.Set("Origin", "https://attacker.invalid")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "untrusted request origin") {
		t.Fatalf("untrusted origin expected 403, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	authorize(request, auth)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid logout expected 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCommandJSONRejectsUnknownFieldsAndTrailingObjects(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	auth := registerTestAdmin(t, server)

	for name, body := range map[string]string{
		"unknown field":   `{"vod_label":"diamond_example","unknown":true}`,
		"trailing object": `{"vod_label":"diamond_example"} {"vod_label":"diamond_example"}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", bytes.NewBufferString(body))
			authorize(request, auth)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAnalysisJobIsOwnerIsolated(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	registerTestAdmin(t, server)
	owner := registerTestAccount(t, server, "owner@example.com")
	other := registerTestAccount(t, server, "other@example.com")
	createdAt := time.Now().UTC()
	if err := server.jobs.CreateAnalysisJob(context.Background(), app.AnalysisJob{
		ID: "job_private", OwnerID: owner.user.ID, RunID: "private", VODLabel: "diamond_example",
		Status: app.AnalysisJobQueued, CreatedAt: createdAt, UpdatedAt: createdAt,
	}); err != nil {
		t.Fatalf("create private analysis job: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/analysis-runs/job_private", nil)
	authorize(request, other)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other user must see 404 for private job, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/analysis-runs/job_private", nil)
	authorize(request, owner)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("owner expected 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestSameRunIDProducesOwnerIsolatedReports(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	registerTestAdmin(t, server)
	owner := registerTestAccount(t, server, "owner@example.com")
	other := registerTestAccount(t, server, "other@example.com")

	for _, auth := range []testAuth{owner, other} {
		request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"shared_run","fps":"1","duration_seconds":5,"force":true}`))
		authorize(request, auth)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"owner_id": "`+auth.user.ID+`"`) {
			t.Fatalf("analysis for %s expected isolated owner metadata, got %d: %s", auth.user.ID, response.Code, response.Body.String())
		}
		path := filepath.Join(fixture.outRoot, "users", auth.user.ID, "analyses", "diamond_example", "reports", "shared_run", "report.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("owner report missing at %s: %v", path, err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/reports/diamond_example/shared_run", nil)
	authorize(request, owner)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"owner_id": "`+owner.user.ID+`"`) || strings.Contains(response.Body.String(), other.user.ID) {
		t.Fatalf("owner must only receive own report, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCorrectionsAreUserIsolatedForSharedVOD(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	registerTestAdmin(t, server)
	owner := registerTestAccount(t, server, "owner@example.com")
	other := registerTestAccount(t, server, "other@example.com")
	reportDir := filepath.Join(fixture.outRoot, "diamond_example", "reports", "shared_report")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), []byte(`{"run_id":"shared_report","vod":{"label":"diamond_example"}}`), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/corrections", bytes.NewBufferString(`{"vod_label":"diamond_example","report_run_id":"shared_report","type":"event_note","comment":"private note"}`))
	authorize(request, owner)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("owner correction expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var correctionResponse ManualCorrectionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &correctionResponse); err != nil {
		t.Fatalf("decode correction response: %v", err)
	}
	relativeCorrectionPath, err := filepath.Rel(fixture.outRoot, correctionResponse.JSONPath)
	if err != nil {
		t.Fatalf("resolve correction artifact path: %v", err)
	}
	correctionArtifactURL := "/artifacts/" + filepath.ToSlash(relativeCorrectionPath)
	request = httptest.NewRequest(http.MethodGet, correctionArtifactURL, nil)
	authorize(request, owner)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "private note") {
		t.Fatalf("owner correction export expected 200, got %d: %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, correctionArtifactURL, nil)
	authorize(request, other)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other user correction export expected 404, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/corrections?vod_label=diamond_example&report_run_id=shared_report", nil)
	authorize(request, other)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private note") {
		t.Fatalf("other user must not receive private correction, got %d: %s", response.Code, response.Body.String())
	}
}

func TestArtifactIsAuthenticatedOwnerIsolatedAndTraversalSafe(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	registerTestAdmin(t, server)
	owner := registerTestAccount(t, server, "owner@example.com")
	other := registerTestAccount(t, server, "other@example.com")
	uploaded := uploadTestVOD(t, server, owner)
	artifactDir := filepath.Join(fixture.outRoot, uploaded.VOD.Label, "frames")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "evidence.jpg"), []byte("private evidence"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	path := "/artifacts/" + uploaded.VOD.Label + "/frames/evidence.jpg"

	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous artifact expected 401, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, path, nil)
	authorize(request, other)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other user artifact expected 404, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, path, nil)
	authorize(request, owner)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "private evidence" {
		t.Fatalf("owner artifact expected content, got %d: %q", response.Code, response.Body.String())
	}

	outside := filepath.Join(filepath.Dir(fixture.outRoot), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if _, _, err := secureArtifactPath(fixture.outRoot, "../outside.txt"); err == nil {
		t.Fatalf("artifact path traversal must be rejected")
	}
}

func TestEvaluationEndpointsRequireAdmin(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	registerTestAdmin(t, server)
	user := registerTestAccount(t, server, "user@example.com")

	for _, path := range []string{"/api/evaluations", "/api/evaluation-annotations"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		authorize(request, user)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s expected admin-only 403, got %d: %s", path, response.Code, response.Body.String())
		}
	}
}
