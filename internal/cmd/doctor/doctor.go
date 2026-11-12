// Package doctor checks whether this machine is ready to work with fortrabbit.
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/fortrabbit/frbit-cli/internal/update"
	"github.com/spf13/cobra"
)

// Status of one check. Only StatusFail makes the command exit non-zero.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// Check is one line of the report.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Environment is the environment of the checkout, as far as it resolved.
type Environment struct {
	ID     string
	From   string
	Target string
}

// Probes are the side effects the checks need; tests replace them.
type Probes struct {
	Latest      func(ctx context.Context, current string) (string, error)
	SignIn      func(ctx context.Context) (email string, err error)
	LookPath    func(name string) (string, error)
	ToolVersion func(ctx context.Context, name string) string
	Linked      func(ctx context.Context) (labels []string, inCheckout bool, err error)
	Environment func(ctx context.Context) (Environment, error)
	SSH         func(ctx context.Context, target string) error
	MCP         func(ctx context.Context) ([]agentmcp.Registration, error)
	Skills      func() ([]agentskills.Installation, error)
}

const sshTimeout = 20 * time.Second

func NewCmdDoctor(factory *app.Factory, probes *Probes) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Annotations: map[string]string{cmdutil.OwnJSONErrors: "true"},
		Use:         "doctor",
		Short:       "Check this machine's setup and connection to fortrabbit",
		Long: `Check the CLI version, the sign-in, the local tools, the linked environment, the SSH connection and the coding agents, one line each with the fix.

The version check asks GitHub every time, also without a terminal. The SSH check runs a no-op command on the environment of this checkout, so it needs frbit link or a matching repository.

Exits 1 when a check fails; warnings and skipped checks exit 0. --json prints the checks for agents.`,
		Example: `  frbit doctor
  frbit doctor --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := probes
			if p == nil {
				p = defaultProbes(cmd, factory)
			}
			checks := Run(cmd.Context(), factory.Version, p)
			out := cmd.OutOrStdout()
			if asJSON {
				if err := writeJSON(out, checks); err != nil {
					return err
				}
			} else if err := writeText(out, checks); err != nil {
				return err
			}
			if failed(checks) {
				return cmdutil.ErrAborted
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "Print the checks as JSON")
	return command
}

// Run runs every check in order; a check that depends on an earlier one is skipped when that one fails.
func Run(ctx context.Context, version string, p *Probes) []Check {
	checks := []Check{versionCheck(ctx, version, p)}
	signIn := signInCheck(ctx, p)
	checks = append(checks, signIn)
	checks = append(checks, toolChecks(ctx, p)...)
	project := projectCheck(ctx, p)
	checks = append(checks, project)
	checks = append(checks, sshCheck(ctx, p, signIn, project))
	checks = append(checks, agentChecks(ctx, p)...)
	return checks
}

func versionCheck(ctx context.Context, version string, p *Probes) Check {
	current := strings.TrimPrefix(version, "v")
	if current == "" || current == "dev" {
		return Check{Name: "Version", Status: StatusSkip, Detail: "development build"}
	}
	latest, err := p.Latest(ctx, version)
	switch {
	case err != nil:
		return Check{Name: "Version", Status: StatusWarn, Detail: fmt.Sprintf("%s, latest release unknown: %v", current, err)}
	case latest != "":
		return Check{Name: "Version", Status: StatusWarn, Detail: fmt.Sprintf("%s, %s is out", current, latest), Fix: "update frbit the way you installed it: https://docs.fortrabbit.com/platform/automation/cli#install"}
	default:
		return Check{Name: "Version", Status: StatusOK, Detail: current + ", the latest release"}
	}
}

func signInCheck(ctx context.Context, p *Probes) Check {
	email, err := p.SignIn(ctx)
	switch {
	case errors.Is(err, credentials.ErrNotFound):
		return Check{Name: "Sign-in", Status: StatusWarn, Detail: "not signed in", Fix: "frbit auth login"}
	case err != nil:
		return Check{Name: "Sign-in", Status: StatusFail, Detail: err.Error(), Fix: "frbit auth login, or check FRBIT_TOKEN"}
	default:
		return Check{Name: "Sign-in", Status: StatusOK, Detail: email}
	}
}

func toolChecks(ctx context.Context, p *Probes) []Check {
	tools := []struct {
		name, use, missing string
	}{
		{"ssh", "SSH, db and files commands", StatusFail},
		{"rsync", "files commands", StatusWarn},
		{"git", "finding the environment of a checkout", StatusWarn},
	}
	checks := make([]Check, 0, len(tools))
	for _, tool := range tools {
		if _, err := p.LookPath(tool.name); err != nil {
			checks = append(checks, Check{Name: tool.name, Status: tool.missing, Detail: "not found, needed for " + tool.use, Fix: sshexec.InstallHint(tool.name)})
			continue
		}
		checks = append(checks, Check{Name: tool.name, Status: StatusOK, Detail: p.ToolVersion(ctx, tool.name)})
	}
	return checks
}

func projectCheck(ctx context.Context, p *Probes) Check {
	labels, inCheckout, err := p.Linked(ctx)
	switch {
	case err != nil:
		return Check{Name: "Project", Status: StatusFail, Detail: err.Error(), Fix: "fix or remove " + link.FilePath}
	case !inCheckout:
		return Check{Name: "Project", Status: StatusSkip, Detail: "not in a git checkout"}
	case len(labels) == 0:
		return Check{Name: "Project", Status: StatusOK, Detail: "no " + link.FilePath + "; the environment comes from the repository connected to the app", Fix: "frbit link <env-id> to name it"}
	default:
		return Check{Name: "Project", Status: StatusOK, Detail: link.FilePath + ": " + strings.Join(labels, ", ")}
	}
}

func sshCheck(ctx context.Context, p *Probes, signIn, project Check) Check {
	if signIn.Status != StatusOK {
		return Check{Name: "SSH", Status: StatusSkip, Detail: "needs a sign-in"}
	}
	if project.Status == StatusSkip || project.Status == StatusFail {
		return Check{Name: "SSH", Status: StatusSkip, Detail: "needs a linked checkout"}
	}
	env, err := p.Environment(ctx)
	if errors.Is(err, link.ErrNoEnvironment) {
		return Check{Name: "SSH", Status: StatusSkip, Detail: "no environment for this checkout", Fix: "frbit link <env-id>"}
	}
	if err != nil {
		return Check{Name: "SSH", Status: StatusFail, Detail: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, sshTimeout)
	defer cancel()
	if err := p.SSH(ctx, env.Target); err != nil {
		return Check{Name: "SSH", Status: StatusFail, Detail: fmt.Sprintf("%s (%s): %v", env.ID, env.From, err)}
	}
	return Check{Name: "SSH", Status: StatusOK, Detail: fmt.Sprintf("%s (%s) answers", env.ID, env.From)}
}

func agentChecks(ctx context.Context, p *Probes) []Check {
	registrations, err := p.MCP(ctx)
	var checks []Check
	switch {
	case err != nil:
		checks = append(checks, Check{Name: "MCP", Status: StatusSkip, Detail: err.Error()})
	default:
		for _, registration := range registrations {
			name := "MCP, " + string(registration.Agent)
			switch {
			case !registration.Installed:
				checks = append(checks, Check{Name: name, Status: StatusWarn, Detail: "not registered", Fix: "frbit setup agent --agent " + string(registration.Agent)})
			case !registration.Current:
				checks = append(checks, Check{Name: name, Status: StatusWarn, Detail: "registered with an outdated URL: " + registration.URL, Fix: "frbit mcp install --agent " + string(registration.Agent)})
			default:
				checks = append(checks, Check{Name: name, Status: StatusOK, Detail: "registered"})
			}
		}
	}
	installed, err := p.Skills()
	switch {
	case err != nil:
		checks = append(checks, Check{Name: "Skills", Status: StatusWarn, Detail: err.Error()})
	case len(installed) == 0:
		checks = append(checks, Check{Name: "Skills", Status: StatusWarn, Detail: "not installed", Fix: "frbit skills install"})
	default:
		seen := map[agent.Agent]string{}
		for _, installation := range installed {
			seen[installation.Agent] = installation.Version
		}
		parts := make([]string, 0, len(seen))
		for _, installation := range installed {
			if version, ok := seen[installation.Agent]; ok {
				parts = append(parts, fmt.Sprintf("%s %s", installation.Agent, version))
				delete(seen, installation.Agent)
			}
		}
		checks = append(checks, Check{Name: "Skills", Status: StatusOK, Detail: strings.Join(parts, ", ")})
	}
	return checks
}

func failed(checks []Check) bool {
	for _, check := range checks {
		if check.Status == StatusFail {
			return true
		}
	}
	return false
}

func writeJSON(out io.Writer, checks []Check) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		OK     bool    `json:"ok"`
		Checks []Check `json:"checks"`
	}{OK: !failed(checks), Checks: checks})
}

var marks = map[string]string{StatusOK: "ok  ", StatusWarn: "warn", StatusFail: "FAIL", StatusSkip: "skip"}

func writeText(out io.Writer, checks []Check) error {
	width := 0
	for _, check := range checks {
		width = max(width, len(check.Name))
	}
	for _, check := range checks {
		if _, err := fmt.Fprintf(out, "%s  %-*s  %s\n", marks[check.Status], width, check.Name, check.Detail); err != nil {
			return err
		}
		if check.Fix != "" && check.Status != StatusOK {
			if _, err := fmt.Fprintf(out, "      %-*s  → %s\n", width, "", check.Fix); err != nil {
				return err
			}
		}
	}
	return nil
}

func defaultProbes(cmd *cobra.Command, factory *app.Factory) *Probes {
	checker := update.NewChecker(factory.HTTPClient)
	checker.Fresh = true
	mcp := agentmcp.NewService(agentmcp.Options{})
	skills := agentskills.NewService(agentskills.Options{HTTPClient: factory.HTTPClient, UserAgent: "frbit/" + factory.Version})
	resolver := link.Resolver{}

	client := func() (*api.Client, error) {
		profile, err := cmdutil.Profile(cmd)
		if err != nil {
			return nil, err
		}
		host, err := cmdutil.Host(cmd, factory)
		if err != nil {
			return nil, err
		}
		credential, _, err := factory.Credential(profile)
		if err != nil {
			return nil, err
		}
		return cmdutil.CredentialClient(factory, host, profile, credential)
	}

	return &Probes{
		Latest: checker.Latest,
		SignIn: func(ctx context.Context) (string, error) {
			c, err := client()
			if err != nil {
				return "", err
			}
			you, err := c.You(ctx)
			if err != nil {
				return "", fmt.Errorf("the API refused the credential: %w", err)
			}
			return you.You.Email, nil
		},
		LookPath: exec.LookPath,
		ToolVersion: func(ctx context.Context, name string) string {
			flag := "--version"
			if name == "ssh" {
				flag = "-V"
			}
			output, _ := exec.CommandContext(ctx, name, flag).CombinedOutput()
			line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
			return line
		},
		Linked: func(ctx context.Context) ([]string, bool, error) {
			dir, err := os.Getwd()
			if err != nil {
				return nil, false, err
			}
			root, inCheckout := resolver.TopLevel(ctx, dir)
			if !inCheckout {
				return nil, false, nil
			}
			entries, err := link.ReadFile(root)
			if err != nil {
				return nil, true, err
			}
			return entries.Labels(), true, nil
		},
		Environment: func(ctx context.Context) (Environment, error) {
			c, err := client()
			if err != nil {
				return Environment{}, err
			}
			dir, err := os.Getwd()
			if err != nil {
				return Environment{}, err
			}
			resolution, err := resolver.Find(ctx, c, dir, "")
			if err != nil {
				return Environment{}, err
			}
			env, err := link.Environment(ctx, c, resolution.ID)
			if err != nil {
				return Environment{}, err
			}
			target, err := sshexec.Target(env, cmdutil.IsDefaultHost(c.Host()))
			if err != nil {
				return Environment{}, err
			}
			from := "from the repository connected to the app"
			switch resolution.Source {
			case link.SourceFile:
				from = "label " + resolution.Label
			case link.SourceEnv:
				from = "from " + link.EnvVar
			}
			return Environment{ID: resolution.ID, From: from, Target: target}, nil
		},
		SSH: func(ctx context.Context, target string) error {
			return sshexec.Run(ctx, sshexec.System{}, target, "true", false, nil, io.Discard, io.Discard)
		},
		MCP: func(ctx context.Context) ([]agentmcp.Registration, error) {
			agents, err := mcp.ResolveAgents(nil)
			if err != nil {
				return nil, err
			}
			return mcp.Inspect(ctx, agents)
		},
		Skills: func() ([]agentskills.Installation, error) {
			return skills.Inspect(false, nil)
		},
	}
}
