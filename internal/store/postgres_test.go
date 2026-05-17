package store

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/pipery-dev/pipery-deploy-bot/internal/deploy"
)

func TestScanDeployParsesInputsAndNormalizesTimes(t *testing.T) {
	local := time.FixedZone("CEST", 2*60*60)
	row := fakeRow{values: []any{
		"deploy-1",
		"deploy-key",
		"default",
		int64(42),
		"pipery-dev",
		"example",
		"deploy.yml",
		"main",
		[]byte(`{"environment":"prod"}`),
		time.Date(2026, 5, 17, 14, 0, 0, 0, local),
		deploy.StatusPending,
		"",
		time.Date(2026, 5, 17, 13, 0, 0, 0, local),
		time.Date(2026, 5, 17, 13, 30, 0, 0, local),
	}}

	item, err := scanDeploy(row)
	if err != nil {
		t.Fatalf("scanDeploy returned error: %v", err)
	}
	if item.Inputs["environment"] != "prod" {
		t.Fatalf("Inputs = %#v", item.Inputs)
	}
	if item.ScheduledAt.Location() != time.UTC || item.CreatedAt.Location() != time.UTC || item.UpdatedAt.Location() != time.UTC {
		t.Fatalf("times were not normalized to UTC: %+v", item)
	}
	if !item.ScheduledAt.Equal(time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("ScheduledAt = %s, want matching UTC instant", item.ScheduledAt)
	}
}

func TestScanDeployRejectsInvalidInputsJSON(t *testing.T) {
	row := fakeRow{values: []any{
		"deploy-1", "deploy-key", "default", int64(42), "pipery-dev", "example",
		"deploy.yml", "main", []byte(`{`), time.Now(), deploy.StatusPending, "",
		time.Now(), time.Now(),
	}}

	if _, err := scanDeploy(row); err == nil {
		t.Fatal("expected invalid inputs JSON error")
	}
}

func TestScanAttemptNormalizesCompletedAt(t *testing.T) {
	local := time.FixedZone("CEST", 2*60*60)
	row := fakeRow{values: []any{
		"attempt-1",
		"deploy-1",
		1,
		deploy.StatusSucceeded,
		204,
		"",
		time.Date(2026, 5, 17, 14, 0, 0, 0, local),
		sql.NullTime{Time: time.Date(2026, 5, 17, 14, 1, 0, 0, local), Valid: true},
	}}

	attempt, err := scanAttempt(row)
	if err != nil {
		t.Fatalf("scanAttempt returned error: %v", err)
	}
	if attempt.RequestedAt.Location() != time.UTC || attempt.CompletedAt.Location() != time.UTC {
		t.Fatalf("attempt times were not normalized to UTC: %+v", attempt)
	}
}

type fakeRow struct {
	values []any
}

func (r fakeRow) Scan(dest ...any) error {
	for i := range dest {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Pointer || dv.IsNil() {
			panic("destination must be a non-nil pointer")
		}
		value := reflect.ValueOf(r.values[i])
		dv.Elem().Set(value)
	}
	return nil
}
