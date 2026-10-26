package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/browser"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/update"
)

const DefaultProfile = "default"

type Factory struct {
	IOStreams       iostreams.IOStreams
	ConfigStore     config.Store
	CredentialStore credentials.Store
	HTTPClient      *http.Client
	Version         string
	Commit          string
	Date            string
	CheckForUpdate  func(context.Context, string) (string, error)
	OpenBrowser     func(string) error
	// CredentialLockPath serialises OAuth refreshes across frbit processes.
	CredentialLockPath string
}

func NewFactory(version string, commit string, date string) *Factory {
	configStore, err := config.NewFileStore()
	if err != nil {
		panic(fmt.Sprintf("initialize config store: %v", err))
	}

	// This client is also used by skills downloads, which do not pass through
	// api.NewClient's per-origin timeout wrapper.
	httpClient := &http.Client{Timeout: 30 * time.Second}
	checker := update.NewChecker(httpClient)

	return &Factory{
		IOStreams:          iostreams.System(),
		ConfigStore:        configStore,
		CredentialStore:    credentials.KeyringStore{},
		HTTPClient:         httpClient,
		Version:            version,
		Commit:             commit,
		Date:               date,
		CheckForUpdate:     checker.Latest,
		OpenBrowser:        browser.Open,
		CredentialLockPath: filepath.Join(filepath.Dir(configStore.Path), "credentials.lock"),
	}
}

func (f Factory) Host(flagHost string) (string, error) {
	stored, err := f.ConfigStore.Load()
	if err != nil {
		return "", err
	}
	return config.ResolveHost(flagHost, stored), nil
}

// Credential resolves the environment value before the secure local credential.
func (f Factory) Credential(profile string) (credentials.Credential, string, error) {
	if token := strings.TrimSpace(os.Getenv("FRBIT_TOKEN")); token != "" {
		return credentials.APIToken(token), "FRBIT_TOKEN", nil
	}

	credential, err := f.CredentialStore.Get(profile)
	if err != nil {
		return credentials.Credential{}, "", fmt.Errorf("no API token is available; set FRBIT_TOKEN or run `frbit auth login`: %w", err)
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return credentials.Credential{}, "", fmt.Errorf("no API token is available; set FRBIT_TOKEN or run `frbit auth login`: %w", credentials.ErrNotFound)
	}

	return credential, "system keychain", nil
}
