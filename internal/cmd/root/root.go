package root

import (
	"fmt"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/apps"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth"
	"github.com/spf13/cobra"
)

func NewCmdRoot(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{
		Use:           "frbit",
		Short:         "The command line interface for the fortrabbit public API",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       factory.Version,
		Long:          "frbit accesses only the fortrabbit public API at /v1.",
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	command.SetOut(factory.IOStreams.Out)
	command.SetErr(factory.IOStreams.ErrOut)
	command.PersistentFlags().String("host", "", "Public API origin (or FRBIT_HOST)")
	command.PersistentFlags().String("profile", app.DefaultProfile, "Credential profile")

	command.AddCommand(
		auth.NewCmdAuth(factory),
		apps.NewCmdApps(factory),
		newCmdVersion(factory),
	)

	return command
}

func newCmdVersion(factory *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "frbit %s\ncommit: %s\nbuilt: %s\n", factory.Version, factory.Commit, factory.Date)
			return err
		},
	}
}
