package mcp

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type targetOptions struct {
	agents []string
}

func NewCmdMCP(factory *app.Factory, service *agentmcp.Service) *cobra.Command {
	if service == nil {
		service = agentmcp.NewService(agentmcp.Options{})
	}
	command := &cobra.Command{
		Use:   "mcp",
		Short: "Manage the fortrabbit MCP server for coding agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(
		newCmdInstall(service),
		newCmdList(service),
		newCmdRemove(factory, service),
	)
	return command
}

func newCmdInstall(service *agentmcp.Service) *cobra.Command {
	options := targetOptions{}
	command := &cobra.Command{
		Use:   "install",
		Short: "Register the fortrabbit MCP server with installed agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := resolveAgents(service, options.agents)
			if err != nil {
				return err
			}
			results, err := service.Install(cmd.Context(), agents)
			if err != nil {
				return err
			}
			if err := printResults(cmd.OutOrStdout(), "fortrabbit MCP server:", results); err != nil {
				return err
			}
			return cmdutil.PrintOAuthHints(cmd.OutOrStdout(), agents)
		},
	}
	addAgentFlag(command, &options)
	return command
}

func newCmdList(service *agentmcp.Service) *cobra.Command {
	options := targetOptions{}
	command := &cobra.Command{
		Use:   "list",
		Short: "List fortrabbit MCP registrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := resolveAgents(service, options.agents)
			if err != nil {
				return err
			}
			if err := service.Preflight(agents); err != nil {
				return err
			}
			registrations, err := service.Inspect(cmd.Context(), agents)
			if err != nil {
				return err
			}
			return printRegistrations(cmd.OutOrStdout(), registrations)
		},
	}
	addAgentFlag(command, &options)
	return command
}

func newCmdRemove(factory *app.Factory, service *agentmcp.Service) *cobra.Command {
	options := targetOptions{}
	var yes bool
	command := &cobra.Command{
		Use:   "remove",
		Short: "Remove the fortrabbit MCP server from coding agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := resolveAgents(service, options.agents)
			if err != nil {
				return err
			}
			if err := service.Preflight(agents); err != nil {
				return err
			}
			registrations, err := service.Inspect(cmd.Context(), agents)
			if err != nil {
				return err
			}
			installed := installedAgents(registrations)
			if len(installed) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "The fortrabbit MCP server is not registered with the selected agents.")
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "The fortrabbit MCP entry will be removed from:"); err != nil {
				return err
			}
			for _, registration := range registrations {
				if registration.Installed {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %-14s %s\n", registration.Agent.Label()+":", registration.ConfigPath); err != nil {
						return err
					}
				}
			}
			if !yes {
				confirmed, err := cmdutil.Confirm(factory.IOStreams.In, cmd.OutOrStdout(), "Continue? [y/N] ")
				if err != nil {
					return err
				}
				if !confirmed {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return err
				}
			}
			results, err := service.Remove(cmd.Context(), installed)
			if err != nil {
				return err
			}
			return printResults(cmd.OutOrStdout(), "Removed fortrabbit MCP server:", results)
		},
	}
	addAgentFlag(command, &options)
	command.Flags().BoolVarP(&yes, "yes", "y", false, "Remove without prompting for confirmation")
	return command
}

func resolveAgents(service *agentmcp.Service, values []string) ([]agent.Agent, error) {
	requested, err := agentmcp.ParseAgents(values)
	if err != nil {
		return nil, err
	}
	return service.ResolveAgents(requested)
}

func addAgentFlag(command *cobra.Command, options *targetOptions) {
	command.Flags().StringArrayVar(&options.agents, "agent", nil, "Target agent (claude-code or codex); repeat to target more than one")
}

func printResults(writer io.Writer, title string, results []agentmcp.Result) error {
	if _, err := fmt.Fprintln(writer, title); err != nil {
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(writer, "  %-14s %-10s %s\n", result.Agent.Label()+":", result.Change, result.ConfigPath); err != nil {
			return err
		}
	}
	return nil
}

func printRegistrations(writer io.Writer, registrations []agentmcp.Registration) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "AGENT\tSTATUS\tURL\tCONFIG"); err != nil {
		return err
	}
	for _, registration := range registrations {
		status := "not installed"
		if registration.Installed {
			status = "installed"
			if !registration.Current {
				status = "needs update"
			}
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", registration.Agent, status, registration.URL, registration.ConfigPath); err != nil {
			return err
		}
	}
	return table.Flush()
}

func installedAgents(registrations []agentmcp.Registration) []agent.Agent {
	var installed []agent.Agent
	for _, registration := range registrations {
		if registration.Installed {
			installed = append(installed, registration.Agent)
		}
	}
	return installed
}
