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
		"codex mcp get fortrabbit --json": {output: fmt.Sprintf(`{"transport":{"url":%q},"oauth_client_id":%q}`, ServerURL, LegacyCodexClientID)},
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
		"codex mcp add fortrabbit --url " + ServerURL,
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestInstallLeavesCodexRegistrationWithoutClientIDAlone(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"codex mcp get fortrabbit --json": {output: fmt.Sprintf(`{"transport":{"url":%q},"oauth_client_id":null}`, ServerURL)},
	}}
	results, err := testService(runner).Install(context.Background(), []agent.Agent{agent.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Change != ChangeUnchanged {
		t.Fatalf("results = %#v", results)
	}
}

func TestInstallReplacesCodexRegistrationWithForeignClientID(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"codex mcp get fortrabbit --json": {output: fmt.Sprintf(`{"transport":{"url":%q},"oauth_client_id":"https://other.example/client"}`, ServerURL)},
	}}
	results, err := testService(runner).Install(context.Background(), []agent.Agent{agent.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Change != ChangeUpdated {
		t.Fatalf("results = %#v", results)
	}
}

func TestWithoutClaudePlugin(t *testing.T) {
	both := []agent.Agent{agent.ClaudeCode, agent.Codex}
	for name, tc := range map[string]struct {
		response fakeResponse
		skipped  bool
	}{
		"enabled":             {response: fakeResponse{output: `[{"id":"fortrabbit@fortrabbit","enabled":true}]`}, skipped: true},
		"other marketplace":   {response: fakeResponse{output: `[{"id":"fortrabbit@claude-plugins-official","enabled":true}]`}, skipped: true},
		"warning before json": {response: fakeResponse{output: "warning: update available\n" + `[{"id":"fortrabbit@fortrabbit","enabled":true}]`}, skipped: true},
		"disabled":            {response: fakeResponse{output: `[{"id":"fortrabbit@fortrabbit","enabled":false}]`}},
		"other plugin":        {response: fakeResponse{output: `[{"id":"fortrabbit-extras@fortrabbit","enabled":true}]`}},
		"no plugin support":   {response: fakeResponse{output: "error: unknown command 'plugin'", err: errors.New("exit 1")}},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{responses: map[string]fakeResponse{"claude plugin list --json": tc.response}}
			remaining, skipped := testService(runner).WithoutClaudePlugin(context.Background(), both)
			if skipped != tc.skipped {
				t.Fatalf("skipped = %v, want %v", skipped, tc.skipped)
			}
			want := both
			if tc.skipped {
				want = []agent.Agent{agent.Codex}
			}
			if !reflect.DeepEqual(remaining, want) {
				t.Fatalf("remaining = %#v, want %#v", remaining, want)
			}
		})
	}
}

func TestWithoutClaudePluginLeavesCodexOnlyAlone(t *testing.T) {
	runner := &fakeRunner{}
	remaining, skipped := testService(runner).WithoutClaudePlugin(context.Background(), []agent.Agent{agent.Codex})
	if skipped || len(remaining) != 1 || len(runner.calls) != 0 {
		t.Fatalf("remaining = %#v, skipped = %v, calls = %#v", remaining, skipped, runner.calls)
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

func TestRemoveClaudePlugin(t *testing.T) {
	for id, want := range map[string][]string{
		"fortrabbit@fortrabbit":              {"claude plugin uninstall fortrabbit@fortrabbit", "claude plugin marketplace remove fortrabbit"},
		"fortrabbit@claude-plugins-official": {"claude plugin uninstall fortrabbit@claude-plugins-official"},
	} {
		runner := &fakeRunner{}
		if err := testService(runner).RemoveClaudePlugin(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(runner.calls, want) {
			t.Fatalf("%s: calls = %#v, want %#v", id, runner.calls, want)
		}
	}
}

func TestClaudePluginIDKeepsTheMarketplace(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"claude plugin list --json": {output: `[{"id":"fortrabbit@claude-plugins-official","enabled":true}]`}}}
	if id := testService(runner).ClaudePluginID(context.Background()); id != "fortrabbit@claude-plugins-official" {
		t.Fatalf("id = %q", id)
	}
}

func TestClaudePluginIDFindsADisabledPlugin(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"claude plugin list --json": {output: `[{"id":"fortrabbit@fortrabbit","enabled":false}]`}}}
	if id := testService(runner).ClaudePluginID(context.Background()); id != "fortrabbit@fortrabbit" {
		t.Fatalf("id = %q", id)
	}
}
