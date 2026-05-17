package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

func TestTickClaimsDueDeploysOnceAndDispatches(t *testing.T) {
	store := &memoryStore{
		due: []deploy.ScheduledDeploy{{
			ID:              "deploy-1",
			InstallationKey: "default",
			InstallationID:  42,
			Owner:           "pipery-dev",
			Repo:            "example",
			WorkflowID:      "deploy.yml",
			Ref:             "main",
			Inputs:          map[string]string{"environment": "prod"},
		}},
	}
	github := &workflowGitHub{status: 204}
	scheduler := New(store, github, time.Hour, 10)

	scheduler.tick(context.Background())
	scheduler.tick(context.Background())

	if store.claimCalls != 2 {
		t.Fatalf("ClaimDue calls = %d, want 2", store.claimCalls)
	}
	if len(github.dispatches) != 1 {
		t.Fatalf("dispatches = %d, want 1", len(github.dispatches))
	}
	if github.dispatches[0] != "default 42 pipery-dev/example deploy.yml main prod" {
		t.Fatalf("dispatch = %q", github.dispatches[0])
	}
	if len(store.completed) != 1 || store.completed[0] != "attempt-1 deploy-1 succeeded 204 " {
		t.Fatalf("completed = %#v", store.completed)
	}
}

func TestTickCompletesFailedDispatch(t *testing.T) {
	store := &memoryStore{
		due: []deploy.ScheduledDeploy{{ID: "deploy-1", WorkflowID: "deploy.yml"}},
	}
	github := &workflowGitHub{status: 422, err: errors.New("workflow missing")}
	scheduler := New(store, github, time.Hour, 10)

	scheduler.tick(context.Background())

	if len(store.completed) != 1 || store.completed[0] != "attempt-1 deploy-1 failed 422 workflow missing" {
		t.Fatalf("completed = %#v", store.completed)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	scheduler := New(&memoryStore{}, &workflowGitHub{}, 0, 0)
	if scheduler.interval != 30*time.Second {
		t.Fatalf("interval = %s, want 30s", scheduler.interval)
	}
	if scheduler.limit != 5 {
		t.Fatalf("limit = %d, want 5", scheduler.limit)
	}
}

type memoryStore struct {
	due        []deploy.ScheduledDeploy
	claimed    bool
	claimCalls int
	completed  []string
}

func (s *memoryStore) ClaimDue(_ context.Context, _ time.Time, _ int) ([]deploy.ScheduledDeploy, error) {
	s.claimCalls++
	if s.claimed {
		return nil, nil
	}
	s.claimed = true
	return append([]deploy.ScheduledDeploy(nil), s.due...), nil
}

func (s *memoryStore) StartAttempt(_ context.Context, deployID string) (deploy.TriggerAttempt, error) {
	return deploy.TriggerAttempt{ID: "attempt-1", DeployID: deployID, AttemptNo: 1, Status: "started"}, nil
}

func (s *memoryStore) CompleteAttempt(_ context.Context, attemptID, deployID string, status string, githubStatus int, message string) error {
	s.completed = append(s.completed, attemptID+" "+deployID+" "+status+" "+itoa(githubStatus)+" "+message)
	return nil
}

type workflowGitHub struct {
	status     int
	err        error
	dispatches []string
}

func (g *workflowGitHub) WorkflowDispatch(_ context.Context, installationKey string, installationID int64, owner, repo, workflowID, ref string, inputs map[string]string) (int, error) {
	g.dispatches = append(g.dispatches, installationKey+" "+itoa64(installationID)+" "+owner+"/"+repo+" "+workflowID+" "+ref+" "+inputs["environment"])
	return g.status, g.err
}

func itoa(n int) string {
	return itoa64(int64(n))
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
