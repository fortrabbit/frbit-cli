package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/oauth"
	"github.com/spf13/cobra"
)

func Host(cmd *cobra.Command, factory *app.Factory) (string, error) {
	host, err := cmd.Flags().GetString("host")
	if err != nil {
		return "", fmt.Errorf("read host flag: %w", err)
	}
	return factory.Host(host)
}

func Profile(cmd *cobra.Command) (string, error) {
	profile, err := cmd.Flags().GetString("profile")
	if err != nil {
		return "", fmt.Errorf("read profile flag: %w", err)
	}
	if profile == "" {
		return app.DefaultProfile, nil
	}
	return profile, nil
}

func Client(factory *app.Factory, host string, token string) (*api.Client, error) {
	return api.NewClient(host, token, factory.HTTPClient, fmt.Sprintf("frbit/%s", factory.Version))
}

// CredentialClient builds a client that refreshes an OAuth credential of profile as needed.
func CredentialClient(factory *app.Factory, host string, profile string, credential credentials.Credential) (*api.Client, error) {
	if credential.Kind != credentials.KindOAuth {
		return Client(factory, host, credential.AccessToken)
	}
	if !oauth.SameOrigin(host, credential.Issuer) {
		return nil, fmt.Errorf("profile %q is signed in to %s, not %s; run `frbit auth login` for that host", profile, credential.Issuer, host)
	}
	if factory.CredentialLockPath == "" {
		return nil, fmt.Errorf("no credential lock path is configured")
	}
	refresh := func(ctx context.Context, refreshToken string) (credentials.Credential, error) {
		return oauth.Refresh(ctx, host, factory.HTTPClient, refreshToken)
	}
	tokens := api.NewOAuthTokens(host, factory.CredentialStore, profile, factory.CredentialLockPath, credential, refresh)
	return api.NewClientWithTokens(host, tokens, factory.HTTPClient, fmt.Sprintf("frbit/%s", factory.Version))
}

func APIClient(command *cobra.Command, factory *app.Factory) (*api.Client, error) {
	host, err := Host(command, factory)
	if err != nil {
		return nil, err
	}
	if err := WarnNonDefaultHost(command, host); err != nil {
		return nil, err
	}
	profile, err := Profile(command)
	if err != nil {
		return nil, err
	}
	credential, _, err := factory.Credential(profile)
	if err != nil {
		return nil, err
	}
	client, err := CredentialClient(factory, host, profile, credential)
	if err != nil {
		return nil, err
	}
	client.Warnings = command.ErrOrStderr()
	return client, nil
}

// OwnJSONErrors marks a command whose --json output already reports its errors, so no problem document follows.
const OwnJSONErrors = "frbit/own-json-errors"

// WriteProblemJSON prints the problem document of an API error to out when command ran with --json.
func WriteProblemJSON(command *cobra.Command, err error, out io.Writer) bool {
	if command == nil || command.Annotations[OwnJSONErrors] != "" {
		return false
	}
	flag := command.Flags().Lookup("json")
	if flag == nil || flag.Value.String() != "true" {
		return false
	}
	var httpErr *api.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Problem == nil {
		return false
	}
	_, writeErr := fmt.Fprintf(out, "%s\n", httpErr.Problem.Raw)
	return writeErr == nil
}

// WarnNonDefaultHost flags a host set by flag, environment or config, since a cloned repo can set FRBIT_HOST.
func WarnNonDefaultHost(command *cobra.Command, host string) error {
	if IsDefaultHost(host) {
		return nil
	}
	_, err := fmt.Fprintf(command.ErrOrStderr(), "Warning: sending API credentials to non-default host %s. Verify this host before continuing.\n", host)
	return err
}

// IsDefaultHost reports whether host is the production API.
func IsDefaultHost(host string) bool {
	parsed, err := url.Parse(strings.TrimSpace(host))
	if err != nil {
		return false
	}
	defaultParsed, _ := url.Parse(config.DefaultHost)
	return strings.EqualFold(parsed.Scheme, defaultParsed.Scheme) && strings.EqualFold(parsed.Host, defaultParsed.Host) && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func TrimToken(token string) string {
	return strings.TrimSpace(token)
}
