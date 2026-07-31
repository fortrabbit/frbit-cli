package logout

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

func NewCmdLogout(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored public API credential",
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
	if err := options.Factory.CredentialStore.Delete(profile); err != nil {
		return err
	}
	_, err = fmt.Fprintf(options.Command.OutOrStdout(), "Removed stored credentials for profile %q.\n", profile)
	return err
}
