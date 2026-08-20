package agent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseNormalizesAliasesAndFiltersSupportedAgents(t *testing.T) {
	got, err := Parse([]string{"claude", "codex", "claude-code"}, MCP)
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{ClaudeCode, Codex}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents = %#v, want %#v", got, want)
	}
	if _, err := Parse([]string{"copilot"}, MCP); err == nil {
		t.Fatal("expected unsupported agent error")
	}
}

func TestDetectorUsesConfigurationAndExecutables(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	detector := Detector{
		HomeDir: func() (string, error) { return home, nil },
		LookPath: func(name string) (string, error) {
			if name == "codex" {
				return "/bin/codex", nil
			}
			return "", errors.New("not found")
		},
	}
	got, err := detector.Detect(MCP)
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{ClaudeCode, Codex}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents = %#v, want %#v", got, want)
	}
}

func TestDetectAvailableIgnoresConfigurationWithoutExecutable(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	detector := Detector{
		HomeDir: func() (string, error) { return home, nil },
		LookPath: func(name string) (string, error) {
			if name == "codex" {
				return "/bin/codex", nil
			}
			return "", errors.New("not found")
		},
	}
	if got := detector.DetectAvailable(MCP); !reflect.DeepEqual(got, []Agent{Codex}) {
		t.Fatalf("agents = %#v", got)
	}
}
