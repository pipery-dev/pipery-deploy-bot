package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pipery-dev/pipery-deploy-bot/internal/config"
)

type Authenticator interface {
	Token(ctx context.Context, installationKey string, installationID int64) (string, error)
}

type AppAuthenticator struct {
	httpClient    *http.Client
	installations map[string]config.GitHubAppInstall
	mu            sync.Mutex
	cache         map[string]cachedToken
	now           func() time.Time
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

func NewAppAuthenticator(httpClient *http.Client, installations map[string]config.GitHubAppInstall) *AppAuthenticator {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &AppAuthenticator{
		httpClient:    httpClient,
		installations: installations,
		cache:         map[string]cachedToken{},
		now:           time.Now,
	}
}

func (a *AppAuthenticator) Token(ctx context.Context, installationKey string, installationID int64) (string, error) {
	install, ok := a.installations[installationKey]
	if !ok {
		return "", fmt.Errorf("unknown installation %q", installationKey)
	}
	if installationID != 0 && installationID != install.InstallationID {
		return "", fmt.Errorf("installation_id does not match configured installation %q", installationKey)
	}
	cacheKey := installationKey + ":" + strconv.FormatInt(install.InstallationID, 10)

	a.mu.Lock()
	if token, ok := a.cache[cacheKey]; ok && a.now().Before(token.expiresAt.Add(-1*time.Minute)) {
		a.mu.Unlock()
		return token.token, nil
	}
	a.mu.Unlock()

	jwt, err := a.jwt(install)
	if err != nil {
		return "", err
	}

	body := bytes.NewBufferString("{}")
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", install.InstallationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("create installation token: github returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var parsed struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("github returned an empty installation token")
	}

	a.mu.Lock()
	a.cache[cacheKey] = cachedToken{token: parsed.Token, expiresAt: parsed.ExpiresAt}
	a.mu.Unlock()
	return parsed.Token, nil
}

func (a *AppAuthenticator) jwt(install config.GitHubAppInstall) (string, error) {
	keyData, err := privateKeyData(install)
	if err != nil {
		return "", err
	}
	key, err := parsePrivateKey(keyData)
	if err != nil {
		return "", err
	}

	now := a.now().UTC()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":%d}`, now.Add(-30*time.Second).Unix(), now.Add(9*time.Minute).Unix(), install.AppID)
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signingInput := header + "." + payload
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func privateKeyData(install config.GitHubAppInstall) ([]byte, error) {
	if install.PrivateKeyEnv != "" {
		value := os.Getenv(install.PrivateKeyEnv)
		if value == "" {
			return nil, fmt.Errorf("environment variable %s is empty", install.PrivateKeyEnv)
		}
		return []byte(value), nil
	}
	return os.ReadFile(install.PrivateKeyFile)
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key must be PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return key, nil
}
