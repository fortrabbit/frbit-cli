package skills

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type targetOptions struct {
	agents  []string
	project bool
}

func NewCmdSkills(factory *app.Factory, service *agentskills.Service) *cobra.Command {
	if service == nil {
		service = agentskills.NewService(agentskills.Options{
			HTTPClient: factory.HTTPClient,
			UserAgent:  fmt.Sprintf("frbit/%s", factory.Version),
		})
	}
	command := &cobra.Command{
		Use:   "skills",
		Short: "Manage fortrabbit agent skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(
		newCmdInstall(service),
		newCmdUpdate(service),
		newCmdList(service, factory.Version),
		newCmdRemove(factory, service),
	)
	return command
}

func newCmdInstall(service *agentskills.Service) *cobra.Command {
	options := targetOptions{}
	command := &cobra.Command{
		Use:   "install",
		Short: "Install the latest fortrabbit agent skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := agentskills.ParseAgents(options.agents)
			if err != nil {
				return err
			}
			targets, err := service.InstallTargets(options.project, agents)
			if err != nil {
				return err
			}
			release, err := service.LatestRelease(cmd.Context())
			if err != nil {
				return err
			}
			installed, err := service.InstallRelease(cmd.Context(), release, targets)
			if err != nil {
				return err
			}
			return printChanges(cmd.OutOrStdout(), "Installed", release, installed)
		},
	}
	addTargetFlags(command, &options)
	return command
}

func newCmdUpdate(service *agentskills.Service) *cobra.Command {
	options := targetOptions{}
	command := &cobra.Command{
		Use:   "update",
		Short: "Update installed fortrabbit agent skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := agentskills.ParseAgents(options.agents)
			if err != nil {
				return err
			}
			installed, err := service.Inspect(options.project, agents)
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No fortrabbit agent skills are installed in this scope.")
				return err
			}
			release, err := service.LatestRelease(cmd.Context())
			if err != nil {
				return err
			}
			if installationsCurrent(installed, release.Version) {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "fortrabbit agent skills are up to date (v%s).\n", release.Version)
				return err
			}

			installedAgents := uniqueAgents(installed)
			targets, err := service.InspectionTargets(options.project, installedAgents)
			if err != nil {
				return err
			}
			updated, err := service.InstallRelease(cmd.Context(), release, targets)
			if err != nil {
				return err
			}
			return printChanges(cmd.OutOrStdout(), "Updated", release, updated)
		},
	}
	addTargetFlags(command, &options)
	return command
}

func newCmdList(service *agentskills.Service, cliVersion string) *cobra.Command {
	options := targetOptions{}
	command := &cobra.Command{
		Use:   "list",
		Short: "List installed fortrabbit agent skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := agentskills.ParseAgents(options.agents)
			if err != nil {
				return err
			}
			installed, err := service.Inspect(options.project, agents)
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "frbit CLI version: %s\nNo fortrabbit agent skills are installed in this scope.\n", cliVersion)
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "frbit CLI version: %s\n", cliVersion); err != nil {
				return err
			}
			return printInstallations(cmd.OutOrStdout(), installed)
		},
	}
	addTargetFlags(command, &options)
	return command
}

func newCmdRemove(factory *app.Factory, service *agentskills.Service) *cobra.Command {
	options := targetOptions{}
	var yes bool
	command := &cobra.Command{
		Use:   "remove",
		Short: "Remove installed fortrabbit agent skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := agentskills.ParseAgents(options.agents)
			if err != nil {
				return err
			}
			removals, err := service.Removals(options.project, agents)
			if err != nil {
				return err
			}
			if len(removals) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No fortrabbit agent skills are installed in this scope.")
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "The following paths will be removed:"); err != nil {
				return err
			}
			for _, removal := range removals {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", removal.Path); err != nil {
					return err
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
			if err := service.Remove(removals); err != nil {
				return err
			}
			for _, removal := range removals {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", removal.Path); err != nil {
					return err
				}
			}
			return nil
		},
	}
	addTargetFlags(command, &options)
	command.Flags().BoolVarP(&yes, "yes", "y", false, "Remove without prompting for confirmation")
	return command
}

func addTargetFlags(command *cobra.Command, options *targetOptions) {
	command.Flags().StringArrayVar(&options.agents, "agent", nil, "Target agent (claude-code, codex, or copilot); repeat to target more than one")
	command.Flags().BoolVarP(&options.project, "project", "p", false, "Use the current project instead of the user-wide install")
}

func printChanges(writer io.Writer, verb string, release agentskills.Release, installed []agentskills.Installation) error {
	if _, err := fmt.Fprintf(writer, "%s fortrabbit agent skills v%s:\n", verb, release.Version); err != nil {
		return err
	}
	for _, installation := range installed {
		if _, err := fmt.Fprintf(writer, "  %-14s %s\n", installation.Agent.Label()+":", installation.Path); err != nil {
			return err
		}
	}
	return nil
}

func printInstallations(writer io.Writer, installed []agentskills.Installation) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "AGENT\tSKILL\tVERSION\tSCOPE\tPATH"); err != nil {
		return err
	}
	for _, installation := range installed {
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\n",
			installation.Agent,
			installation.Skill,
			installation.Version,
			installation.Scope,
			installation.Path,
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func installationsCurrent(installed []agentskills.Installation, version string) bool {
	for _, installation := range installed {
		if installation.Version != version {
			return false
		}
	}
	return true
}

func uniqueAgents(installed []agentskills.Installation) []agentskills.Agent {
	seen := make(map[agentskills.Agent]bool)
	var agents []agentskills.Agent
	for _, installation := range installed {
		if !seen[installation.Agent] {
			seen[installation.Agent] = true
			agents = append(agents, installation.Agent)
		}
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i] < agents[j] })
	return agents
}
