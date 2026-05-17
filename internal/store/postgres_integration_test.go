package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

func TestPostgresRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("PIPERY_DEPLOY_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PIPERY_DEPLOY_POSTGRES_DSN to run Postgres integration test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	ctx := context.Background()
	schema := "pipery_deploy_bot_test_" + strings.ReplaceAll(newID(), "-", "_")
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "001_init.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	repo := NewPostgres(db)
	scheduledAt := time.Now().UTC().Add(-time.Minute)
	item := deploy.ScheduledDeploy{
		ID:              newID(),
		IdempotencyKey:  "deploy-key",
		InstallationKey: "default",
		InstallationID:  42,
		Owner:           "pipery-dev",
		Repo:            "example",
		WorkflowID:      "deploy.yml",
		Ref:             "main",
		Inputs:          map[string]string{"environment": "prod"},
		ScheduledAt:     scheduledAt,
	}

	created, isNew, err := repo.CreateScheduledDeploy(ctx, item)
	if err != nil {
		t.Fatalf("CreateScheduledDeploy returned error: %v", err)
	}
	if !isNew || created.Status != deploy.StatusPending {
		t.Fatalf("created = %+v isNew=%v", created, isNew)
	}
	existing, isNew, err := repo.CreateScheduledDeploy(ctx, item)
	if err != nil {
		t.Fatalf("duplicate CreateScheduledDeploy returned error: %v", err)
	}
	if isNew || existing.ID != created.ID {
		t.Fatalf("duplicate created = %+v isNew=%v, want existing %s", existing, isNew, created.ID)
	}

	claimed, err := repo.ClaimDue(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("ClaimDue returned error: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != created.ID || claimed[0].Status != deploy.StatusClaimed {
		t.Fatalf("claimed = %+v", claimed)
	}
	claimedAgain, err := repo.ClaimDue(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("second ClaimDue returned error: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("second ClaimDue = %+v, want none", claimedAgain)
	}

	attempt, err := repo.StartAttempt(ctx, created.ID)
	if err != nil {
		t.Fatalf("StartAttempt returned error: %v", err)
	}
	if err := repo.CompleteAttempt(ctx, attempt.ID, created.ID, deploy.StatusSucceeded, 204, ""); err != nil {
		t.Fatalf("CompleteAttempt returned error: %v", err)
	}
}
