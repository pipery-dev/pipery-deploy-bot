package httpapi

import "testing"

func TestRequestToDeployRequiresUTC(t *testing.T) {
	_, err := requestToDeploy(createDeployRequest{
		IdempotencyKey: "deploy-1",
		Owner:          "pipery-dev",
		Repo:           "example",
		WorkflowID:     "deploy.yml",
		Ref:            "main",
		ScheduledAt:    "2026-05-17T12:00:00+02:00",
	})
	if err == nil {
		t.Fatal("expected non-UTC scheduled_at to be rejected")
	}
}

func TestRequestToDeployParsesRFC3339UTC(t *testing.T) {
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
	if item.ScheduledAt.Location().String() != "UTC" {
		t.Fatalf("ScheduledAt location = %s, want UTC", item.ScheduledAt.Location())
	}
}
