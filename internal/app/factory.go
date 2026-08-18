package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

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
}

func NewFactory(version string, commit string, date string) *Factory {
	configStore, err := config.NewFileStore()
	if err != nil {
		panic(fmt.Sprintf("initialize config store: %v", err))
	}

	httpClient := &http.Client{}
	checker := update.NewChecker(httpClient)

	return &Factory{
		IOStreams:       iostreams.System(),
		ConfigStore:     configStore,
		CredentialStore: credentials.KeyringStore{},
		HTTPClient:      httpClient,
		Version:         version,
		Commit:          commit,
		Date:            date,
		CheckForUpdate:  checker.Latest,
		OpenBrowser:     browser.Open,
	}
}

func (f Factory) Host(flagHost string) (string, error) {
	stored, err := f.ConfigStore.Load()
	if err != nil {
		return "", err
	}
	return config.ResolveHost(flagHost, stored), nil
}

// Token resolves the environment value before the secure local credential.
func (f Factory) Token(profile string) (string, string, error) {
	if token := strings.TrimSpace(os.Getenv("FRBIT_TOKEN")); token != "" {
		return token, "FRBIT_TOKEN", nil
	}

	token, err := f.CredentialStore.Get(profile)
	if err != nil {
		return "", "", fmt.Errorf("no API token is available; set FRBIT_TOKEN or run `frbit auth login`: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", "", errors.New("no API token is available; set FRBIT_TOKEN or run `frbit auth login`")
	}

	return token, "system keychain", nil
}
