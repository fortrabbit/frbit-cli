package mysql

import (
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

const rotateWarning = "The app breaks until it reads the new password (redeploy or restart)."

func NewCmdMysql(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "mysql", Short: "Inspect the MySQL database of an environment and rotate its password", Args: cobra.NoArgs}
	command.AddCommand(newCmdGet(factory), newCmdRotatePassword(factory))
	return command
}

func newCmdGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <environment>",
		Short: "Show host, port, user and database",
		Long:  "The password is never printed. The app reads it from its environment variables.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/mysql")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if printJSON {
				_, err = fmt.Fprintf(out, "%s\n", response.Raw)
				return err
			}
			keys := make([]string, 0, 5)
			for _, key := range []string{"host", "port", "user", "database", "version"} {
				if _, ok := response.Resource[key]; ok {
					keys = append(keys, key)
				}
			}
			table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			for _, key := range keys {
				value := response.Resource[key]
				if value == nil {
					value = ""
				}
				if _, err := fmt.Fprintf(table, "%s\t%v\n", key, value); err != nil {
					return err
				}
			}
			return table.Flush()
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdRotatePassword(factory *app.Factory) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use:   "rotate-password <environment>",
		Short: "Generate a new MySQL password",
		Long:  "The new password is never printed. Once the platform has applied it, the app reads it from its environment variables. " + rotateWarning,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := cmdutil.CheckDestructive(factory, "password rotation", id, confirmation); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, cmd.ErrOrStderr(), "password rotation", id, confirmation, rotateWarning)
			if err != nil {
				return err
			}
			if !confirmed {
				_, err = fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if _, err := client.PostAction(cmd.Context(), "/v1/environments/"+url.PathEscape(id)+"/mysql/regenerate-password"); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Rotating the MySQL password of %s. The app reads the new one from its environment variables once applied; redeploy or restart it.\n", id)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm by repeating the environment ID")
	return command
}
