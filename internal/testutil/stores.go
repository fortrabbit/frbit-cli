// Package testutil holds stubs that command tests share.
package testutil

import (
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
)

// ConfigStore loads an empty config and saves nothing.
type ConfigStore struct{}

func (ConfigStore) Load() (config.Config, error) { return config.Config{}, nil }
func (ConfigStore) Save(config.Config) error     { return nil }

// CredentialStore holds no credential, so FRBIT_TOKEN is used.
type CredentialStore struct{}

func (CredentialStore) Get(string) (credentials.Credential, error) {
	return credentials.Credential{}, credentials.ErrNotFound
}
func (CredentialStore) Set(string, credentials.Credential) error { return nil }
func (CredentialStore) Delete(string) error                      { return nil }
