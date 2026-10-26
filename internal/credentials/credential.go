package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	KindAPIToken = "api_token"
	KindOAuth    = "oauth"
)

var ErrNotFound = errors.New("no stored credential")

type Credential struct {
	Kind         string    `json:"kind"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitzero"`
	Issuer       string    `json:"issuer,omitempty"`
}

func APIToken(token string) Credential {
	return Credential{Kind: KindAPIToken, AccessToken: token}
}

// Encode keeps API tokens as plain strings so older frbit binaries can still read them.
func Encode(credential Credential) (string, error) {
	if credential.Kind == KindAPIToken {
		return credential.AccessToken, nil
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("encode credential: %w", err)
	}
	return string(encoded), nil
}

// Decode reads a stored credential; a plain string predates the JSON format and is an API token.
func Decode(value string) (Credential, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") {
		return APIToken(value), nil
	}
	var credential Credential
	if err := json.Unmarshal([]byte(value), &credential); err != nil {
		return Credential{}, fmt.Errorf("decode stored credential: %w", err)
	}
	switch credential.Kind {
	case KindAPIToken, KindOAuth:
		return credential, nil
	default:
		return Credential{}, fmt.Errorf("decode stored credential: unknown kind %q", credential.Kind)
	}
}
