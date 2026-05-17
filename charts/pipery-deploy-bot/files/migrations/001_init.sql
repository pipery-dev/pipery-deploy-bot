CREATE TABLE IF NOT EXISTS scheduled_deploys (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    installation_key TEXT NOT NULL,
    installation_id BIGINT NOT NULL DEFAULT 0,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    inputs JSONB NOT NULL DEFAULT '{}'::jsonb,
    scheduled_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'claimed', 'succeeded', 'failed')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS scheduled_deploys_due_idx
    ON scheduled_deploys (scheduled_at)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS trigger_attempts (
    id TEXT PRIMARY KEY,
    deploy_id TEXT NOT NULL REFERENCES scheduled_deploys(id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('started', 'succeeded', 'failed')),
    github_status INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    UNIQUE (deploy_id, attempt_no)
);

CREATE INDEX IF NOT EXISTS trigger_attempts_deploy_id_idx
    ON trigger_attempts (deploy_id, requested_at DESC);
