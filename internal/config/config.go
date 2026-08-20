package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultHost         = "https://api.fortrabbit.com"
	DefaultDashboardURL = "https://dash.fortrabbit.com"
)

type Config struct {
	Host string `json:"host,omitempty"`
}

type Store interface {
	Load() (Config, error)
	Save(Config) error
}

type FileStore struct {
	Path string
}

func NewFileStore() (*FileStore, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user config directory: %w", err)
	}

	return &FileStore{Path: filepath.Join(configDir, "frbit", "config.json")}, nil
}

func (s FileStore) Load() (Config, error) {
	contents, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	return config, nil
}

func (s FileStore) Save(config Config) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	contents, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := os.WriteFile(s.Path, append(contents, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// ResolveHost uses flag > environment > global config > built-in default.
func ResolveHost(flagHost string, config Config) string {
	if host := strings.TrimSpace(flagHost); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv("FRBIT_HOST")); host != "" {
		return host
	}
	if host := strings.TrimSpace(config.Host); host != "" {
		return host
	}

	return DefaultHost
}

// APITokensURL resolves the dashboard origin from the environment and returns
// the page listing the current person's API tokens.
func APITokensURL() (string, error) {
	dashboardURL := strings.TrimSpace(os.Getenv("FRBIT_DASHBOARD_URL"))
	if dashboardURL == "" {
		dashboardURL = DefaultDashboardURL
	}

	parsed, err := url.Parse(dashboardURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid dashboard URL %q; provide an absolute HTTP(S) origin", dashboardURL)
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid dashboard URL %q; provide an origin without a path, query, or fragment", dashboardURL)
	}

	return strings.TrimRight(dashboardURL, "/") + "/you/api-tokens", nil
}
