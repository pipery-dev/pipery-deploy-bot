package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	httpClient *http.Client
	auth       Authenticator
	baseURL    string
}

func NewClient(httpClient *http.Client, auth Authenticator) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient, auth: auth, baseURL: "https://api.github.com"}
}

func (c *Client) WorkflowDispatch(ctx context.Context, installationKey string, installationID int64, owner, repo, workflowID, ref string, inputs map[string]string) (int, error) {
	body := map[string]any{
		"ref":    ref,
		"inputs": inputs,
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowID))
	return c.do(ctx, installationKey, installationID, http.MethodPost, path, body)
}

func (c *Client) do(ctx context.Context, installationKey string, installationID int64, method, path string, in any) (int, error) {
	token, err := c.auth.Token(ctx, installationKey, installationID)
	if err != nil {
		return 0, err
	}

	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, fmt.Errorf("github %s %s returned %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
