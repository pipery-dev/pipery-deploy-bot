package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

type Store interface {
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]deploy.ScheduledDeploy, error)
	StartAttempt(ctx context.Context, deployID string) (deploy.TriggerAttempt, error)
	CompleteAttempt(ctx context.Context, attemptID, deployID string, status string, githubStatus int, message string) error
}

type GitHub interface {
	WorkflowDispatch(ctx context.Context, installationKey string, installationID int64, owner, repo, workflowID, ref string, inputs map[string]string) (int, error)
}

type Scheduler struct {
	store    Store
	github   GitHub
	interval time.Duration
	limit    int
}

func New(store Store, github GitHub, interval time.Duration, limit int) *Scheduler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if limit <= 0 {
		limit = 5
	}
	return &Scheduler{store: store, github: github, interval: interval, limit: limit}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.tick(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	items, err := s.store.ClaimDue(ctx, time.Now().UTC(), s.limit)
	if err != nil {
		log.Printf("claim due deploys: %v", err)
		return
	}
	for _, item := range items {
		s.trigger(ctx, item)
	}
}

func (s *Scheduler) trigger(ctx context.Context, item deploy.ScheduledDeploy) {
	attempt, err := s.store.StartAttempt(ctx, item.ID)
	if err != nil {
		log.Printf("start attempt for deploy %s: %v", item.ID, err)
		return
	}

	statusCode, err := s.github.WorkflowDispatch(ctx, item.InstallationKey, item.InstallationID, item.Owner, item.Repo, item.WorkflowID, item.Ref, item.Inputs)
	if err != nil {
		if completeErr := s.store.CompleteAttempt(ctx, attempt.ID, item.ID, deploy.StatusFailed, statusCode, err.Error()); completeErr != nil {
			log.Printf("complete failed attempt %s: %v", attempt.ID, completeErr)
		}
		return
	}
	if err := s.store.CompleteAttempt(ctx, attempt.ID, item.ID, deploy.StatusSucceeded, statusCode, ""); err != nil {
		log.Printf("complete succeeded attempt %s: %v", attempt.ID, err)
	}
}
