package login

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/filelock"
	"github.com/fortrabbit/frbit-cli/internal/oauth"
	"github.com/spf13/cobra"
)

type Options struct {
	Factory    *app.Factory
	Command    *cobra.Command
	TokenStdin bool
	NoBrowser  bool
	JSON       bool
	Timeout    time.Duration
}

const longHelp = `Sign in to fortrabbit in the browser and store the credential in the system keychain.

The command prints the sign-in URL first, opens it in the default browser unless
--no-browser is set, and waits up to 15 minutes for the sign-in to finish. This also
works without a terminal, for example when a coding agent runs it in the background.
The credential refreshes itself; frbit auth logout removes it.

With --json, the command prints {"url": …, "status": "waiting"} at once and
{"status": "signed_in", "next": …} after the sign-in.

In CI or without a browser, pipe a personal API token to --token-stdin, or set
FRBIT_TOKEN for each command instead of signing in. With FRBIT_TOKEN set, login
refuses to start, since FRBIT_TOKEN overrides any stored credential.

With --json, a failure prints {"status": "error", "error": …}.`

func NewCmdLogin(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	command := &cobra.Command{
		Use:   "login",
		Short: "Sign in to fortrabbit in the browser",
		Long:  longHelp,
		Example: `  frbit auth login
  frbit auth login --json
  printf '%s' "$TOKEN" | frbit auth login --token-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Command = cmd
			if runF != nil {
				return runF(options)
			}
			return run(options)
		},
	}
	command.Flags().BoolVar(&options.TokenStdin, "token-stdin", false, "Read a personal API token from standard input instead of signing in")
	command.Flags().BoolVar(&options.NoBrowser, "no-browser", false, "Print the sign-in URL without opening a browser")
	command.Flags().BoolVar(&options.JSON, "json", false, "Print the sign-in URL and result as JSON lines")
	return command
}

func run(options *Options) error {
	if options.TokenStdin {
		return runToken(options)
	}
	var err error
	if strings.TrimSpace(os.Getenv("FRBIT_TOKEN")) != "" {
		err = errors.New("FRBIT_TOKEN is set and overrides any stored credential; unset it to sign in in the browser")
	} else {
		err = runBrowser(options)
	}
	if err != nil && options.JSON {
		_ = writeJSONLine(options.Command.OutOrStdout(), struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}{"error", err.Error()})
	}
	return err
}

func runBrowser(options *Options) error {
	command := options.Command
	profile, err := cmdutil.Profile(command)
	if err != nil {
		return err
	}
	host, err := cmdutil.Host(command, options.Factory)
	if err != nil {
		return err
	}
	dashboardURL, err := config.DashboardURL()
	if err != nil {
		return err
	}

	out := command.OutOrStdout()
	loginOptions := oauth.Options{Host: host, DashboardURL: dashboardURL, Out: out, Timeout: options.Timeout, HTTPClient: options.Factory.HTTPClient}
	if !options.NoBrowser {
		loginOptions.OpenBrowser = options.Factory.OpenBrowser
	}
	if options.JSON {
		loginOptions.Out = command.ErrOrStderr()
		loginOptions.OnURL = func(authorizeURL string) error {
			return writeJSONLine(out, struct {
				URL    string `json:"url"`
				Status string `json:"status"`
			}{authorizeURL, "waiting"})
		}
	}
	credential, err := oauth.Login(command.Context(), loginOptions)
	if err != nil {
		return err
	}
	if err := storeCredential(options, profile, credential); err != nil {
		return err
	}
	if err := options.Factory.ConfigStore.Save(config.Config{Host: host}); err != nil {
		return fmt.Errorf("save API host: %w", err)
	}

	client, err := cmdutil.CredentialClient(options.Factory, host, profile, credential)
	if err != nil {
		return err
	}
	you, err := client.You(command.Context())
	if err != nil {
		return fmt.Errorf("signed in, but reading the account failed: %w", err)
	}
	if options.JSON {
		return writeJSONLine(out, struct {
			Status string          `json:"status"`
			Next   json.RawMessage `json:"next"`
		}{"signed_in", you.Next})
	}
	if _, err := fmt.Fprintf(out, "Signed in as %s. Profile %q is authenticated against %s.\n", you.You.Email, profile, host); err != nil {
		return err
	}
	return cmdutil.WriteNext(out, you.You.Next)
}

// storeCredential takes the refresh lock so a concurrent refresh cannot overwrite the new sign-in.
func storeCredential(options *Options, profile string, credential credentials.Credential) error {
	if options.Factory.CredentialLockPath == "" {
		return errors.New("no credential lock path is configured")
	}
	lock, err := filelock.Acquire(options.Command.Context(), options.Factory.CredentialLockPath)
	if err != nil {
		return fmt.Errorf("store credential: %w", err)
	}
	defer func() { _ = lock.Release() }()
	return options.Factory.CredentialStore.Set(profile, credential)
}

func writeJSONLine(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	_, err = fmt.Fprintf(writer, "%s\n", encoded)
	return err
}

func runToken(options *Options) error {
	token, err := readToken(options)
	if err != nil {
		return err
	}
	profile, err := cmdutil.Profile(options.Command)
	if err != nil {
		return err
	}
	host, err := cmdutil.Host(options.Command, options.Factory)
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
	if err := options.Factory.CredentialStore.Set(profile, credentials.APIToken(token)); err != nil {
		return err
	}
	if err := options.Factory.ConfigStore.Save(config.Config{Host: host}); err != nil {
		return fmt.Errorf("save API host: %w", err)
	}

	_, err = fmt.Fprintf(options.Command.OutOrStdout(), "Authenticated profile %q against %s.\n", profile, host)
	return err
}

func readToken(options *Options) (string, error) {
	contents, err := io.ReadAll(options.Factory.IOStreams.In)
	if err != nil {
		return "", fmt.Errorf("read token from standard input: %w", err)
	}
	token := cmdutil.TrimToken(string(contents))
	if token == "" {
		return "", fmt.Errorf("token from standard input is empty")
	}
	return token, nil
}
