package cmdutil

import (
	"errors"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
)

func TestLoginCommandAddsOnlyNonDefaultFlags(t *testing.T) {
	cases := map[[2]string]string{
		{"https://api.fortrabbit.com", "default"}: "frbit auth login",
		{"https://api.fortrabbit.com", ""}:        "frbit auth login",
		{"https://api.fortrabbit.com", "work"}:    "frbit auth login --profile work",
		{"http://localhost:8080", "default"}:      "frbit auth login --host http://localhost:8080",
		{"https://api.fortrabbit.com", "my work"}: "frbit auth login --profile 'my work'",
		{"https://api.fortrabbit.com", "it's"}:    `frbit auth login --profile 'it'\''s'`,
	}
	for input, want := range cases {
		if got := LoginCommand(input[0], input[1]); got != want {
			t.Errorf("LoginCommand(%q, %q) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

func TestCredentialClientKnowsAnEnvironmentToken(t *testing.T) {
	factory := &app.Factory{}
	t.Setenv("FRBIT_TOKEN", " frbit-at-env ")
	client, err := CredentialClient(factory, "https://api.fortrabbit.com", "work", credentials.APIToken("frbit-at-env"))
	if err != nil {
		t.Fatal(err)
	}
	if !client.SignIn.FromEnv || client.SignIn.Login != "frbit auth login --profile work" {
		t.Fatalf("SignIn = %+v", client.SignIn)
	}
	stored, err := CredentialClient(factory, "https://api.fortrabbit.com", "work", credentials.APIToken("frbit-at-stored"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.SignIn.FromEnv {
		t.Fatal("a stored token counted as FRBIT_TOKEN")
	}
	signIn := &api.SignInError{Hint: client.SignIn, Err: errors.New("HTTP 401")}
	if want := "the API token in FRBIT_TOKEN is invalid, expired or revoked; set a valid token, or unset FRBIT_TOKEN and run `frbit auth login --profile work` (HTTP 401)"; signIn.Error() != want {
		t.Fatalf("SignInError = %q", signIn.Error())
	}
}
