package logout

import (
	"errors"
	"fmt"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/filelock"
	"github.com/fortrabbit/frbit-cli/internal/oauth"
	"github.com/spf13/cobra"
)

type Options struct {
	Factory *app.Factory
	Command *cobra.Command
}

const longHelp = `Remove the stored credential for the selected profile.

A browser sign-in is revoked on the server first, so it disappears from Connections in the
dashboard. If the server cannot be reached, the credential is still removed on this machine
and the sign-in stays under Connections until you disconnect it there. A stored API token is
only removed locally; revoke it under API tokens in the dashboard.

With FRBIT_TOKEN set, logout warns that the token keeps being used.`

func NewCmdLogout(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored public API credential",
		Long:  longHelp,
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
	command := options.Command
	profile, err := cmdutil.Profile(command)
	if err != nil {
		return err
	}
	if options.Factory.CredentialLockPath == "" {
		return errors.New("no credential lock path is configured")
	}
	lock, err := filelock.Acquire(command.Context(), options.Factory.CredentialLockPath)
	if err != nil {
		return fmt.Errorf("log out: %w", err)
	}
	defer func() { _ = lock.Release() }()

	stderr := command.ErrOrStderr()
	if credential, err := options.Factory.CredentialStore.Get(profile); err == nil && credential.Kind == credentials.KindOAuth && credential.RefreshToken != "" {
		if revokeErr := revoke(options, credential); revokeErr != nil {
			if _, err := fmt.Fprintf(stderr, "Could not revoke the sign-in on the server: %v\nIt stays listed under Connections in the dashboard until you disconnect it there.\n", revokeErr); err != nil {
				return err
			}
		}
	}
	if err := options.Factory.CredentialStore.Delete(profile); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(command.OutOrStdout(), "Removed stored credentials for profile %q.\n", profile); err != nil {
		return err
	}
	if cmdutil.FrbitTokenSet() {
		_, err = fmt.Fprintln(stderr, cmdutil.FrbitTokenWarning)
	}
	return err
}

// revoke uses the host the credential was issued for, falling back to the configured host.
func revoke(options *Options, credential credentials.Credential) error {
	host := credential.Issuer
	if host == "" {
		var err error
		if host, err = cmdutil.Host(options.Command, options.Factory); err != nil {
			return err
		}
	}
	return oauth.Revoke(options.Command.Context(), host, options.Factory.HTTPClient, credential.RefreshToken)
}
