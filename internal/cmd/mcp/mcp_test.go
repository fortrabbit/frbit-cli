package mcp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

type fakeRunner struct {
	responses map[string]response
	calls     []string
}

type response struct {
	output string
	err    error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	response := r.responses[call]
	return response.output, response.err
}

func TestInstallReportsChangesAndOAuthHint(t *testing.T) {
	runner := &fakeRunner{responses: map[string]response{
		"codex mcp get fortrabbit --json": {output: "No MCP server named 'fortrabbit'", err: errors.New("exit 1")},
	}}
	output := executeMCP(t, runner, strings.NewReader(""), "install", "--agent", "codex")
	for _, expected := range []string{"Codex:", "installed", "codex mcp login fortrabbit"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
	if got := runner.calls[len(runner.calls)-1]; !strings.Contains(got, "--oauth-client-id "+agentmcp.CodexClientID) {
		t.Fatalf("add call = %q", got)
	}
}

func TestRemoveListsEntryAndAborts(t *testing.T) {
	runner := &fakeRunner{responses: map[string]response{
		"claude mcp get fortrabbit": {output: "URL: " + agentmcp.ServerURL},
	}}
	output := executeMCP(t, runner, strings.NewReader("n\n"), "remove", "--agent", "claude-code")
	if !strings.Contains(output, ".claude.json") || !strings.Contains(output, "Aborted.") {
		t.Fatalf("output = %q", output)
	}
	for _, call := range runner.calls {
		if call == "claude mcp remove fortrabbit" {
			t.Fatalf("aborted removal ran %q", call)
		}
	}
}

func executeMCP(t *testing.T, runner agentmcp.Runner, input *strings.Reader, arguments ...string) string {
	t.Helper()
	output := &bytes.Buffer{}
	service := agentmcp.NewService(agentmcp.Options{
		HomeDir:  func() (string, error) { return "/home/test", nil },
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Runner:   runner,
	})
	factory := &app.Factory{IOStreams: iostreams.IOStreams{In: input, Out: output, ErrOut: output}}
	command := NewCmdMCP(factory, service)
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(arguments)
	if err := command.Execute(); err != nil {
		t.Fatalf("frbit mcp %s: %v", strings.Join(arguments, " "), err)
	}
	return output.String()
}
