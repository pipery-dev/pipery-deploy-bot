package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	ListenAddr        string
	SchedulerInterval time.Duration
	PGConfig          *pgx.ConnConfig
	Installations     map[string]GitHubAppInstall
	APIToken          string
}

type fileConfig struct {
	Installations map[string]GitHubAppInstall `json:"installations"`
}

type GitHubAppInstall struct {
	AppID          int64  `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	PrivateKeyFile string `json:"private_key_file"`
	PrivateKeyEnv  string `json:"private_key_env"`
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	pgConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	path := os.Getenv("PIPERY_DEPLOY_CONFIG")
	if path == "" {
		return Config{}, errors.New("PIPERY_DEPLOY_CONFIG is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var file fileConfig
	if err := json.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(file.Installations) == 0 {
		return Config{}, errors.New("at least one GitHub App installation is required")
	}
	for key, install := range file.Installations {
		if install.AppID == 0 || install.InstallationID == 0 {
			return Config{}, fmt.Errorf("installation %q requires app_id and installation_id", key)
		}
		if install.PrivateKeyFile == "" && install.PrivateKeyEnv == "" {
			return Config{}, fmt.Errorf("installation %q requires private_key_file or private_key_env", key)
		}
	}

	interval := 30 * time.Second
	if value := os.Getenv("SCHEDULER_INTERVAL"); value != "" {
		interval, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse SCHEDULER_INTERVAL: %w", err)
		}
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return Config{
		ListenAddr:        addr,
		SchedulerInterval: interval,
		PGConfig:          pgConfig,
		Installations:     file.Installations,
		APIToken:          os.Getenv("PIPERY_DEPLOY_API_TOKEN"),
	}, nil
}
