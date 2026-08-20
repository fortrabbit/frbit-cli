package agentmcp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/agent"
)

type fakeRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	output string
	err    error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	response, ok := r.responses[call]
	if !ok {
		return "", nil
	}
	return response.output, response.err
}

func testService(runner Runner) *Service {
	return NewService(Options{
		HomeDir:  func() (string, error) { return "/home/test", nil },
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Runner:   runner,
	})
}

func TestInstallAddsMissingAndLeavesCurrentRegistrationAlone(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"claude mcp get fortrabbit":       {output: "No MCP server named: fortrabbit", err: errors.New("exit 1")},
		"codex mcp get fortrabbit --json": {output: fmt.Sprintf(`{"transport":{"url":%q},"oauth_client_id":%q}`, ServerURL, CodexClientID)},
	}}
	results, err := testService(runner).Install(context.Background(), []agent.Agent{agent.ClaudeCode, agent.Codex})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Agent: agent.ClaudeCode, Change: ChangeInstalled, ConfigPath: "/home/test/.claude.json"},
		{Agent: agent.Codex, Change: ChangeUnchanged, ConfigPath: "/home/test/.codex/config.toml"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %#v, want %#v", results, want)
	}
	wantCalls := []string{
		"claude mcp get fortrabbit",
		"claude mcp add --transport http --scope user fortrabbit " + ServerURL,
		"codex mcp get fortrabbit --json",
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestInstallReplacesOnlyNamedStaleRegistration(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"codex mcp get fortrabbit --json": {output: `{"transport":{"url":"https://old.example/mcp"}}`},
	}}
	results, err := testService(runner).Install(context.Background(), []agent.Agent{agent.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Change != ChangeUpdated {
		t.Fatalf("results = %#v", results)
	}
	wantCalls := []string{
		"codex mcp get fortrabbit --json",
		"codex mcp remove fortrabbit",
		"codex mcp add fortrabbit --url " + ServerURL + " --oauth-client-id " + CodexClientID,
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestPreflightReportsMissingAgentCLI(t *testing.T) {
	service := NewService(Options{
		HomeDir: func() (string, error) { return "/home/test", nil },
		LookPath: func(name string) (string, error) {
			return "", errors.New("missing")
		},
		Runner: &fakeRunner{},
	})
	err := service.Preflight([]agent.Agent{agent.Codex})
	if err == nil || !strings.Contains(err.Error(), `"codex" was not found`) {
		t.Fatalf("error = %v", err)
	}
}
