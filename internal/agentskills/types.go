package agentskills

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/fortrabbit/frbit-cli/internal/agent"
)

type Agent = agent.Agent

const (
	AgentClaudeCode = agent.ClaudeCode
	AgentCodex      = agent.Codex
	AgentCopilot    = agent.Copilot
)

var supportedAgents = agent.All

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
	return agent.Parse(values, agent.All)
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
			detected, err := (agent.Detector{HomeDir: func() (string, error) { return home, nil }}).Detect(agent.MCP)
			if err != nil {
				return nil, err
			}
			agents = detected
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
