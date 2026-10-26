package status

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/spf13/cobra"
)

type Options struct {
	Factory *app.Factory
	Command *cobra.Command
	JSON    bool
}

func NewCmdStatus(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	command := &cobra.Command{
		Use:   "status",
		Short: "Validate the active public API credential",
		Long:  "Validate the active credential and show the profile, API host, credential kind, its expiry, and the next setup step.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Command = cmd
			if runF != nil {
				return runF(options)
			}
			return run(options)
		},
	}
	command.Flags().BoolVar(&options.JSON, "json", false, "Print the status as JSON")
	return command
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
	credential, source, err := options.Factory.Credential(profile)
	if err != nil {
		return err
	}
	client, err := cmdutil.CredentialClient(options.Factory, host, profile, credential)
	if err != nil {
		return err
	}
	you, err := client.You(options.Command.Context())
	if err != nil {
		return fmt.Errorf("validate credential: %w", err)
	}
	if credential.Kind == credentials.KindOAuth {
		// The request above may have refreshed the credential.
		if stored, err := options.Factory.CredentialStore.Get(profile); err == nil {
			credential = stored
		}
	}

	out := options.Command.OutOrStdout()
	if options.JSON {
		status := struct {
			Profile   string          `json:"profile"`
			Host      string          `json:"host"`
			Source    string          `json:"source"`
			Kind      string          `json:"kind"`
			ExpiresAt *time.Time      `json:"expiresAt"`
			Email     string          `json:"email"`
			Next      json.RawMessage `json:"next"`
		}{Profile: profile, Host: host, Source: source, Kind: credential.Kind, Email: you.You.Email, Next: you.Next}
		if !credential.ExpiresAt.IsZero() {
			status.ExpiresAt = &credential.ExpiresAt
		}
		encoded, err := json.Marshal(status)
		if err != nil {
			return fmt.Errorf("encode JSON output: %w", err)
		}
		_, err = fmt.Fprintf(out, "%s\n", encoded)
		return err
	}

	if _, err := fmt.Fprintf(out, "Authenticated profile %q against %s using %s.\n", profile, host, source); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Account: %s\nCredential: %s\n", you.You.Email, describe(credential)); err != nil {
		return err
	}
	return cmdutil.WriteNext(out, you.You.Next)
}

func describe(credential credentials.Credential) string {
	if credential.Kind != credentials.KindOAuth {
		return "API token"
	}
	if credential.ExpiresAt.IsZero() {
		return "browser sign-in, refreshes automatically"
	}
	return fmt.Sprintf("browser sign-in, access token expires %s, refreshes automatically", credential.ExpiresAt.Local().Format(time.RFC3339))
}
