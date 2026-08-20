package setup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

type fakeMCPService struct {
	preflighted []agent.Agent
}

func (s *fakeMCPService) ResolveAgents(requested []agent.Agent) ([]agent.Agent, error) {
	if len(requested) == 0 {
		return []agent.Agent{agent.ClaudeCode, agent.Codex}, nil
	}
	return requested, nil
}
func (s *fakeMCPService) Preflight(agents []agent.Agent) error {
	s.preflighted = append([]agent.Agent(nil), agents...)
	return nil
}
func (s *fakeMCPService) Install(_ context.Context, agents []agent.Agent) ([]agentmcp.Result, error) {
	results := make([]agentmcp.Result, 0, len(agents))
	for _, target := range agents {
		results = append(results, agentmcp.Result{Agent: target, Change: agentmcp.ChangeInstalled, ConfigPath: "/config/" + string(target)})
	}
	return results, nil
}

type fakeSkillsService struct{}

func (fakeSkillsService) InstallTargets(_ bool, agents []agentskills.Agent) ([]agentskills.Target, error) {
	targets := make([]agentskills.Target, 0, len(agents))
	for _, target := range agents {
		targets = append(targets, agentskills.Target{Agent: target})
	}
	return targets, nil
}
func (fakeSkillsService) LatestRelease(context.Context) (agentskills.Release, error) {
	return agentskills.Release{Tag: "v1.2.3", Version: "1.2.3"}, nil
}
func (fakeSkillsService) InstallRelease(_ context.Context, release agentskills.Release, targets []agentskills.Target) ([]agentskills.Installation, error) {
	installations := make([]agentskills.Installation, 0, len(targets))
	for _, target := range targets {
		installations = append(installations, agentskills.Installation{Agent: target.Agent, Skill: "fortrabbit", Version: release.Version, Path: "/skills/" + string(target.Agent)})
	}
	return installations, nil
}

type missingCredentials struct{}

var _ credentials.Store = missingCredentials{}

func (missingCredentials) Get(string) (string, error) { return "", errors.New("missing") }
func (missingCredentials) Set(string, string) error   { return nil }
func (missingCredentials) Delete(string) error        { return nil }

func TestAgentSetupPrintsCombinedSummary(t *testing.T) {
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: output},
		CredentialStore: missingCredentials{},
		Version:         "test",
	}
	mcp := &fakeMCPService{}
	root := &cobra.Command{Use: "frbit"}
	root.PersistentFlags().String("profile", app.DefaultProfile, "Credential profile")
	root.AddCommand(NewCmdSetup(factory, mcp, fakeSkillsService{}))
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"setup", "agent", "--agent", "codex"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Agent setup complete.",
		"/config/codex",
		"Skills v1.2.3",
		"/skills/codex",
		"No credential configured",
		"separate from MCP OAuth",
		"codex mcp login fortrabbit",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output %q does not contain %q", output.String(), expected)
		}
	}
	if len(mcp.preflighted) != 1 || mcp.preflighted[0] != agent.Codex {
		t.Fatalf("preflighted = %#v", mcp.preflighted)
	}
}
