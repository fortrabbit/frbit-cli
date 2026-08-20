package setup

import (
	"context"
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type mcpService interface {
	ResolveAgents([]agent.Agent) ([]agent.Agent, error)
	Preflight([]agent.Agent) error
	Install(context.Context, []agent.Agent) ([]agentmcp.Result, error)
}

type skillsService interface {
	InstallTargets(bool, []agentskills.Agent) ([]agentskills.Target, error)
	LatestRelease(context.Context) (agentskills.Release, error)
	InstallRelease(context.Context, agentskills.Release, []agentskills.Target) ([]agentskills.Installation, error)
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
	command := &cobra.Command{
		Use:   "agent",
		Short: "Register MCP and install skills for coding agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			requested, err := agentmcp.ParseAgents(agentValues)
			if err != nil {
				return err
			}
			agents, err := mcp.ResolveAgents(requested)
			if err != nil {
				return err
			}
			// Validate every native agent CLI before writing either integration.
			if err := mcp.Preflight(agents); err != nil {
				return err
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
			_, tokenSource, tokenErr := factory.Token(profile)
			return printSummary(cmd.OutOrStdout(), release, installations, mcpResults, agents, profile, tokenSource, tokenErr)
		},
	}
	command.Flags().StringArrayVar(&agentValues, "agent", nil, "Target agent (claude-code or codex); repeat to target more than one")
	return command
}

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
