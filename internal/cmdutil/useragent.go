package cmdutil

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var agentGetenv = os.Getenv

// agentVariables are set by the coding agents themselves, checked after the shared AI_AGENT.
var agentVariables = []struct{ variable, agent string }{
	{"CLAUDECODE", "claude-code"},
	{"CODEX_SANDBOX", "codex"},
	{"CURSOR_AGENT", "cursor"},
	{"OPENCODE", "opencode"},
	{"GEMINI_CLI", "gemini-cli"},
}

var agentName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// DetectAgent names the coding agent that runs this process, or returns "" for a person at a terminal. Self-reported, so FRAPI never trusts it for access.
func DetectAgent() string {
	// AI_AGENT looks like claude-code_2-1-322_agent: the name comes before the first underscore.
	if value := strings.ToLower(strings.TrimSpace(agentGetenv("AI_AGENT"))); value != "" {
		name, _, _ := strings.Cut(value, "_")
		if agentName.MatchString(name) {
			return name
		}
	}
	for _, candidate := range agentVariables {
		if agentGetenv(candidate.variable) != "" {
			return candidate.agent
		}
	}
	return ""
}

// UserAgent is the User-Agent of API requests: frbit/<version>, plus agent/<name> when a coding agent runs the command.
func UserAgent(version string) string {
	userAgent := fmt.Sprintf("frbit/%s", version)
	if agent := DetectAgent(); agent != "" {
		userAgent += " agent/" + agent
	}
	return userAgent
}
