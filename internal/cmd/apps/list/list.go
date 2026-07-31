package list

import (
	"fmt"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type Options struct {
	Factory *app.Factory
	Command *cobra.Command
	Page    int
	JSON    bool
}

func NewCmdList(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	command := &cobra.Command{
		Use:   "list",
		Short: "List apps available to the authenticated person",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Command = cmd
			if runF != nil {
				return runF(options)
			}
			return run(options)
		},
	}
	command.Flags().IntVar(&options.Page, "page", 1, "Page number")
	command.Flags().BoolVar(&options.JSON, "json", false, "Print the API response as JSON")
	return command
}

func run(options *Options) error {
	if options.Page < 1 {
		return fmt.Errorf("--page must be at least 1")
	}

	host, err := cmdutil.Host(options.Command, options.Factory)
	if err != nil {
		return err
	}
	profile, err := cmdutil.Profile(options.Command)
	if err != nil {
		return err
	}
	token, _, err := options.Factory.Token(profile)
	if err != nil {
		return err
	}
	client, err := cmdutil.Client(options.Factory, host, token)
	if err != nil {
		return err
	}

	response, err := client.Apps(options.Command.Context(), options.Page)
	if err != nil {
		return err
	}
	if options.JSON {
		_, err = fmt.Fprintf(options.Command.OutOrStdout(), "%s\n", response.Raw)
		return err
	}

	return writeTable(options.Command.OutOrStdout(), response.Apps)
}

func writeTable(output interface{ Write([]byte) (int, error) }, apps []api.App) error {
	if len(apps) == 0 {
		_, err := fmt.Fprintln(output, "No apps found.")
		return err
	}

	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "ID\tNAME\tDESCRIPTION\tTRIAL\tUPDATED"); err != nil {
		return err
	}
	for _, app := range apps {
		description := ""
		if app.Description != nil {
			description = *app.Description
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%t\t%s\n", app.PublicID, app.Name, description, app.Trial, app.UpdatedAt); err != nil {
			return err
		}
	}
	return table.Flush()
}
