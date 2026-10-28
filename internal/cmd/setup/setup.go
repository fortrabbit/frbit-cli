package setup

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth/logout"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/spf13/cobra"
)

type mcpService interface {
	ResolveAgents([]agent.Agent) ([]agent.Agent, error)
	Preflight([]agent.Agent) error
	Install(context.Context, []agent.Agent) ([]agentmcp.Result, error)
	Inspect(context.Context, []agent.Agent) ([]agentmcp.Registration, error)
	Remove(context.Context, []agent.Agent) ([]agentmcp.Result, error)
	WithoutClaudePlugin(context.Context, []agent.Agent) ([]agent.Agent, bool)
}

type skillsService interface {
	InstallTargets(bool, []agentskills.Agent) ([]agentskills.Target, error)
	LatestRelease(context.Context) (agentskills.Release, error)
	InstallRelease(context.Context, agentskills.Release, []agentskills.Target) ([]agentskills.Installation, error)
	Removals(bool, []agentskills.Agent) ([]agentskills.Removal, error)
	Remove([]agentskills.Removal) error
}

func NewCmdSetup(factory *app.Factory, mcp mcpService, skills skillsService) *cobra.Command {
	if mcp == nil {
		mcp = agentmcp.NewService(agentmcp.Options{})
	}
	if skills == nil {
		skills = agentskills.NewService(agentskills.Options{
			HTTPClient: factory.HTTPClient,
			UserAgent:  fmt.Sprintf("frbit/%s", factory.Version),
		})
	}
	command := &cobra.Command{
		Use:   "setup",
		Short: "Set up integrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(newCmdAgent(factory, mcp, skills))
	return command
}

func newCmdAgent(factory *app.Factory, mcp mcpService, skills skillsService) *cobra.Command {
	var agentValues []string
	var remove, yes bool
	command := &cobra.Command{
		Use:   "agent",
		Short: "Register MCP and install skills for coding agents",
		Long: `Register the fortrabbit MCP server and install the fortrabbit skill for Claude Code and Codex.

With --remove, undo both and sign the CLI out. The frbit binary stays in place.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			requested, err := agentmcp.ParseAgents(agentValues)
			if err != nil {
				return err
			}
			agents, err := mcp.ResolveAgents(requested)
			if err != nil {
				return err
			}
			if remove {
				return removeAgentSetup(cmd, factory, mcp, skills, agents, yes)
			}
			// Validate every native agent CLI before writing either integration.
			if err := mcp.Preflight(agents); err != nil {
				return err
			}
			agents, pluginSkipped := mcp.WithoutClaudePlugin(cmd.Context(), agents)
			if pluginSkipped {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), claudePluginNotice); err != nil {
					return err
				}
				if len(agents) == 0 {
					return nil
				}
			}

			targets, err := skills.InstallTargets(false, agents)
			if err != nil {
				return err
			}
			release, err := skills.LatestRelease(cmd.Context())
			if err != nil {
				return err
			}
			installations, err := skills.InstallRelease(cmd.Context(), release, targets)
			if err != nil {
				return err
			}

			mcpResults, err := mcp.Install(cmd.Context(), agents)
			if err != nil {
				return err
			}
			profile, err := cmdutil.Profile(cmd)
			if err != nil {
				return err
			}
			_, tokenSource, tokenErr := factory.Credential(profile)
			return printSummary(cmd.OutOrStdout(), release, installations, mcpResults, agents, profile, tokenSource, tokenErr)
		},
	}
	command.Flags().StringArrayVar(&agentValues, "agent", nil, "Target agent (claude-code or codex); repeat to target more than one")
	command.Flags().BoolVar(&remove, "remove", false, "Remove the MCP server and the skill, and sign the CLI out")
	command.Flags().BoolVarP(&yes, "yes", "y", false, "Remove without prompting for confirmation")
	return command
}

const claudePluginNotice = "Claude Code: skipped, the fortrabbit plugin already brings the skill and the MCP server."

func printSummary(
	writer io.Writer,
	release agentskills.Release,
	installations []agentskills.Installation,
	mcpResults []agentmcp.Result,
	agents []agent.Agent,
	profile string,
	tokenSource string,
	tokenErr error,
) error {
	if _, err := fmt.Fprintln(writer, "Agent setup complete."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nMCP server:"); err != nil {
		return err
	}
	for _, result := range mcpResults {
		if _, err := fmt.Fprintf(writer, "  %-14s %-10s %s\n", result.Agent.Label()+":", result.Change, result.ConfigPath); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(writer, "\nSkills v%s:\n", release.Version); err != nil {
		return err
	}
	for _, installation := range installations {
		if _, err := fmt.Fprintf(writer, "  %-14s %s\n", installation.Agent.Label()+":", installation.Path); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(writer, "\nPublic API CLI:"); err != nil {
		return err
	}
	if tokenErr == nil {
		if _, err := fmt.Fprintf(writer, "  Profile %q has a credential configured via %s.\n", profile, tokenSource); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(writer, "  No credential configured for profile %q (optional; run `frbit auth login`).\n", profile); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer, "  Public API credentials are separate from MCP OAuth."); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	return cmdutil.PrintOAuthHints(writer, agents)
}

const (
	claudePluginRemoveNotice = "Claude Code: the fortrabbit plugin brings the skill and the MCP server; remove it with `claude plugin uninstall fortrabbit@fortrabbit`."
	disconnectHint           = "Revoke the MCP grants in the dashboard: https://dash.fortrabbit.com/you/ai-agents"
	binaryHint               = "Remove the frbit binary: https://docs.fortrabbit.com/platform/automation/cli#uninstall"
)

func removeAgentSetup(cmd *cobra.Command, factory *app.Factory, mcp mcpService, skills skillsService, agents []agent.Agent, yes bool) error {
	writer := cmd.OutOrStdout()
	if err := mcp.Preflight(agents); err != nil {
		return err
	}
	agents, pluginEnabled := mcp.WithoutClaudePlugin(cmd.Context(), agents)
	if pluginEnabled {
		if _, err := fmt.Fprintln(writer, claudePluginRemoveNotice); err != nil {
			return err
		}
	}

	var registrations []agentmcp.Registration
	var removals []agentskills.Removal
	// An empty agent list means all agents to the skills service, so skip both lookups.
	if len(agents) > 0 {
		var err error
		if registrations, err = mcp.Inspect(cmd.Context(), agents); err != nil {
			return err
		}
		if removals, err = skills.Removals(false, agents); err != nil {
			return err
		}
	}
	var registered []agent.Agent
	for _, registration := range registrations {
		if registration.Installed {
			registered = append(registered, registration.Agent)
		}
	}
	profile, err := cmdutil.Profile(cmd)
	if err != nil {
		return err
	}
	_, credentialErr := factory.CredentialStore.Get(profile)
	if credentialErr != nil && !errors.Is(credentialErr, credentials.ErrNotFound) {
		return credentialErr
	}
	signedIn := credentialErr == nil

	if len(registered) == 0 && len(removals) == 0 && !signedIn {
		_, err := fmt.Fprintln(writer, "Nothing to remove: no fortrabbit MCP server, skill or sign-in found.")
		return err
	}

	if _, err := fmt.Fprintln(writer, "This removes:"); err != nil {
		return err
	}
	for _, registration := range registrations {
		if !registration.Installed {
			continue
		}
		if _, err := fmt.Fprintf(writer, "  %-14s MCP server in %s\n", registration.Agent.Label()+":", registration.ConfigPath); err != nil {
			return err
		}
	}
	for _, removal := range removals {
		if _, err := fmt.Fprintf(writer, "  %-14s skill at %s\n", removal.Agent.Label()+":", removal.Path); err != nil {
			return err
		}
	}
	if signedIn {
		if _, err := fmt.Fprintf(writer, "  %-14s sign-in of profile %q\n", "frbit CLI:", profile); err != nil {
			return err
		}
	}
	if !yes {
		confirmed, err := cmdutil.Confirm(factory.IOStreams.In, writer, "Continue? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			_, err := fmt.Fprintln(writer, "Aborted.")
			return err
		}
	}

	if len(registered) > 0 {
		results, err := mcp.Remove(cmd.Context(), registered)
		if err != nil {
			return err
		}
		for _, result := range results {
			if _, err := fmt.Fprintf(writer, "Removed the MCP server from %s\n", result.Agent.Label()); err != nil {
				return err
			}
		}
	}
	if err := skills.Remove(removals); err != nil {
		return err
	}
	for _, removal := range removals {
		if _, err := fmt.Fprintf(writer, "Removed %s\n", removal.Path); err != nil {
			return err
		}
	}
	if signedIn {
		if err := logout.Run(factory, cmd); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(writer, "Agent setup removed.\n\n%s\n%s\n", disconnectHint, binaryHint)
	return err
}
