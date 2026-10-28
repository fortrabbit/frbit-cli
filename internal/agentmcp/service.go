package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/agent"
)

const (
	ServerName = "fortrabbit"
	ServerURL  = "https://mcp.fortrabbit.com/mcp"
	// Codex before 0.148 needed this static client ID; entries that carry it keep working.
	LegacyCodexClientID = "https://api.fortrabbit.com/.well-known/oauth-client/codex"
)

type Change string

const (
	ChangeInstalled Change = "installed"
	ChangeUpdated   Change = "updated"
	ChangeUnchanged Change = "unchanged"
	ChangeRemoved   Change = "removed"
)

type Registration struct {
	Agent      agent.Agent
	Installed  bool
	Current    bool
	URL        string
	ConfigPath string
}

type Result struct {
	Agent      agent.Agent
	Change     Change
	ConfigPath string
}

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("run %s: %w", name, err)
	}
	return string(output), nil
}

type Options struct {
	HomeDir  func() (string, error)
	LookPath func(string) (string, error)
	Runner   Runner
}

type Service struct {
	homeDir  func() (string, error)
	lookPath func(string) (string, error)
	runner   Runner
}

func NewService(options Options) *Service {
	homeDir := options.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	runner := options.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Service{homeDir: homeDir, lookPath: lookPath, runner: runner}
}

func ParseAgents(values []string) ([]agent.Agent, error) {
	return agent.Parse(values, agent.MCP)
}

func (s *Service) ResolveAgents(requested []agent.Agent) ([]agent.Agent, error) {
	if len(requested) > 0 {
		return requested, nil
	}
	detected := (agent.Detector{LookPath: s.lookPath}).DetectAvailable(agent.MCP)
	if len(detected) == 0 {
		return nil, errors.New("neither Claude Code nor Codex appears to be installed; use --agent to select one explicitly")
	}
	return detected, nil
}

func (s *Service) Preflight(agents []agent.Agent) error {
	for _, target := range agents {
		name := target.Executable()
		if name == "" {
			return fmt.Errorf("%s does not support MCP setup", target.Label())
		}
		if _, err := s.lookPath(name); err != nil {
			return fmt.Errorf("%s CLI executable %q was not found in PATH", target.Label(), name)
		}
	}
	return nil
}

// ClaudePluginName is the Claude Code plugin from fortrabbit/agent-skills; it brings its own skill and MCP server.
const ClaudePluginName = "fortrabbit"

// WithoutClaudePlugin drops Claude Code from agents when the fortrabbit plugin is enabled there.
func (s *Service) WithoutClaudePlugin(ctx context.Context, agents []agent.Agent) ([]agent.Agent, bool) {
	if !slices.Contains(agents, agent.ClaudeCode) || !s.claudePluginEnabled(ctx) {
		return agents, false
	}
	return slices.DeleteFunc(slices.Clone(agents), func(target agent.Agent) bool {
		return target == agent.ClaudeCode
	}), true
}

func (s *Service) claudePluginEnabled(ctx context.Context) bool {
	return s.ClaudePluginID(ctx) != ""
}

// ClaudePluginID returns the enabled fortrabbit plugin as name@marketplace, or "" without one.
// A Claude Code without plugin support, or output that does not parse, counts as no plugin.
func (s *Service) ClaudePluginID(ctx context.Context) string {
	output, err := s.runner.Run(ctx, "claude", "plugin", "list", "--json")
	if err != nil {
		return ""
	}
	if start := strings.Index(output, "["); start > 0 {
		output = output[start:]
	}
	var plugins []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if json.Unmarshal([]byte(output), &plugins) != nil {
		return ""
	}
	for _, plugin := range plugins {
		name, _, _ := strings.Cut(plugin.ID, "@")
		if plugin.Enabled && name == ClaudePluginName {
			return plugin.ID
		}
	}
	return ""
}

// RemoveClaudePlugin uninstalls the plugin, and the fortrabbit marketplace it came from.
// Another marketplace, such as the official one, carries other plugins and stays.
func (s *Service) RemoveClaudePlugin(ctx context.Context, pluginID string) error {
	if output, err := s.runner.Run(ctx, "claude", "plugin", "uninstall", pluginID); err != nil {
		return fmt.Errorf("uninstall the Claude Code plugin %s: %w: %s", pluginID, err, strings.TrimSpace(output))
	}
	if _, marketplace, _ := strings.Cut(pluginID, "@"); marketplace == ClaudePluginName {
		if output, err := s.runner.Run(ctx, "claude", "plugin", "marketplace", "remove", marketplace); err != nil {
			return fmt.Errorf("remove the Claude Code marketplace %s: %w: %s", marketplace, err, strings.TrimSpace(output))
		}
	}
	return nil
}

func (s *Service) Inspect(ctx context.Context, agents []agent.Agent) ([]Registration, error) {
	registrations := make([]Registration, 0, len(agents))
	for _, target := range agents {
		registration, err := s.inspectOne(ctx, target)
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, registration)
	}
	return registrations, nil
}

func (s *Service) Install(ctx context.Context, agents []agent.Agent) ([]Result, error) {
	if err := s.Preflight(agents); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(agents))
	for _, target := range agents {
		registration, err := s.inspectOne(ctx, target)
		if err != nil {
			return nil, err
		}
		if registration.Current {
			results = append(results, Result{Agent: target, Change: ChangeUnchanged, ConfigPath: registration.ConfigPath})
			continue
		}

		change := ChangeInstalled
		if registration.Installed {
			if err := s.removeOne(ctx, target); err != nil {
				return nil, err
			}
			change = ChangeUpdated
		}
		if err := s.addOne(ctx, target); err != nil {
			return nil, err
		}
		results = append(results, Result{Agent: target, Change: change, ConfigPath: registration.ConfigPath})
	}
	return results, nil
}

func (s *Service) Remove(ctx context.Context, agents []agent.Agent) ([]Result, error) {
	if err := s.Preflight(agents); err != nil {
		return nil, err
	}
	var results []Result
	for _, target := range agents {
		registration, err := s.inspectOne(ctx, target)
		if err != nil {
			return nil, err
		}
		if !registration.Installed {
			continue
		}
		if err := s.removeOne(ctx, target); err != nil {
			return nil, err
		}
		results = append(results, Result{Agent: target, Change: ChangeRemoved, ConfigPath: registration.ConfigPath})
	}
	return results, nil
}

func (s *Service) inspectOne(ctx context.Context, target agent.Agent) (Registration, error) {
	registration, err := s.emptyRegistration(target)
	if err != nil {
		return Registration{}, err
	}

	var output string
	switch target {
	case agent.ClaudeCode:
		output, err = s.runner.Run(ctx, "claude", "mcp", "get", ServerName)
	case agent.Codex:
		output, err = s.runner.Run(ctx, "codex", "mcp", "get", ServerName, "--json")
	default:
		return Registration{}, fmt.Errorf("%s does not support MCP setup", target.Label())
	}
	if err != nil {
		if missingRegistration(output) {
			return registration, nil
		}
		return Registration{}, fmt.Errorf("inspect %s MCP configuration: %w: %s", target.Label(), err, strings.TrimSpace(output))
	}

	registration.Installed = true
	registration.URL = extractURL(output)
	registration.Current = registration.URL == ServerURL
	if target == agent.Codex && registration.Current {
		if clientID, present := extractJSONValue(output, "oauth_client_id", "oauthClientId"); present && clientID != LegacyCodexClientID {
			registration.Current = false
		}
	}
	return registration, nil
}

func (s *Service) emptyRegistration(target agent.Agent) (Registration, error) {
	home, err := s.homeDir()
	if err != nil {
		return Registration{}, fmt.Errorf("resolve home directory: %w", err)
	}
	registration := Registration{Agent: target}
	switch target {
	case agent.ClaudeCode:
		registration.ConfigPath = filepath.Join(home, ".claude.json")
	case agent.Codex:
		registration.ConfigPath = filepath.Join(home, ".codex", "config.toml")
	default:
		return Registration{}, fmt.Errorf("%s does not support MCP setup", target.Label())
	}
	return registration, nil
}

func (s *Service) addOne(ctx context.Context, target agent.Agent) error {
	var output string
	var err error
	switch target {
	case agent.ClaudeCode:
		output, err = s.runner.Run(ctx, "claude", "mcp", "add", "--transport", "http", "--scope", "user", ServerName, ServerURL)
	case agent.Codex:
		output, err = s.runner.Run(ctx, "codex", "mcp", "add", ServerName, "--url", ServerURL)
	}
	if err != nil {
		return fmt.Errorf("register fortrabbit MCP server with %s: %w: %s", target.Label(), err, strings.TrimSpace(output))
	}
	return nil
}

func (s *Service) removeOne(ctx context.Context, target agent.Agent) error {
	var output string
	var err error
	switch target {
	case agent.ClaudeCode:
		output, err = s.runner.Run(ctx, "claude", "mcp", "remove", ServerName)
	case agent.Codex:
		output, err = s.runner.Run(ctx, "codex", "mcp", "remove", ServerName)
	}
	if err != nil {
		return fmt.Errorf("remove fortrabbit MCP server from %s: %w: %s", target.Label(), err, strings.TrimSpace(output))
	}
	return nil
}

func missingRegistration(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no mcp server named") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "does not exist")
}

func extractURL(output string) string {
	if value, ok := extractJSONValue(output, "url"); ok {
		return value
	}
	for _, field := range strings.Fields(output) {
		trimmed := strings.Trim(field, "\"'(),")
		if strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "http://") {
			return trimmed
		}
	}
	return ""
}

func extractJSONValue(output string, keys ...string) (string, bool) {
	var value any
	if json.Unmarshal([]byte(output), &value) != nil {
		return "", false
	}
	wanted := make(map[string]bool, len(keys))
	for _, key := range keys {
		wanted[key] = true
	}
	return findJSONValue(value, wanted)
}

func findJSONValue(value any, keys map[string]bool) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if keys[key] {
				stringValue, ok := child.(string)
				return stringValue, ok
			}
		}
		for _, child := range typed {
			if found, ok := findJSONValue(child, keys); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range typed {
			if found, ok := findJSONValue(child, keys); ok {
				return found, true
			}
		}
	}
	return "", false
}
