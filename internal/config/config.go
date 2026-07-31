package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultHost = "https://api.fortrabbit.com"

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
