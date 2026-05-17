package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

type Postgres struct {
	db *sql.DB
}

const claimLease = 15 * time.Minute

func NewPostgres(db *sql.DB) *Postgres {
	return &Postgres{db: db}
}

func (p *Postgres) CreateScheduledDeploy(ctx context.Context, d deploy.ScheduledDeploy) (deploy.ScheduledDeploy, bool, error) {
	inputs, err := json.Marshal(d.Inputs)
	if err != nil {
		return deploy.ScheduledDeploy{}, false, err
	}
	if d.Status == "" {
		d.Status = deploy.StatusPending
	}

	row := p.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_deploys (
			id, idempotency_key, installation_key, installation_id, owner, repo,
			workflow_id, ref, inputs, scheduled_at, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, idempotency_key, installation_key, installation_id, owner, repo,
			workflow_id, ref, inputs, scheduled_at, status, last_error, created_at, updated_at`,
		d.ID, d.IdempotencyKey, d.InstallationKey, d.InstallationID, d.Owner, d.Repo,
		d.WorkflowID, d.Ref, inputs, d.ScheduledAt.UTC(), d.Status,
	)
	created, err := scanDeploy(row)
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return deploy.ScheduledDeploy{}, false, err
	}

	existing, err := p.GetScheduledDeployByIdempotencyKey(ctx, d.IdempotencyKey)
	return existing, false, err
}

func (p *Postgres) GetScheduledDeployByIdempotencyKey(ctx context.Context, key string) (deploy.ScheduledDeploy, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT id, idempotency_key, installation_key, installation_id, owner, repo,
			workflow_id, ref, inputs, scheduled_at, status, last_error, created_at, updated_at
		FROM scheduled_deploys
		WHERE idempotency_key = $1`, key)
	return scanDeploy(row)
}

func (p *Postgres) ListScheduledDeploys(ctx context.Context, status string, limit int) ([]deploy.ScheduledDeploy, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id, idempotency_key, installation_key, installation_id, owner, repo,
			workflow_id, ref, inputs, scheduled_at, status, last_error, created_at, updated_at
		FROM scheduled_deploys`
	args := []any{}
	if status != "" {
		query += ` WHERE status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY scheduled_at ASC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []deploy.ScheduledDeploy
	for rows.Next() {
		item, err := scanDeploy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *Postgres) ListTriggerAttempts(ctx context.Context, deployID string, limit int) ([]deploy.TriggerAttempt, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id, deploy_id, attempt_no, status, github_status, error, requested_at, completed_at
		FROM trigger_attempts`
	args := []any{}
	if deployID != "" {
		query += ` WHERE deploy_id = $1`
		args = append(args, deployID)
	}
	query += ` ORDER BY requested_at DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []deploy.TriggerAttempt
	for rows.Next() {
		item, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *Postgres) ClaimDue(ctx context.Context, now time.Time, limit int) ([]deploy.ScheduledDeploy, error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		UPDATE scheduled_deploys
		SET status = 'claimed', claimed_at = $1, updated_at = $1
		WHERE id IN (
			SELECT id
			FROM scheduled_deploys
			WHERE scheduled_at <= $1
				AND (
					status = 'pending'
					OR (status = 'claimed' AND claimed_at < $1 - ($3 * interval '1 second'))
				)
			ORDER BY scheduled_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		RETURNING id, idempotency_key, installation_key, installation_id, owner, repo,
			workflow_id, ref, inputs, scheduled_at, status, last_error, created_at, updated_at`,
		now.UTC(), limit, int(claimLease.Seconds()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claimed []deploy.ScheduledDeploy
	for rows.Next() {
		item, err := scanDeploy(rows)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return claimed, tx.Commit()
}

func (p *Postgres) StartAttempt(ctx context.Context, deployID string) (deploy.TriggerAttempt, error) {
	row := p.db.QueryRowContext(ctx, `
		INSERT INTO trigger_attempts (id, deploy_id, attempt_no, status)
		VALUES ($1, $2, 1, 'started')
		ON CONFLICT (deploy_id, attempt_no) DO UPDATE
			SET id = trigger_attempts.id
		RETURNING id, deploy_id, attempt_no, status, github_status, error, requested_at, completed_at`,
		newID(), deployID,
	)
	return scanAttempt(row)
}

func (p *Postgres) CompleteAttempt(ctx context.Context, attemptID, deployID string, status string, githubStatus int, message string) error {
	finalDeployStatus := deploy.StatusSucceeded
	if status != deploy.StatusSucceeded {
		finalDeployStatus = deploy.StatusFailed
	}

	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE trigger_attempts
		SET status = $1, github_status = $2, error = $3, completed_at = $4
		WHERE id = $5`,
		status, githubStatus, message, now, attemptID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduled_deploys
		SET status = $1, last_error = $2, completed_at = $3, updated_at = $3
		WHERE id = $4`,
		finalDeployStatus, message, now, deployID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDeploy(row scanner) (deploy.ScheduledDeploy, error) {
	var item deploy.ScheduledDeploy
	var inputs []byte
	if err := row.Scan(
		&item.ID,
		&item.IdempotencyKey,
		&item.InstallationKey,
		&item.InstallationID,
		&item.Owner,
		&item.Repo,
		&item.WorkflowID,
		&item.Ref,
		&inputs,
		&item.ScheduledAt,
		&item.Status,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return deploy.ScheduledDeploy{}, err
	}
	if len(inputs) > 0 {
		if err := json.Unmarshal(inputs, &item.Inputs); err != nil {
			return deploy.ScheduledDeploy{}, err
		}
	}
	item.ScheduledAt = item.ScheduledAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func scanAttempt(row scanner) (deploy.TriggerAttempt, error) {
	var item deploy.TriggerAttempt
	var completed sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.DeployID,
		&item.AttemptNo,
		&item.Status,
		&item.GitHubStatus,
		&item.Error,
		&item.RequestedAt,
		&completed,
	); err != nil {
		return deploy.TriggerAttempt{}, err
	}
	item.RequestedAt = item.RequestedAt.UTC()
	if completed.Valid {
		item.CompletedAt = completed.Time.UTC()
	}
	return item, nil
}
