package root

import (
	"bytes"
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
