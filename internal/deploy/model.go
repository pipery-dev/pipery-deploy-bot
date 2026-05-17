package deploy

import "time"

const (
	StatusPending   = "pending"
	StatusClaimed   = "claimed"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

type ScheduledDeploy struct {
	ID              string            `json:"id"`
	IdempotencyKey  string            `json:"idempotency_key"`
	InstallationKey string            `json:"installation_key"`
	InstallationID  int64             `json:"installation_id"`
	Owner           string            `json:"owner"`
	Repo            string            `json:"repo"`
	WorkflowID      string            `json:"workflow_id"`
	Ref             string            `json:"ref"`
	Inputs          map[string]string `json:"inputs"`
	ScheduledAt     time.Time         `json:"scheduled_at"`
	Status          string            `json:"status"`
	LastError       string            `json:"last_error,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type TriggerAttempt struct {
	ID           string    `json:"id"`
	DeployID     string    `json:"deploy_id"`
	AttemptNo    int       `json:"attempt_no"`
	Status       string    `json:"status"`
	GitHubStatus int       `json:"github_status"`
	Error        string    `json:"error,omitempty"`
	RequestedAt  time.Time `json:"requested_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
}
