package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

type Store interface {
	CreateScheduledDeploy(ctx context.Context, d deploy.ScheduledDeploy) (deploy.ScheduledDeploy, bool, error)
	ListScheduledDeploys(ctx context.Context, status string, limit int) ([]deploy.ScheduledDeploy, error)
	ListTriggerAttempts(ctx context.Context, deployID string, limit int) ([]deploy.TriggerAttempt, error)
}

type Server struct {
	store         Store
	authenticator *authenticator
}

func NewServer(store Store, auth ...any) *Server {
	server := &Server{store: store}
	if len(auth) > 0 {
		switch value := auth[0].(type) {
		case *authenticator:
			server.authenticator = value
		case string:
			server.authenticator = &authenticator{apiToken: value}
		}
	}
	return server
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /", s.dashboard)
	mux.HandleFunc("GET /dashboard", s.dashboard)
	mux.HandleFunc("POST /v1/scheduled-deploys", s.createScheduledDeploy)
	mux.HandleFunc("GET /v1/scheduled-deploys", s.listScheduledDeploys)
	mux.HandleFunc("GET /v1/trigger-attempts", s.listTriggerAttempts)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	if !s.authenticator.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.authenticator.Authorize(r.Context(), r.Header.Get("Authorization")) {
			bearerAuthError(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	deploys, err := s.store.ListScheduledDeploys(r.Context(), r.URL.Query().Get("status"), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	attempts, err := s.store.ListTriggerAttempts(r.Context(), r.URL.Query().Get("deploy_id"), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTemplate.Execute(w, map[string]any{"Deploys": deploys, "Attempts": attempts}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type createDeployRequest struct {
	IdempotencyKey  string            `json:"idempotency_key"`
	InstallationKey string            `json:"installation_key"`
	InstallationID  int64             `json:"installation_id"`
	Owner           string            `json:"owner"`
	Repo            string            `json:"repo"`
	WorkflowID      string            `json:"workflow_id"`
	Ref             string            `json:"ref"`
	Inputs          map[string]string `json:"inputs"`
	ScheduledAt     string            `json:"scheduled_at"`
}

func (s *Server) createScheduledDeploy(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req createDeployRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	item, err := requestToDeploy(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, isNew, err := s.store.CreateScheduledDeploy(r.Context(), item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := http.StatusCreated
	if !isNew {
		status = http.StatusOK
	}
	writeJSON(w, status, created)
}

func (s *Server) listScheduledDeploys(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 100)
	items, err := s.store.ListScheduledDeploys(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scheduled_deploys": items})
}

func (s *Server) listTriggerAttempts(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 100)
	items, err := s.store.ListTriggerAttempts(r.Context(), r.URL.Query().Get("deploy_id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trigger_attempts": items})
}

func requestToDeploy(req createDeployRequest) (deploy.ScheduledDeploy, error) {
	if req.IdempotencyKey == "" || req.Owner == "" || req.Repo == "" || req.WorkflowID == "" || req.Ref == "" || req.ScheduledAt == "" {
		return deploy.ScheduledDeploy{}, fmt.Errorf("idempotency_key, owner, repo, workflow_id, ref, and scheduled_at are required")
	}
	if req.InstallationKey == "" {
		req.InstallationKey = "default"
	}
	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		return deploy.ScheduledDeploy{}, fmt.Errorf("scheduled_at must be RFC3339 UTC")
	}
	if scheduledAt.Location() != time.UTC && !strings.HasSuffix(req.ScheduledAt, "Z") {
		return deploy.ScheduledDeploy{}, fmt.Errorf("scheduled_at must be UTC and end with Z")
	}
	if req.Inputs == nil {
		req.Inputs = map[string]string{}
	}
	return deploy.ScheduledDeploy{
		ID:              newID(),
		IdempotencyKey:  req.IdempotencyKey,
		InstallationKey: req.InstallationKey,
		InstallationID:  req.InstallationID,
		Owner:           req.Owner,
		Repo:            req.Repo,
		WorkflowID:      req.WorkflowID,
		Ref:             req.Ref,
		Inputs:          req.Inputs,
		ScheduledAt:     scheduledAt.UTC(),
		Status:          deploy.StatusPending,
	}, nil
}

func parseLimit(r *http.Request, fallback int) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return limit
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Pipery Deploy Bot</title>
  <style>
    body { margin: 2rem; background: #0f172a; color: #e5e7eb; font-family: system-ui, sans-serif; }
    table { width: 100%; border-collapse: collapse; margin-bottom: 2rem; background: rgba(15, 23, 42, 0.72); }
    th, td { border-bottom: 1px solid rgba(148, 163, 184, 0.24); padding: .7rem; text-align: left; }
    th { color: #93c5fd; font-size: .82rem; text-transform: uppercase; }
    h1, h2 { margin: 0 0 1rem; }
    h2 { margin-top: 2rem; }
    code { color: #c4b5fd; }
  </style>
</head>
<body>
  <h1>Pipery Deploy Bot</h1>
  <h2>Scheduled Deploys</h2>
  <table>
    <thead><tr><th>ID</th><th>Repo</th><th>Workflow</th><th>Ref</th><th>Scheduled</th><th>Status</th><th>Error</th></tr></thead>
    <tbody>{{range .Deploys}}<tr><td><code>{{.ID}}</code></td><td>{{.Owner}}/{{.Repo}}</td><td>{{.WorkflowID}}</td><td>{{.Ref}}</td><td>{{.ScheduledAt}}</td><td>{{.Status}}</td><td>{{.LastError}}</td></tr>{{else}}<tr><td colspan="7">No scheduled deploys.</td></tr>{{end}}</tbody>
  </table>
  <h2>Trigger Attempts</h2>
  <table>
    <thead><tr><th>ID</th><th>Deploy</th><th>Attempt</th><th>Status</th><th>GitHub</th><th>Requested</th><th>Error</th></tr></thead>
    <tbody>{{range .Attempts}}<tr><td><code>{{.ID}}</code></td><td><code>{{.DeployID}}</code></td><td>{{.AttemptNo}}</td><td>{{.Status}}</td><td>{{.GitHubStatus}}</td><td>{{.RequestedAt}}</td><td>{{.Error}}</td></tr>{{else}}<tr><td colspan="7">No trigger attempts.</td></tr>{{end}}</tbody>
  </table>
</body>
</html>`))
