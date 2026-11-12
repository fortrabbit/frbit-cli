// Package doctor checks whether this machine is ready to work with fortrabbit.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

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
	// MCP reports the registrations and whether the fortrabbit Claude Code plugin brings MCP and skills instead.
	MCP    func(ctx context.Context) (registrations []agentmcp.Registration, viaPlugin bool, err error)
	Skills func() ([]agentskills.Installation, error)
	// Login is the sign-in command for the profile and host doctor checked.
	Login string
}

// ErrRefused marks a credential the API turned down, as opposed to an API it could not reach.
var ErrRefused = errors.New("the API refused the credential")

const (
	sshTimeout   = 20 * time.Second
	probeTimeout = 15 * time.Second
)

// bounded keeps an unattended run, as at the end of install.sh, from hanging on one probe.
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, probeTimeout)
}

func NewCmdDoctor(factory *app.Factory, probes *Probes) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Annotations: map[string]string{cmdutil.OwnJSONErrors: "true"},
		Use:         "doctor",
		Short:       "Check this machine's setup and connection to fortrabbit",
		Long: `Check the CLI version, the sign-in, the local tools, the linked environment, the SSH connection and the coding agents, one line each, most problems with their fix.

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
	ctx, cancel := bounded(ctx)
	defer cancel()
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
	ctx, cancel := bounded(ctx)
	defer cancel()
	login := p.Login
	if login == "" {
		login = "frbit auth login"
	}
	email, err := p.SignIn(ctx)
	switch {
	case errors.Is(err, credentials.ErrNotFound):
		return Check{Name: "Sign-in", Status: StatusWarn, Detail: "not signed in", Fix: login}
	case errors.Is(err, ErrRefused):
		return Check{Name: "Sign-in", Status: StatusFail, Detail: err.Error(), Fix: login + ", or check FRBIT_TOKEN"}
	case err != nil:
		return Check{Name: "Sign-in", Status: StatusWarn, Detail: "the API did not answer: " + err.Error(), Fix: "check the network and https://status.fortrabbit.com"}
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
		toolCtx, cancel := bounded(ctx)
		checks = append(checks, Check{Name: tool.name, Status: StatusOK, Detail: p.ToolVersion(toolCtx, tool.name)})
		cancel()
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
		return Check{Name: "Project", Status: StatusOK, Detail: "no " + link.FilePath + "; the environment comes from the repository connected to the app"}
	default:
		return Check{Name: "Project", Status: StatusOK, Detail: link.FilePath + ": " + strings.Join(labels, ", ")}
	}
}

func sshCheck(ctx context.Context, p *Probes, signIn, project Check) Check {
	if signIn.Status != StatusOK {
		return Check{Name: "SSH", Status: StatusSkip, Detail: "needs a sign-in"}
	}
	if project.Status == StatusFail {
		return Check{Name: "SSH", Status: StatusSkip, Detail: "needs a readable " + link.FilePath}
	}
	envCtx, cancel := bounded(ctx)
	env, err := p.Environment(envCtx)
	cancel()
	if errors.Is(err, link.ErrNoEnvironment) {
		return Check{Name: "SSH", Status: StatusSkip, Detail: "no environment for this checkout", Fix: "frbit link <env-id>"}
	}
	if err != nil {
		return Check{Name: "SSH", Status: StatusFail, Detail: err.Error()}
	}
	sshCtx, cancelSSH := context.WithTimeout(ctx, sshTimeout)
	defer cancelSSH()
	if err := p.SSH(sshCtx, env.Target); err != nil {
		return Check{Name: "SSH", Status: StatusFail, Detail: fmt.Sprintf("%s (%s): %v", env.ID, env.From, err)}
	}
	return Check{Name: "SSH", Status: StatusOK, Detail: fmt.Sprintf("%s (%s) answers", env.ID, env.From)}
}

func agentChecks(ctx context.Context, p *Probes) []Check {
	const plugin = "from the fortrabbit Claude Code plugin"
	mcpCtx, cancel := bounded(ctx)
	registrations, viaPlugin, err := p.MCP(mcpCtx)
	cancel()
	var checks []Check
	if viaPlugin {
		checks = append(checks, Check{Name: "MCP, claude-code", Status: StatusOK, Detail: plugin})
	}
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
		checks = append(checks, Check{Name: "Skills", Status: StatusWarn, Detail: err.Error(), Fix: "frbit skills list"})
	case len(installed) == 0 && viaPlugin:
		checks = append(checks, Check{Name: "Skills", Status: StatusOK, Detail: plugin})
	case len(installed) == 0:
		checks = append(checks, Check{Name: "Skills", Status: StatusWarn, Detail: "not installed", Fix: "frbit skills install"})
	default:
		seen := map[string]bool{}
		var parts []string
		for _, installation := range installed {
			part := fmt.Sprintf("%s %s", installation.Agent, installation.Version)
			if !seen[part] {
				seen[part] = true
				parts = append(parts, part)
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
	return cmdutil.WriteJSONLine(out, struct {
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

	login := "frbit auth login"
	for _, flag := range []string{"profile", "host"} {
		if f := cmd.Flags().Lookup(flag); f != nil && f.Changed {
			login += fmt.Sprintf(" --%s %s", flag, f.Value.String())
		}
	}
	hostWarned := false
	warnHost := func() {
		if host, err := cmdutil.Host(cmd, factory); err == nil && !hostWarned {
			hostWarned = true
			_ = cmdutil.WarnNonDefaultHost(cmd, host)
		}
	}

	return &Probes{
		Login:  login,
		Latest: checker.Latest,
		SignIn: func(ctx context.Context) (string, error) {
			warnHost()
			c, err := client()
			if err != nil {
				return "", err
			}
			you, err := c.You(ctx)
			var httpErr *api.HTTPError
			if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
				return "", fmt.Errorf("%w: %w", ErrRefused, err)
			}
			if err != nil {
				return "", err
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
		MCP: func(ctx context.Context) ([]agentmcp.Registration, bool, error) {
			agents, err := mcp.ResolveAgents(nil)
			if err != nil {
				return nil, false, err
			}
			agents, viaPlugin := mcp.WithoutClaudePlugin(ctx, agents)
			if len(agents) == 0 {
				return nil, viaPlugin, nil
			}
			registrations, err := mcp.Inspect(ctx, agents)
			return registrations, viaPlugin, err
		},
		Skills: func() ([]agentskills.Installation, error) {
			return skills.Inspect(false, nil)
		},
	}
}
