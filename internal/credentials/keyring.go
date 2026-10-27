package credentials

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "frbit-cli"

type Store interface {
	Get(profile string) (Credential, error)
	Set(profile string, credential Credential) error
	Delete(profile string) error
}

type KeyringStore struct{}

func (KeyringStore) Get(profile string) (Credential, error) {
	value, err := keyring.Get(service, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("read credential from system keychain: %w", err)
	}
	return Decode(value)
}

func (KeyringStore) Set(profile string, credential Credential) error {
	value, err := Encode(credential)
	if err != nil {
		return err
	}
	if err := keyring.Set(service, profile, value); err != nil {
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
