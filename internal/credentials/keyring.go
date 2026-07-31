package credentials

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "frbit-cli"

type Store interface {
	Get(profile string) (string, error)
	Set(profile string, token string) error
	Delete(profile string) error
}

type KeyringStore struct{}

func (KeyringStore) Get(profile string) (string, error) {
	token, err := keyring.Get(service, profile)
	if err != nil {
		return "", fmt.Errorf("read credential from system keychain: %w", err)
	}
	return token, nil
}

func (KeyringStore) Set(profile string, token string) error {
	if err := keyring.Set(service, profile, token); err != nil {
		return fmt.Errorf("save credential to system keychain: %w", err)
	}
	return nil
}

func (KeyringStore) Delete(profile string) error {
	if err := keyring.Delete(service, profile); err != nil {
		return fmt.Errorf("delete credential from system keychain: %w", err)
	}
	return nil
}
