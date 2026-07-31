package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
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
}

func NewFactory(version string, commit string, date string) *Factory {
	configStore, err := config.NewFileStore()
	if err != nil {
		panic(fmt.Sprintf("initialize config store: %v", err))
	}

	return &Factory{
		IOStreams:       iostreams.System(),
		ConfigStore:     configStore,
		CredentialStore: credentials.KeyringStore{},
		HTTPClient:      &http.Client{},
		Version:         version,
		Commit:          commit,
		Date:            date,
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
