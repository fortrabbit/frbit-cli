package credentials

import (
	"testing"
	"time"
)

func TestDecodeLegacyStringIsAPIToken(t *testing.T) {
	credential, err := Decode("frbit-at-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if credential.Kind != KindAPIToken || credential.AccessToken != "frbit-at-legacy" {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestEncodeDecodeOAuthCredential(t *testing.T) {
	expiresAt := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	encoded, err := Encode(Credential{Kind: KindOAuth, AccessToken: "access", RefreshToken: "refresh", ExpiresAt: expiresAt})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Kind != KindOAuth || credential.AccessToken != "access" || credential.RefreshToken != "refresh" || !credential.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestDecodeRejectsUnknownKind(t *testing.T) {
	if _, err := Decode(`{"kind":"password","accessToken":"x"}`); err == nil {
		t.Fatal("Decode() accepted an unknown credential kind")
	}
}
