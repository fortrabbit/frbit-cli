package agentskills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Agent string

const (
	AgentClaudeCode Agent = "claude-code"
	AgentCodex      Agent = "codex"
	AgentCopilot    Agent = "copilot"
)

var supportedAgents = []Agent{AgentClaudeCode, AgentCodex, AgentCopilot}

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

type Target struct {
	Agent            Agent
	Scope            Scope
	SkillsBase       string
	InstructionsFile string
	VersionFile      string
}

type Installation struct {
	Agent   Agent
	Skill   string
	Scope   Scope
	Version string
	Path    string
}

type Removal struct {
	Agent      Agent
	Path       string
	Additional []string
	Directory  bool
}

func ParseAgents(values []string) ([]Agent, error) {
	if len(values) == 0 {
		return nil, nil
	}

	seen := make(map[Agent]bool)
	agents := make([]Agent, 0, len(values))
	for _, value := range values {
		var agent Agent
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "claude", "claude-code":
			agent = AgentClaudeCode
		case "codex":
			agent = AgentCodex
		case "copilot", "github-copilot":
			agent = AgentCopilot
		default:
			return nil, fmt.Errorf("unsupported agent %q; supported agents: claude-code, codex, copilot", value)
		}
		if !seen[agent] {
			seen[agent] = true
			agents = append(agents, agent)
		}
	}
	return agents, nil
}

func (a Agent) Label() string {
	switch a {
	case AgentClaudeCode:
		return "Claude Code"
	case AgentCodex:
		return "Codex"
	case AgentCopilot:
		return "GitHub Copilot"
	default:
		return string(a)
	}
}

func resolveTargets(home string, workDir string, project bool, requested []Agent, detect bool) ([]Target, error) {
	scope := ScopeGlobal
	if project {
		scope = ScopeProject
	}

	agents := requested
	if len(agents) == 0 {
		if project || !detect {
			agents = supportedAgents
			if !project {
				agents = []Agent{AgentClaudeCode, AgentCodex}
			}
		} else {
			agents = detectGlobalAgents(home)
			if len(agents) == 0 {
				return nil, fmt.Errorf("neither Claude Code (%s) nor Codex (%s) appears to be installed; use --agent to select a target explicitly, or --project for a project install", filepath.Join(home, ".claude"), filepath.Join(home, ".codex"))
			}
		}
	}

	targets := make([]Target, 0, len(agents))
	for _, agent := range agents {
		if !project && agent == AgentCopilot {
			return nil, fmt.Errorf("GitHub Copilot skills are project-scoped; use --project with --agent copilot")
		}
		targets = append(targets, targetFor(agent, scope, home, workDir))
	}
	return targets, nil
}

func detectGlobalAgents(home string) []Agent {
	var agents []Agent
	for _, candidate := range []struct {
		agent Agent
		path  string
	}{
		{AgentClaudeCode, filepath.Join(home, ".claude")},
		{AgentCodex, filepath.Join(home, ".codex")},
	} {
		info, err := os.Stat(candidate.path)
		if err == nil && info.IsDir() {
			agents = append(agents, candidate.agent)
		}
	}
	if !containsAgent(agents, AgentCodex) {
		// Existing user skills also prove that Codex has been configured, even
		// when its application configuration lives outside the default path.
		info, err := os.Stat(filepath.Join(home, ".agents"))
		if err == nil && info.IsDir() {
			agents = append(agents, AgentCodex)
		}
	}
	return agents
}

func containsAgent(agents []Agent, wanted Agent) bool {
	for _, agent := range agents {
		if agent == wanted {
			return true
		}
	}
	return false
}

func targetFor(agent Agent, scope Scope, home string, workDir string) Target {
	root := home
	if scope == ScopeProject {
		root = workDir
	}
	switch agent {
	case AgentClaudeCode:
		return Target{Agent: agent, Scope: scope, SkillsBase: filepath.Join(root, ".claude", "skills")}
	case AgentCodex:
		return Target{Agent: agent, Scope: scope, SkillsBase: filepath.Join(root, ".agents", "skills")}
	case AgentCopilot:
		instructions := filepath.Join(root, ".github", "instructions", "fortrabbit.instructions.md")
		return Target{
			Agent:            agent,
			Scope:            scope,
			InstructionsFile: instructions,
			VersionFile:      instructions + ".version",
		}
	default:
		panic("unhandled agent " + agent)
	}
}

func sortInstallations(installations []Installation) {
	sort.Slice(installations, func(i, j int) bool {
		if installations[i].Agent != installations[j].Agent {
			return installations[i].Agent < installations[j].Agent
		}
		if installations[i].Skill != installations[j].Skill {
			return installations[i].Skill < installations[j].Skill
		}
		return installations[i].Path < installations[j].Path
	})
}
