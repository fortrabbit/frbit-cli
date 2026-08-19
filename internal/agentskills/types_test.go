package agentskills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseAgentsNormalizesAliasesAndRemovesDuplicates(t *testing.T) {
	agents, err := ParseAgents([]string{"claude", "codex", "github-copilot", "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{AgentClaudeCode, AgentCodex, AgentCopilot}
	if !reflect.DeepEqual(agents, want) {
		t.Fatalf("agents = %#v, want %#v", agents, want)
	}
}

func TestParseAgentsRejectsUnsupportedAgent(t *testing.T) {
	if _, err := ParseAgents([]string{"cursor"}); err == nil {
		t.Fatal("expected unsupported agent error")
	}
}

func TestInstallTargetsDetectGlobalAgents(t *testing.T) {
	home := t.TempDir()
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := NewService(Options{
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return workDir, nil },
	})

	targets, err := service.InstallTargets(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Agent != AgentCodex {
		t.Fatalf("targets = %#v", targets)
	}
	if want := filepath.Join(home, ".agents", "skills"); targets[0].SkillsBase != want {
		t.Fatalf("skills base = %q, want %q", targets[0].SkillsBase, want)
	}
}

func TestInstallTargetsDetectExistingCodexSkills(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := NewService(Options{
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return t.TempDir(), nil },
	})
	targets, err := service.InstallTargets(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Agent != AgentCodex {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestExplicitAgentDoesNotRequireDetection(t *testing.T) {
	home := t.TempDir()
	service := NewService(Options{
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return t.TempDir(), nil },
	})
	targets, err := service.InstallTargets(false, []Agent{AgentClaudeCode})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Agent != AgentClaudeCode {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestCopilotRequiresProjectScope(t *testing.T) {
	service := NewService(Options{
		HomeDir:    func() (string, error) { return t.TempDir(), nil },
		WorkingDir: func() (string, error) { return t.TempDir(), nil },
	})
	if _, err := service.InstallTargets(false, []Agent{AgentCopilot}); err == nil {
		t.Fatal("expected project-scope error")
	}
}
