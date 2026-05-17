package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestWorkflowDispatchUsesGitHubAPIShape(t *testing.T) {
	var seen seenRequest
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen.Method = r.Method
		seen.Path = r.URL.EscapedPath()
		seen.Authorization = r.Header.Get("Authorization")
		seen.Accept = r.Header.Get("Accept")
		seen.APIVersion = r.Header.Get("X-GitHub-Api-Version")
		seen.ContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&seen.Body); err != nil {
			t.Fatalf("decode dispatch body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}, staticAuth{token: "test-token"})

	status, err := client.WorkflowDispatch(context.Background(), "default", 7, "pipery-dev", "example", "deploy workflow.yml", "main", map[string]string{"environment": "prod"})
	if err != nil {
		t.Fatalf("WorkflowDispatch returned error: %v", err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
	if seen.Method != http.MethodPost || seen.Path != "/repos/pipery-dev/example/actions/workflows/deploy%20workflow.yml/dispatches" {
		t.Fatalf("request = %s %s", seen.Method, seen.Path)
	}
	if seen.Authorization != "Bearer test-token" || seen.Accept != "application/vnd.github+json" || seen.APIVersion != "2022-11-28" || seen.ContentType != "application/json" {
		t.Fatalf("unexpected headers: %+v", seen)
	}
	if seen.Body["ref"] != "main" {
		t.Fatalf("ref body = %#v", seen.Body["ref"])
	}
	inputs, ok := seen.Body["inputs"].(map[string]any)
	if !ok || inputs["environment"] != "prod" {
		t.Fatalf("inputs body = %#v", seen.Body["inputs"])
	}
}

func TestWorkflowDispatchReturnsGitHubStatusOnError(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       io.NopCloser(strings.NewReader("workflow not found")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}, staticAuth{token: "test-token"})

	status, err := client.WorkflowDispatch(context.Background(), "default", 7, "pipery-dev", "example", "deploy.yml", "main", nil)
	if err == nil {
		t.Fatal("expected GitHub error")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

type staticAuth struct {
	token string
}

func (a staticAuth) Token(context.Context, string, int64) (string, error) {
	return a.token, nil
}

type seenRequest struct {
	Method        string
	Path          string
	Authorization string
	Accept        string
	APIVersion    string
	ContentType   string
	Body          map[string]any
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
