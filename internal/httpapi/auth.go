package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type AuthConfig struct {
	APIToken  string
	IssuerURL string
	ClientID  string
}

type tokenVerifier interface {
	Verify(context.Context, string) (*oidc.IDToken, error)
}

type authenticator struct {
	apiToken string
	verifier tokenVerifier
}

func NewAuthenticator(ctx context.Context, cfg AuthConfig) (*authenticator, error) {
	auth := &authenticator{apiToken: cfg.APIToken}
	if cfg.IssuerURL == "" && cfg.ClientID == "" {
		return auth, nil
	}
	if cfg.IssuerURL == "" || cfg.ClientID == "" {
		return nil, errors.New("Dex OIDC auth requires issuer URL and client ID")
	}
	provider, err := oidc.NewProvider(ctx, strings.TrimRight(cfg.IssuerURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("discover Dex issuer: %w", err)
	}
	auth.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return auth, nil
}

func (a *authenticator) Enabled() bool {
	return a != nil && (a.apiToken != "" || a.verifier != nil)
}

func (a *authenticator) Authorize(ctx context.Context, header string) bool {
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		return false
	}
	if a.apiToken != "" && token == a.apiToken {
		return true
	}
	if a.verifier == nil {
		return false
	}
	_, err := a.verifier.Verify(ctx, token)
	return err == nil
}

func bearerAuthError(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="pipery-deploy-bot"`)
	writeError(w, http.StatusUnauthorized, "unauthorized")
}
