package login

import (
	"fmt"
	"io"
	"os"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type Options struct {
	Factory    *app.Factory
	Command    *cobra.Command
	TokenStdin bool
	NoBrowser  bool
}

func NewCmdLogin(factory *app.Factory, runF func(*Options) error) *cobra.Command {
	options := &Options{Factory: factory}
	command := &cobra.Command{
		Use:   "login",
		Short: "Validate and store a public API token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Command = cmd
			if runF != nil {
				return runF(options)
			}
			return run(options)
		},
	}
	command.Flags().BoolVar(&options.TokenStdin, "token-stdin", false, "Read the token from standard input")
	command.Flags().BoolVar(&options.NoBrowser, "no-browser", false, "Do not open the API token page in a browser")
	return command
}

func run(options *Options) error {
	if err := prepareTokenEntry(options); err != nil {
		return err
	}
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
	if err := options.Factory.CredentialStore.Set(profile, token); err != nil {
		return err
	}
	if err := options.Factory.ConfigStore.Save(config.Config{Host: host}); err != nil {
		return fmt.Errorf("save API host: %w", err)
	}

	_, err = fmt.Fprintf(options.Command.OutOrStdout(), "Authenticated profile %q against %s.\n", profile, host)
	return err
}

func prepareTokenEntry(options *Options) error {
	if options.TokenStdin || !options.Factory.IOStreams.IsTTY {
		return nil
	}

	tokenURL, err := config.TokenCreationURL()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(options.Command.OutOrStdout(), "Create an API token at:\n%s\n\n", tokenURL); err != nil {
		return err
	}
	if options.NoBrowser || options.Factory.OpenBrowser == nil {
		return nil
	}
	_ = options.Factory.OpenBrowser(tokenURL)
	return nil
}

func readToken(options *Options) (string, error) {
	if options.TokenStdin {
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
	if !options.Factory.IOStreams.IsTTY {
		return "", fmt.Errorf("refusing to read a token from a non-interactive terminal; use --token-stdin or FRBIT_TOKEN")
	}
	file, ok := options.Factory.IOStreams.In.(*os.File)
	if !ok {
		return "", fmt.Errorf("interactive token entry requires a terminal")
	}
	if _, err := fmt.Fprint(options.Command.OutOrStdout(), "Token: "); err != nil {
		return "", err
	}
	contents, err := term.ReadPassword(int(file.Fd()))
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	if _, err := fmt.Fprintln(options.Command.OutOrStdout()); err != nil {
		return "", err
	}
	token := cmdutil.TrimToken(string(contents))
	if token == "" {
		return "", fmt.Errorf("token is empty")
	}
	return token, nil
}
