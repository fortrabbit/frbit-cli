package status

import (
	"fmt"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type Options struct {
	Factory *app.Factory
	Command *cobra.Command
}

func NewCmdStatus(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	return &cobra.Command{
		Use:   "status",
		Short: "Validate the active public API credential",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Command = cmd
			if runF != nil {
				return runF(options)
			}
			return run(options)
		},
	}
}

func run(options *Options) error {
	profile, err := cmdutil.Profile(options.Command)
	if err != nil {
		return err
	}
	host, err := cmdutil.Host(options.Command, options.Factory)
	if err != nil {
		return err
	}
	token, source, err := options.Factory.Token(profile)
	if err != nil {
		return err
	}
	client, err := cmdutil.Client(options.Factory, host, token)
	if err != nil {
		return err
	}
	if err := client.CheckToken(options.Command.Context()); err != nil {
		return fmt.Errorf("validate token: %w", err)
	}
	_, err = fmt.Fprintf(options.Command.OutOrStdout(), "Authenticated profile %q against %s using %s.\n", profile, host, source)
	return err
}
