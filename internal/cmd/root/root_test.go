package root

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRootRegistersAgentCommands(t *testing.T) {
	command := NewCmdRoot(testFactory(&bytes.Buffer{}))
	for _, arguments := range [][]string{
		{"setup", "agent"},
		{"mcp", "install"},
		{"mcp", "list"},
		{"mcp", "remove"},
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

func TestRootRegistersPublicWriteCommands(t *testing.T) {
	command := NewCmdRoot(testFactory(&bytes.Buffer{}))
	for _, arguments := range [][]string{
		{"apps", "create"},
		{"apps", "update"},
		{"environments", "create"},
		{"environments", "update"},
		{"environments", "variables", "get"},
		{"environments", "variables", "update"},
		{"environments", "restart"},
		{"environments", "deploy"},
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

func TestAppWriteCommandsSendExpectedPayloads(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request %d: %v", requests, err)
		}
		switch requests {
		case 1:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/apps" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			initial, _ := body["initialEnvironment"].(map[string]any)
			deployment, _ := initial["deployment"].(map[string]any)
			git, _ := deployment["git"].(map[string]any)
			if body["name"] != "store" || body["region"] != "eu-w1a" || git["repository"] != "acme/store" || deployment["startFirstDeployment"] != true {
				t.Fatalf("create payload = %#v", body)
			}
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"store"}`))
		case 2:
			if request.Method != http.MethodPatch || request.URL.Path != "/v1/apps/ap-abc123" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			if request.Header.Get("Content-Type") != "application/merge-patch+json" || body["name"] != "shop" {
				t.Fatalf("update payload = %#v", body)
			}
			_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"shop"}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"--host", server.URL, "apps", "create", "--name", "store", "--region", "eu-w1a", "--repository", "acme/store", "--branch", "main", "--deploy"},
		{"--host", server.URL, "apps", "update", "ap-abc123", "--name", "shop"},
	} {
		output := &bytes.Buffer{}
		factory := testFactory(output)
		t.Setenv("FRBIT_TOKEN", "test-token")
		command := NewCmdRoot(factory)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(output.String(), "ap-abc123") {
			t.Fatalf("%v output = %q", args, output.String())
		}
	}
}

func TestEnvironmentWriteCommandsUseExpectedEndpoints(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/environments" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["appId"] != "ap-abc123" || body["name"] != "staging" || body["autoscaling"] != false {
				t.Fatalf("create payload = %#v", body)
			}
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"publicId":"en-abc123","name":"staging"}`))
		case 2:
			if request.Method != http.MethodPatch || request.URL.Path != "/v1/environments/en-abc123" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			_, _ = writer.Write([]byte(`{"publicId":"en-abc123","name":"preview"}`))
		case 3:
			if request.Method != http.MethodGet || request.URL.Path != "/v1/environments/en-abc123/environment-variables" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			_, _ = writer.Write([]byte(`{"custom":[{"name":"APP_ENV","value":"staging"}],"platform":[]}`))
		case 4:
			if request.Method != http.MethodPatch || request.URL.Path != "/v1/environments/en-abc123/environment-variables" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			set, _ := body["set"].(map[string]any)
			if set["APP_ENV"] != "production" {
				t.Fatalf("variables payload = %#v", body)
			}
			_, _ = writer.Write([]byte(`{"custom":[{"name":"APP_ENV","value":"production"}],"platform":[]}`))
		case 5:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/environments/en-abc123/restart" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			writer.WriteHeader(http.StatusAccepted)
		case 6:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/environments/en-abc123/deployments" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"publicId":"de-abc123","environment":"en-abc123","status":"pending"}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	commands := [][]string{
		{"environments", "create", "--app", "ap-abc123", "--name", "staging", "--component", "php=sm", "--autoscaling=false"},
		{"environments", "update", "en-abc123", "--name", "preview"},
		{"environments", "variables", "get", "en-abc123"},
		{"environments", "variables", "update", "en-abc123", "--set", "APP_ENV=production"},
		{"environments", "restart", "en-abc123"},
		{"environments", "deploy", "en-abc123"},
	}
	for _, arguments := range commands {
		output := &bytes.Buffer{}
		factory := testFactory(output)
		t.Setenv("FRBIT_TOKEN", "test-token")
		command := NewCmdRoot(factory)
		command.SetArgs(append([]string{"--host", server.URL}, arguments...))
		if err := command.Execute(); err != nil {
			t.Fatalf("%v: %v", arguments, err)
		}
		if output.Len() == 0 {
			t.Fatalf("%v: no output", arguments)
		}
	}
}

func TestCreateReadsCompleteJSONPayloadFromStdin(t *testing.T) {
	const payload = `{"name":"store","region":"eu-w1a","teamId":null,"softwarePresetName":"generic-php","initialEnvironment":{"components":{"php":"sm"}},"paymentMethodId":null}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := &bytes.Buffer{}
		_, _ = body.ReadFrom(request.Body)
		if body.String() != payload {
			t.Fatalf("body = %q, want %q", body.String(), payload)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"store"}`))
	}))
	defer server.Close()

	output := &bytes.Buffer{}
	factory := testFactory(output)
	factory.IOStreams.In = strings.NewReader(payload)
	t.Setenv("FRBIT_TOKEN", "test-token")
	command := NewCmdRoot(factory)
	command.SetArgs([]string{"--host", server.URL, "apps", "create", "--file", "-"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
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
