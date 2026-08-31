package root

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		if request.Header.Get("Accept") != "application/ld+json" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"member":[{"publicId":"ap-abc123","name":"Store","description":"Example app","teams":["tm-abc123"],"trial":false,"updatedAt":"2026-01-02T00:00:00Z"}],"totalItems":23}`))
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
	for _, expected := range []string{"ID", "TEAMS", "ap-abc123", "Store", "Example app", "tm-abc123", "Total: 23 apps"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output %q does not contain %q", got, expected)
		}
	}
}

func TestResourceListUsesSingularTotal(t *testing.T) {
	output := executeResourceList(t, "/v1/deployments", `[{"publicId":"dp-abc123"}]`, "deployments")
	if !strings.Contains(output, "Total: 1 deployment") {
		t.Fatalf("output %q does not contain singular total", output)
	}
}

func TestEmptyResourceListIncludesZeroTotal(t *testing.T) {
	output := executeResourceList(t, "/v1/apps", `[]`, "apps")
	for _, expected := range []string{"No apps found.", "Total: 0 apps"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestAppsListJSONPreservesAPIResponse(t *testing.T) {
	const payload = `[{"publicId":"ap-abc123","name":"Store","trial":false}]`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
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
			_, _ = writer.Write([]byte(`[{"publicId":"en-abc123","name":"production","softwareVersion":"12"}]`))
		case "/v1/people/pn-abc123":
			_, _ = writer.Write([]byte(`{"publicId":"pn-abc123","name":"Ada","email":"ada@example.test","type":"developer"}`))
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

func TestDomainsListRendersEnvironmentRelationship(t *testing.T) {
	output := executeResourceList(t, "/v1/domains", `[{"publicId":"do-abc123","name":"example.test","type":"apex","environment":"en-abc123","isMain":true}]`, "domains")
	assertTableColumns(t, output, "ID", "NAME", "TYPE", "ENVIRONMENT", "MAIN", "UPDATED")
	for _, expected := range []string{"do-abc123", "example.test", "en-abc123"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestEnvironmentsListRendersTable(t *testing.T) {
	output := executeResourceList(t, "/v1/environments", `[{"publicId":"en-abc123","name":"production","softwareVersion":"12","updatedAt":"2026-01-02T00:00:00Z"}]`, "environments")
	assertTableColumns(t, output, "ID", "NAME", "SOFTWARE", "UPDATED")
	for _, expected := range []string{"en-abc123", "production", "12"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestPeopleListRendersTable(t *testing.T) {
	output := executeResourceList(t, "/v1/people", `[{"publicId":"pn-abc123","name":"Ada","email":"ada@example.test","type":"developer"}]`, "people")
	assertTableColumns(t, output, "ID", "NAME", "EMAIL", "TYPE")
	for _, expected := range []string{"pn-abc123", "Ada", "ada@example.test", "developer"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func executeResourceList(t *testing.T, path string, payload string, resource string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			t.Fatalf("path = %q, want %q", request.URL.Path, path)
		}
		_, _ = writer.Write([]byte(payload))
	}))
	defer server.Close()

	t.Setenv("FRBIT_TOKEN", "test-token")
	output := &bytes.Buffer{}
	command := NewCmdRoot(testFactory(output))
	command.SetArgs([]string{"--host", server.URL, resource, "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func assertTableColumns(t *testing.T, output string, expected ...string) {
	t.Helper()
	header, _, _ := strings.Cut(output, "\n")
	actual := strings.Fields(header)
	if fmt.Sprint(actual) != fmt.Sprint(expected) {
		t.Fatalf("columns = %v, want %v", actual, expected)
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
		{"apps", "delete"},
		{"environments", "create"},
		{"environments", "update"},
		{"environments", "delete"},
		{"environments", "variables", "get"},
		{"environments", "variables", "update"},
		{"environments", "restart"},
		{"environments", "deploy"},
		{"domains", "delete"},
		{"teams", "delete"},
		{"payment-methods", "delete"},
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

func TestDeleteCommandsUseExpectedEndpoints(t *testing.T) {
	for _, test := range []struct {
		resource string
		publicID string
		name     string
		warning  string
	}{
		{resource: "apps", publicID: "ap-abc123", name: "Store", warning: "All files and database contents will be erased."},
		{resource: "environments", publicID: "en-abc123", name: "production", warning: "All files and database contents will be erased"},
		{resource: "domains", publicID: "do-abc123", name: "example.test", warning: "The domain will no longer serve the environment."},
		{resource: "teams", publicID: "tm-abc123", name: "Developers", warning: "payment methods owned solely by it"},
		{resource: "payment-methods", publicID: "pm-abc123", name: "Company card", warning: "every app booked on it"},
	} {
		t.Run(test.resource, func(t *testing.T) {
			requests := 0
			path := "/v1/" + test.resource + "/" + test.publicID
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests++
				if request.URL.Path != path {
					t.Fatalf("path = %q, want %q", request.URL.Path, path)
				}
				switch requests {
				case 1:
					if request.Method != http.MethodGet {
						t.Fatalf("method = %s, want GET", request.Method)
					}
					_, _ = fmt.Fprintf(writer, `{"publicId":%q,"name":%q}`, test.publicID, test.name)
				case 2:
					if request.Method != http.MethodDelete {
						t.Fatalf("method = %s, want DELETE", request.Method)
					}
					writer.WriteHeader(http.StatusNoContent)
				default:
					t.Fatalf("unexpected request %d", requests)
				}
			}))
			defer server.Close()

			output := &bytes.Buffer{}
			t.Setenv("FRBIT_TOKEN", "test-token")
			command := NewCmdRoot(testFactory(output))
			command.SetArgs([]string{"--host", server.URL, test.resource, "delete", test.publicID, "--confirm", test.publicID})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if requests != 2 {
				t.Fatalf("requests = %d, want GET followed by DELETE", requests)
			}
			for _, expected := range []string{test.name, test.publicID, test.warning, "This cannot be undone.", "Deleted"} {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("output %q does not contain %q", output.String(), expected)
				}
			}
		})
	}
}

func TestDeleteAbortsWhenInteractiveConfirmationDoesNotMatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method == http.MethodDelete {
			t.Fatal("DELETE request was sent after mismatched confirmation")
		}
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"Store"}`))
	}))
	defer server.Close()

	output := &bytes.Buffer{}
	factory := testFactory(output)
	factory.IOStreams.In = strings.NewReader("ap-wrong1\n")
	factory.IOStreams.IsTTY = true
	t.Setenv("FRBIT_TOKEN", "test-token")
	command := NewCmdRoot(factory)
	command.SetArgs([]string{"--host", server.URL, "apps", "delete", "ap-abc123"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(output.String(), "Type ap-abc123 to confirm:") || !strings.Contains(output.String(), "Aborted.") {
		t.Fatalf("requests = %d, output = %q", requests, output.String())
	}
}

func TestDeleteRequiresConfirmationInNonInteractiveUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
	}))
	defer server.Close()

	for _, arguments := range [][]string{
		{"apps", "delete", "ap-abc123"},
		{"apps", "delete", "ap-abc123", "--confirm", "ap-wrong1"},
	} {
		output := &bytes.Buffer{}
		command := NewCmdRoot(testFactory(output))
		command.SetArgs(append([]string{"--host", server.URL}, arguments...))
		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), "confirm") {
			t.Fatalf("%v error = %v, want confirmation error", arguments, err)
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
			deployment, _ := body["deployment"].(map[string]any)
			git, _ := deployment["git"].(map[string]any)
			if body["appId"] != "ap-abc123" || body["name"] != "staging" || body["autoscaling"] != false || git["branch"] != "main" || deployment["startFirstDeployment"] != true {
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
		{"environments", "create", "--app", "ap-abc123", "--name", "staging", "--component", "php=sm", "--autoscaling=false", "--branch", "main", "--deploy"},
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

func TestCompletionHelpListsAllShells(t *testing.T) {
	output := &bytes.Buffer{}
	command := NewCmdRoot(testFactory(output))
	command.SetArgs([]string{"completion", "--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"bash", "fish", "powershell", "zsh", "install"} {
		if !strings.Contains(output.String(), shell) {
			t.Errorf("help = %q, does not contain %q", output.String(), shell)
		}
	}
}

func TestCompletionGeneratorsIncludeInstallHint(t *testing.T) {
	tests := []struct {
		shell  string
		script string
	}{
		{"bash", "_frbit"},
		{"fish", "complete -c frbit"},
		{"powershell", "Register-ArgumentCompleter"},
		{"zsh", "#compdef frbit"},
	}
	for _, test := range tests {
		output := &bytes.Buffer{}
		command := NewCmdRoot(testFactory(output))
		command.SetArgs([]string{"completion", test.shell})
		if err := command.Execute(); err != nil {
			t.Fatalf("completion %s: %v", test.shell, err)
		}
		want := "# To install this completion, run: frbit completion install " + test.shell + "\n\n"
		if got := output.String(); !strings.HasPrefix(got, want) {
			t.Errorf("%s output = %q, want prefix %q", test.shell, got, want)
		}
		if got := output.String(); !strings.Contains(got, test.script) {
			t.Errorf("%s output = %q, want generated script containing %q", test.shell, got, test.script)
		}
		wantSuffix := "\n# To install this completion, run: frbit completion install " + test.shell + "\n"
		if got := output.String(); !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("%s output = %q, want suffix %q", test.shell, got, wantSuffix)
		}
	}
}

func TestCompletionInstallZshWritesCompletionAndConfiguresZsh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)

	output := &bytes.Buffer{}
	command := NewCmdRoot(testFactory(output))
	command.SetArgs([]string{"completion", "install", "zsh"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	completionPath := filepath.Join(home, ".zfunc", "_frbit")
	completion, err := os.ReadFile(completionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(completion), "#compdef frbit") {
		t.Errorf("completion = %q, want zsh completion", completion)
	}

	zshrcPath := filepath.Join(home, ".zshrc")
	zshrc, err := os.ReadFile(zshrcPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(zshrc), zshCompletionMarker); got != 1 {
		t.Errorf("completion marker count = %d, want 1", got)
	}

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	zshrc, err = os.ReadFile(zshrcPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(zshrc), zshCompletionMarker); got != 1 {
		t.Errorf("completion marker count after reinstall = %d, want 1", got)
	}
}

func TestCompletionInstallWritesScriptsForOtherShells(t *testing.T) {
	home := t.TempDir()
	dataHome := filepath.Join(home, "data")
	configHome := filepath.Join(home, "config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	tests := []struct {
		shell string
		path  string
		want  string
	}{
		{"bash", filepath.Join(dataHome, "bash-completion", "completions", "frbit"), "_frbit"},
		{"fish", filepath.Join(configHome, "fish", "completions", "frbit.fish"), "complete -c frbit"},
		{"powershell", filepath.Join(configHome, "frbit", "completion.ps1"), "Register-ArgumentCompleter"},
	}
	for _, test := range tests {
		output := &bytes.Buffer{}
		command := NewCmdRoot(testFactory(output))
		command.SetArgs([]string{"completion", "install", test.shell})
		if err := command.Execute(); err != nil {
			t.Fatalf("install %s: %v", test.shell, err)
		}
		completion, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatalf("read %s completion: %v", test.shell, err)
		}
		if !strings.Contains(string(completion), test.want) {
			t.Errorf("%s completion = %q, want %q", test.shell, completion, test.want)
		}
	}

	profile, err := os.ReadFile(powerShellProfilePath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(profile), zshCompletionMarker) {
		t.Errorf("PowerShell profile = %q, want completion setup", profile)
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
