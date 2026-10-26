package credentials

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	KindAPIToken = "api_token"
	KindOAuth    = "oauth"
)

type Credential struct {
	Kind         string    `json:"kind"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitzero"`
}

func APIToken(token string) Credential {
	return Credential{Kind: KindAPIToken, AccessToken: token}
}

func Encode(credential Credential) (string, error) {
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
