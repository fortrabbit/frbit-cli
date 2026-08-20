package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Agent string

const (
	ClaudeCode Agent = "claude-code"
	Codex      Agent = "codex"
	Copilot    Agent = "copilot"
)

var All = []Agent{ClaudeCode, Codex, Copilot}
var MCP = []Agent{ClaudeCode, Codex}

func Parse(values []string, supported []Agent) ([]Agent, error) {
	if len(values) == 0 {
		return nil, nil
	}

	allowed := make(map[Agent]bool, len(supported))
	for _, candidate := range supported {
		allowed[candidate] = true
	}

	seen := make(map[Agent]bool)
	agents := make([]Agent, 0, len(values))
	for _, value := range values {
		var parsed Agent
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "claude", "claude-code":
			parsed = ClaudeCode
		case "codex":
			parsed = Codex
		case "copilot", "github-copilot":
			parsed = Copilot
		default:
			return nil, unsupported(value, supported)
		}
		if !allowed[parsed] {
			return nil, unsupported(value, supported)
		}
		if !seen[parsed] {
			seen[parsed] = true
			agents = append(agents, parsed)
		}
	}
	return agents, nil
}

func unsupported(value string, supported []Agent) error {
	names := make([]string, 0, len(supported))
	for _, candidate := range supported {
		names = append(names, string(candidate))
	}
	return fmt.Errorf("unsupported agent %q; supported agents: %s", value, strings.Join(names, ", "))
}

func (a Agent) Label() string {
	switch a {
	case ClaudeCode:
		return "Claude Code"
	case Codex:
		return "Codex"
	case Copilot:
		return "GitHub Copilot"
	default:
		return string(a)
	}
}

func (a Agent) Executable() string {
	switch a {
	case ClaudeCode:
		return "claude"
	case Codex:
		return "codex"
	default:
		return ""
	}
}

type Detector struct {
	HomeDir  func() (string, error)
	LookPath func(string) (string, error)
}

func (d Detector) Detect(candidates []Agent) ([]Agent, error) {
	homeDir := d.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err := homeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	var detected []Agent
	for _, candidate := range candidates {
		if configured(home, candidate) || d.available(candidate) {
			detected = append(detected, candidate)
		}
	}
	return detected, nil
}

func (d Detector) DetectAvailable(candidates []Agent) []Agent {
	var detected []Agent
	for _, candidate := range candidates {
		if d.available(candidate) {
			detected = append(detected, candidate)
		}
	}
	return detected
}

func configured(home string, candidate Agent) bool {
	paths := map[Agent][]string{
		ClaudeCode: {filepath.Join(home, ".claude")},
		Codex:      {filepath.Join(home, ".codex"), filepath.Join(home, ".agents")},
	}[candidate]
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (d Detector) available(candidate Agent) bool {
	if d.LookPath == nil || candidate.Executable() == "" {
		return false
	}
	_, err := d.LookPath(candidate.Executable())
	return err == nil
}
