package mysql

import (
	"fmt"
	"sort"
	"strings"
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
			response, err := client.GetResource(cmd.Context(), "/v1/environments/"+args[0]+"/mysql")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if printJSON {
				_, err = fmt.Fprintf(out, "%s\n", response.Raw)
				return err
			}
			keys := make([]string, 0, len(response.Resource))
			for key := range response.Resource {
				if strings.Contains(strings.ToLower(key), "password") || strings.HasPrefix(key, "@") {
					continue
				}
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool {
				return fieldRank(keys[i]) < fieldRank(keys[j]) || fieldRank(keys[i]) == fieldRank(keys[j]) && keys[i] < keys[j]
			})
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

func fieldRank(key string) int {
	for index, name := range []string{"host", "port", "user", "database", "version"} {
		if key == name {
			return index
		}
	}
	return 99
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
			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, out, "password rotation", id, confirmation, rotateWarning)
			if err != nil {
				return err
			}
			if !confirmed {
				_, err = fmt.Fprintln(out, "Aborted.")
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if _, err := client.PostAction(cmd.Context(), "/v1/environments/"+id+"/mysql/regenerate-password"); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Rotating the MySQL password of %s. The app reads the new one from its environment variables once applied; redeploy or restart it.\n", id)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm by repeating the environment ID")
	return command
}
