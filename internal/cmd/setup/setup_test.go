package setup

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
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
	preflighted   []agent.Agent
	installed     []agent.Agent
	registered    []agent.Agent
	removed       []agent.Agent
	removedPlugin string
	claudePlugin  bool
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
func (s *fakeMCPService) WithoutClaudePlugin(_ context.Context, agents []agent.Agent) ([]agent.Agent, bool) {
	if !s.claudePlugin {
		return agents, false
	}
	remaining := []agent.Agent{}
	for _, target := range agents {
		if target != agent.ClaudeCode {
			remaining = append(remaining, target)
		}
	}
	return remaining, len(remaining) != len(agents)
}
func (s *fakeMCPService) Install(_ context.Context, agents []agent.Agent) ([]agentmcp.Result, error) {
	s.installed = append([]agent.Agent(nil), agents...)
	results := make([]agentmcp.Result, 0, len(agents))
	for _, target := range agents {
		results = append(results, agentmcp.Result{Agent: target, Change: agentmcp.ChangeInstalled, ConfigPath: "/config/" + string(target)})
	}
	return results, nil
}

func (s *fakeMCPService) Inspect(_ context.Context, agents []agent.Agent) ([]agentmcp.Registration, error) {
	registrations := make([]agentmcp.Registration, 0, len(agents))
	for _, target := range agents {
		registrations = append(registrations, agentmcp.Registration{Agent: target, Installed: slices.Contains(s.registered, target), ConfigPath: "/config/" + string(target)})
	}
	return registrations, nil
}
func (s *fakeMCPService) Remove(_ context.Context, agents []agent.Agent) ([]agentmcp.Result, error) {
	s.removed = append([]agent.Agent(nil), agents...)
	return nil, nil
}

func (s *fakeMCPService) ClaudePluginID(context.Context) string {
	if s.claudePlugin {
		return "fortrabbit@fortrabbit"
	}
	return ""
}
func (s *fakeMCPService) RemoveClaudePlugin(_ context.Context, pluginID string) error {
	s.removedPlugin = pluginID
	return nil
}

type fakeSkillsService struct {
	installed []agent.Agent
	removed   *[]agentskills.Removal
}

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
func (s fakeSkillsService) Removals(_ bool, agents []agentskills.Agent) ([]agentskills.Removal, error) {
	if len(agents) == 0 {
		agents = []agentskills.Agent{agent.ClaudeCode, agent.Codex}
	}
	var removals []agentskills.Removal
	for _, target := range agents {
		if slices.Contains(s.installed, target) {
			removals = append(removals, agentskills.Removal{Agent: target, Path: "/skills/" + string(target), Directory: true})
		}
	}
	return removals, nil
}
func (s fakeSkillsService) Remove(removals []agentskills.Removal) error {
	if s.removed != nil {
		*s.removed = append(*s.removed, removals...)
	}
	return nil
}

type missingCredentials struct{}

var _ credentials.Store = missingCredentials{}

func (missingCredentials) Get(string) (credentials.Credential, error) {
	return credentials.Credential{}, credentials.ErrNotFound
}
func (missingCredentials) Set(string, credentials.Credential) error { return nil }
func (missingCredentials) Delete(string) error                      { return nil }

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

func runSetup(t *testing.T, mcp *fakeMCPService, args ...string) string {
	t.Helper()
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: output},
		CredentialStore: missingCredentials{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit"}
	root.PersistentFlags().String("profile", app.DefaultProfile, "Credential profile")
	root.AddCommand(NewCmdSetup(factory, mcp, fakeSkillsService{}))
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(append([]string{"setup", "agent"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestAgentSetupSkipsClaudeCodeWithThePlugin(t *testing.T) {
	mcp := &fakeMCPService{claudePlugin: true}
	output := runSetup(t, mcp)
	if !strings.Contains(output, claudePluginNotice) {
		t.Fatalf("output %q does not contain the plugin notice", output)
	}
	if strings.Contains(output, "/skills/claude-code") || strings.Contains(output, "/config/claude-code") {
		t.Fatalf("output %q still sets up Claude Code", output)
	}
	if len(mcp.installed) != 1 || mcp.installed[0] != agent.Codex {
		t.Fatalf("installed = %#v", mcp.installed)
	}
}

func TestAgentSetupStopsWhenOnlyClaudeCodeHasThePlugin(t *testing.T) {
	mcp := &fakeMCPService{claudePlugin: true}
	output := runSetup(t, mcp, "--agent", "claude-code")
	if !strings.Contains(output, claudePluginNotice) || strings.Contains(output, "Agent setup complete.") {
		t.Fatalf("output = %q", output)
	}
	if mcp.installed != nil {
		t.Fatalf("installed = %#v", mcp.installed)
	}
}

type storedCredentials struct {
	deleted *bool
}

func (storedCredentials) Get(string) (credentials.Credential, error) {
	return credentials.APIToken("token"), nil
}
func (storedCredentials) Set(string, credentials.Credential) error { return nil }
func (s storedCredentials) Delete(string) error {
	*s.deleted = true
	return nil
}

func runRemove(t *testing.T, store credentials.Store, input string, mcp *fakeMCPService, skills fakeSkillsService, args ...string) string {
	t.Helper()
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:          iostreams.IOStreams{In: strings.NewReader(input), Out: output, ErrOut: output},
		CredentialStore:    store,
		CredentialLockPath: filepath.Join(t.TempDir(), "credentials.lock"),
		Version:            "test",
	}
	root := &cobra.Command{Use: "frbit"}
	root.PersistentFlags().String("profile", app.DefaultProfile, "Credential profile")
	root.AddCommand(NewCmdSetup(factory, mcp, skills))
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(append([]string{"setup", "agent", "--remove"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestAgentRemoveUndoesTheSetup(t *testing.T) {
	deleted := false
	removed := []agentskills.Removal{}
	mcp := &fakeMCPService{registered: []agent.Agent{agent.ClaudeCode, agent.Codex}}
	skills := fakeSkillsService{installed: []agent.Agent{agent.Codex}, removed: &removed}
	output := runRemove(t, storedCredentials{deleted: &deleted}, "", mcp, skills, "--yes")
	for _, expected := range []string{"MCP server in /config/codex", "skill at /skills/codex", "Agent setup removed.", disconnectHint, uninstallHint} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
	if len(mcp.removed) != 2 || len(removed) != 1 || !deleted {
		t.Fatalf("mcp removed = %#v, skills removed = %#v, signed out = %v", mcp.removed, removed, deleted)
	}
}

func TestAgentRemoveAbortsWithoutConfirmation(t *testing.T) {
	deleted := false
	removed := []agentskills.Removal{}
	mcp := &fakeMCPService{registered: []agent.Agent{agent.Codex}}
	output := runRemove(t, storedCredentials{deleted: &deleted}, "n\n", mcp, fakeSkillsService{removed: &removed})
	if !strings.Contains(output, "Aborted.") || mcp.removed != nil || len(removed) != 0 || deleted {
		t.Fatalf("output = %q, mcp removed = %#v, skills removed = %#v, signed out = %v", output, mcp.removed, removed, deleted)
	}
}

func TestAgentRemoveReportsNothingToRemove(t *testing.T) {
	output := runRemove(t, missingCredentials{}, "", &fakeMCPService{}, fakeSkillsService{})
	if !strings.Contains(output, "Nothing to remove") {
		t.Fatalf("output = %q", output)
	}
}

func TestAgentRemoveLeavesTheClaudePluginToClaude(t *testing.T) {
	mcp := &fakeMCPService{claudePlugin: true, registered: []agent.Agent{agent.ClaudeCode, agent.Codex}}
	output := runRemove(t, missingCredentials{}, "", mcp, fakeSkillsService{}, "--yes")
	if !strings.Contains(output, claudePluginRemoveNotice) {
		t.Fatalf("output %q does not contain the plugin notice", output)
	}
	if len(mcp.removed) != 1 || mcp.removed[0] != agent.Codex {
		t.Fatalf("removed = %#v", mcp.removed)
	}
}

func TestAgentRemoveKeepsCodexWhenOnlyClaudeCodeHasThePlugin(t *testing.T) {
	removed := []agentskills.Removal{}
	mcp := &fakeMCPService{claudePlugin: true, registered: []agent.Agent{agent.Codex}}
	skills := fakeSkillsService{installed: []agent.Agent{agent.Codex}, removed: &removed}
	output := runRemove(t, missingCredentials{}, "", mcp, skills, "--agent", "claude-code", "--yes")
	if !strings.Contains(output, "Nothing to remove") || mcp.removed != nil || len(removed) != 0 {
		t.Fatalf("output = %q, mcp removed = %#v, skills removed = %#v", output, mcp.removed, removed)
	}
}
