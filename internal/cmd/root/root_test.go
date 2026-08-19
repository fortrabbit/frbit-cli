package root

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

type memoryConfigStore struct {
	config config.Config
}

func (s *memoryConfigStore) Load() (config.Config, error) { return s.config, nil }
func (s *memoryConfigStore) Save(value config.Config) error {
	s.config = value
	return nil
}

type memoryCredentialStore struct {
	tokens map[string]string
}

func (s *memoryCredentialStore) Get(profile string) (string, error) {
	token, ok := s.tokens[profile]
	if !ok {
		return "", fmt.Errorf("credential not found")
	}
	return token, nil
}
func (s *memoryCredentialStore) Set(profile string, token string) error {
	s.tokens[profile] = token
	return nil
}
func (s *memoryCredentialStore) Delete(profile string) error {
	delete(s.tokens, profile)
	return nil
}

func TestAppsListRendersTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/apps" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`[{"publicId":"ap-abc123","name":"Store","description":"Example app","trial":false,"updatedAt":"2026-01-02T00:00:00Z"}]`))
	}))
	defer server.Close()

	output := &bytes.Buffer{}
	factory := testFactory(output)
	t.Setenv("FRBIT_TOKEN", "test-token")
	command := NewCmdRoot(factory)
	command.SetArgs([]string{"--host", server.URL, "apps", "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	got := output.String()
	for _, expected := range []string{"ID", "ap-abc123", "Store", "Example app"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output %q does not contain %q", got, expected)
		}
	}
}

func TestAppsListJSONPreservesAPIResponse(t *testing.T) {
	const payload = `[{"publicId":"ap-abc123","name":"Store","trial":false}]`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(payload))
	}))
	defer server.Close()

	output := &bytes.Buffer{}
	factory := testFactory(output)
	t.Setenv("FRBIT_TOKEN", "test-token")
	command := NewCmdRoot(factory)
	command.SetArgs([]string{"--host", server.URL, "apps", "list", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(output.String()); got != payload {
		t.Fatalf("json output = %q, want %q", got, payload)
	}
}

func TestPublicReadCommandsUseExpectedEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/environments":
			if got := request.URL.Query()["publicId[]"]; len(got) != 2 || got[0] != "en-abc123" || got[1] != "en-def456" {
				t.Fatalf("environment filter = %#v", got)
			}
			_, _ = writer.Write([]byte(`[{"publicId":"en-abc123","name":"production","state":"ready"}]`))
		case "/v1/people/pn-abc123":
			_, _ = writer.Write([]byte(`{"publicId":"pn-abc123","name":"Ada","email":"ada@example.test","active":true}`))
		case "/v1/deployments/dp-abc123/logs":
			_, _ = writer.Write([]byte(`{"logs":[{"time":"2026-01-01T00:00:00Z","log":"Build started"}]}`))
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"--host", server.URL, "environments", "list", "--id", "en-abc123", "--id", "en-def456"},
		{"--host", server.URL, "people", "get", "pn-abc123"},
		{"--host", server.URL, "deployments", "logs", "dp-abc123"},
	} {
		output := &bytes.Buffer{}
		factory := testFactory(output)
		t.Setenv("FRBIT_TOKEN", "test-token")
		command := NewCmdRoot(factory)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if output.Len() == 0 {
			t.Fatalf("%v: no output", args)
		}
	}
}

func TestInteractiveCommandShowsUpdateNotice(t *testing.T) {
	output := &bytes.Buffer{}
	errorOutput := &bytes.Buffer{}
	factory := testFactory(output)
	factory.IOStreams.ErrOut = errorOutput
	factory.IOStreams.IsErrTTY = true
	factory.Version = "1.0.0"
	factory.CheckForUpdate = func(ctx context.Context, current string) (string, error) {
		if current != "1.0.0" {
			t.Fatalf("current = %q", current)
		}
		return "v1.1.0", nil
	}

	command := NewCmdRoot(factory)
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := errorOutput.String(); !strings.Contains(got, "Update available: v1.0.0 → v1.1.0") {
		t.Fatalf("update output = %q", got)
	}
}

func TestRootRegistersSkillsLifecycle(t *testing.T) {
	command := NewCmdRoot(testFactory(&bytes.Buffer{}))
	for _, arguments := range [][]string{
		{"skills", "install"},
		{"skills", "update"},
		{"skills", "list"},
		{"skills", "remove"},
	} {
		found, remaining, err := command.Find(arguments)
		if err != nil {
			t.Fatalf("find %v: %v", arguments, err)
		}
		if found.Name() != arguments[len(arguments)-1] || len(remaining) != 0 {
			t.Fatalf("find %v returned command %q and remaining %#v", arguments, found.Name(), remaining)
		}
	}
}

func testFactory(output *bytes.Buffer) *app.Factory {
	return &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: &bytes.Buffer{}, IsTTY: false},
		ConfigStore:     &memoryConfigStore{},
		CredentialStore: &memoryCredentialStore{tokens: map[string]string{}},
		HTTPClient:      &http.Client{},
		Version:         "test",
		Commit:          "test",
		Date:            "test",
	}
}
