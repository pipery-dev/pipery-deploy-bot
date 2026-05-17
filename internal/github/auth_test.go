package github

import (
	"context"
	"testing"

	"github.com/pipery-dev/pipery-deploy-bot/internal/config"
)

func TestTokenRejectsInstallationIDOverride(t *testing.T) {
	auth := NewAppAuthenticator(nil, map[string]config.GitHubAppInstall{
		"default": {AppID: 1, InstallationID: 100, PrivateKeyEnv: "UNUSED"},
	})

	_, err := auth.Token(context.Background(), "default", 200)
	if err == nil {
		t.Fatal("expected installation_id override to be rejected")
	}
}
