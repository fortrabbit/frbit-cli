package credentials

import (
	"strings"
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

func TestEncodeKeepsAPITokenAsPlainString(t *testing.T) {
	encoded, err := Encode(APIToken("frbit-at-plain"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "frbit-at-plain" {
		t.Fatalf("encoded = %q", encoded)
	}
	credential, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if credential != APIToken("frbit-at-plain") {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestEncodeDecodeOAuthCredential(t *testing.T) {
	expiresAt := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	original := Credential{Kind: KindOAuth, AccessToken: "access", RefreshToken: "refresh", ExpiresAt: expiresAt, Issuer: "https://api.fortrabbit.com"}
	encoded, err := Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "{") {
		t.Fatalf("encoded = %q, want JSON", encoded)
	}
	credential, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if credential != original {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestDecodeRejectsUnknownKind(t *testing.T) {
	if _, err := Decode(`{"kind":"password","accessToken":"x"}`); err == nil {
		t.Fatal("Decode() accepted an unknown credential kind")
	}
}
