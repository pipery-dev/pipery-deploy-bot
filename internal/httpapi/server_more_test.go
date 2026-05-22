package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

func TestRequestToDeployValidatesRequiredFields(t *testing.T) {
	_, err := requestToDeploy(createDeployRequest{
		Owner:       "pipery-dev",
		Repo:        "example",
		WorkflowID:  "deploy.yml",
		Ref:         "main",
		ScheduledAt: "2026-05-17T12:00:00Z",
	})
	if err == nil {
		t.Fatal("expected missing idempotency_key to be rejected")
	}
}

func TestRequestToDeployRejectsNonRFC3339Timestamps(t *testing.T) {
	_, err := requestToDeploy(createDeployRequest{
		IdempotencyKey: "deploy-1",
		Owner:          "pipery-dev",
		Repo:           "example",
		WorkflowID:     "deploy.yml",
		Ref:            "main",
		ScheduledAt:    "2026-05-17 12:00:00Z",
	})
	if err == nil {
		t.Fatal("expected non-RFC3339 scheduled_at to be rejected")
	}
}

func TestRequestToDeployDefaultsAndNormalizes(t *testing.T) {
	item, err := requestToDeploy(createDeployRequest{
		IdempotencyKey: "deploy-1",
		Owner:          "pipery-dev",
		Repo:           "example",
		WorkflowID:     "deploy.yml",
		Ref:            "main",
		ScheduledAt:    "2026-05-17T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("requestToDeploy returned error: %v", err)
	}
	if item.ID == "" {
		t.Fatal("ID was not assigned")
	}
	if item.InstallationKey != "default" {
		t.Fatalf("InstallationKey = %q, want default", item.InstallationKey)
	}
	if item.Status != deploy.StatusPending {
		t.Fatalf("Status = %q, want pending", item.Status)
	}
	if item.Inputs == nil || len(item.Inputs) != 0 {
		t.Fatalf("Inputs = %#v, want empty map", item.Inputs)
	}
	if !item.ScheduledAt.Equal(time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("ScheduledAt = %s, want UTC timestamp", item.ScheduledAt)
	}
}

func TestCreateScheduledDeployHandlerCreatesAndDeduplicates(t *testing.T) {
	store := &handlerStore{
		created: deploy.ScheduledDeploy{
			ID:             "deploy-1",
			IdempotencyKey: "deploy-key",
			Status:         deploy.StatusPending,
		},
		isNew: true,
	}
	server := NewServer(store)

	rec := postScheduledDeploy(t, server, `{
		"idempotency_key":"deploy-key",
		"owner":"pipery-dev",
		"repo":"example",
		"workflow_id":"deploy.yml",
		"ref":"main",
		"scheduled_at":"2026-05-17T12:00:00Z"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	store.isNew = false
	rec = postScheduledDeploy(t, server, `{
		"idempotency_key":"deploy-key",
		"owner":"pipery-dev",
		"repo":"example",
		"workflow_id":"deploy.yml",
		"ref":"main",
		"scheduled_at":"2026-05-17T12:00:00Z"
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("dedupe status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if store.createCalls != 2 {
		t.Fatalf("CreateScheduledDeploy calls = %d, want 2", store.createCalls)
	}
	if store.lastCreated.InstallationKey != "default" || store.lastCreated.Status != deploy.StatusPending {
		t.Fatalf("created item was not normalized: %+v", store.lastCreated)
	}
}

func TestCreateScheduledDeployHandlerValidationAndStoreErrors(t *testing.T) {
	server := NewServer(&handlerStore{})
	rec := postScheduledDeploy(t, server, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want 400", rec.Code)
	}

	rec = postScheduledDeploy(t, server, `{"owner":"pipery-dev"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d, want 400", rec.Code)
	}

	server = NewServer(&handlerStore{err: errors.New("database unavailable")})
	rec = postScheduledDeploy(t, server, `{
		"idempotency_key":"deploy-key",
		"owner":"pipery-dev",
		"repo":"example",
		"workflow_id":"deploy.yml",
		"ref":"main",
		"scheduled_at":"2026-05-17T12:00:00Z"
	}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error status = %d, want 500", rec.Code)
	}
}

func TestListHandlersUseStatusDeployIDAndLimit(t *testing.T) {
	store := &handlerStore{
		listDeploys: []deploy.ScheduledDeploy{{ID: "deploy-1", Status: deploy.StatusPending}},
		attempts:    []deploy.TriggerAttempt{{ID: "attempt-1", DeployID: "deploy-1"}},
	}
	server := NewServer(store)

	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/scheduled-deploys?status=pending&limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list deploys status = %d, want 200", rec.Code)
	}
	if store.listStatus != "pending" || store.listLimit != 2 {
		t.Fatalf("ListScheduledDeploys got status=%q limit=%d", store.listStatus, store.listLimit)
	}

	rec = httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/trigger-attempts?deploy_id=deploy-1&limit=3", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list attempts status = %d, want 200", rec.Code)
	}
	if store.attemptDeployID != "deploy-1" || store.attemptLimit != 3 {
		t.Fatalf("ListTriggerAttempts got deployID=%q limit=%d", store.attemptDeployID, store.attemptLimit)
	}
}

func postScheduledDeploy(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/scheduled-deploys", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	return rec
}

type handlerStore struct {
	created         deploy.ScheduledDeploy
	isNew           bool
	err             error
	createCalls     int
	lastCreated     deploy.ScheduledDeploy
	listDeploys     []deploy.ScheduledDeploy
	listStatus      string
	listLimit       int
	attempts        []deploy.TriggerAttempt
	attemptDeployID string
	attemptLimit    int
}

func (s *handlerStore) CreateScheduledDeploy(_ context.Context, d deploy.ScheduledDeploy) (deploy.ScheduledDeploy, bool, error) {
	s.createCalls++
	s.lastCreated = d
	if s.err != nil {
		return deploy.ScheduledDeploy{}, false, s.err
	}
	if s.created.ID == "" {
		s.created = d
	}
	return s.created, s.isNew, nil
}

func (s *handlerStore) ListScheduledDeploys(_ context.Context, status string, limit int) ([]deploy.ScheduledDeploy, error) {
	s.listStatus = status
	s.listLimit = limit
	return s.listDeploys, s.err
}

func (s *handlerStore) ListTriggerAttempts(_ context.Context, deployID string, limit int) ([]deploy.TriggerAttempt, error) {
	s.attemptDeployID = deployID
	s.attemptLimit = limit
	return s.attempts, s.err
}

func TestHealthz(t *testing.T) {
	server := NewServer(&handlerStore{})
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

func TestBearerAuthProtectsAPIsAndDashboardButNotHealth(t *testing.T) {
	server := NewServer(&handlerStore{}, "secret-token")

	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized dashboard status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Pipery Deploy Bot") {
		t.Fatalf("dashboard body missing title: %s", rec.Body.String())
	}
}

func TestDexAuthAcceptsVerifiedBearerToken(t *testing.T) {
	server := NewServer(&handlerStore{}, &authenticator{verifier: fakeVerifier{validToken: "dex-token"}})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("Authorization", "Bearer dex-token")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

type fakeVerifier struct {
	validToken string
}

func (v fakeVerifier) Verify(_ context.Context, token string) (*oidc.IDToken, error) {
	if token != v.validToken {
		return nil, errors.New("invalid token")
	}
	return &oidc.IDToken{}, nil
}
