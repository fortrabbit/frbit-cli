package status

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		Long:  "Validate the active credential and show the profile, API host, credential kind, its expiry, and the next setup step.\n\nWith --json, the status is signed_in, signed_out (no credential, exit 0), or error.",
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
	err := report(options)
	if !options.JSON {
		return err
	}
	out := options.Command.OutOrStdout()
	if errors.Is(err, credentials.ErrNotFound) {
		// Agents poll this while a browser sign-in is pending, so a missing credential is a state, not a failure.
		return writeJSON(out, map[string]string{"status": "signed_out"})
	}
	if err != nil {
		_ = writeJSON(out, map[string]string{"status": "error", "error": err.Error()})
	}
	return err
}

func report(options *Options) error {
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
		next, err := cmdutil.PlainJSON(you.Next)
		if err != nil {
			return err
		}
		status := struct {
			Status    string          `json:"status"`
			Profile   string          `json:"profile"`
			Host      string          `json:"host"`
			Source    string          `json:"source"`
			Kind      string          `json:"kind"`
			ExpiresAt *time.Time      `json:"expiresAt"`
			Email     string          `json:"email"`
			Next      json.RawMessage `json:"next"`
		}{Status: "signed_in", Profile: profile, Host: host, Source: source, Kind: credential.Kind, Email: you.You.Email, Next: next}
		if !credential.ExpiresAt.IsZero() {
			status.ExpiresAt = &credential.ExpiresAt
		}
		return writeJSON(out, status)
	}

	if _, err := fmt.Fprintf(out, "Authenticated profile %q against %s using %s.\n", profile, host, source); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Account: %s\nCredential: %s\n", you.You.Email, describe(credential)); err != nil {
		return err
	}
	return cmdutil.WriteNext(out, you.You.Next)
}

func writeJSON(writer io.Writer, value any) error {
	return cmdutil.WriteJSONLine(writer, value)
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
