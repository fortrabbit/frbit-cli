package cmdutil

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
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

func APIClient(command *cobra.Command, factory *app.Factory) (*api.Client, error) {
	host, err := Host(command, factory)
	if err != nil {
		return nil, err
	}
	if !isDefaultHost(host) {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "Warning: sending API credentials to non-default host %s. Verify this host before continuing.\n", host); err != nil {
			return nil, err
		}
	}
	profile, err := Profile(command)
	if err != nil {
		return nil, err
	}
	token, _, err := factory.Token(profile)
	if err != nil {
		return nil, err
	}
	return Client(factory, host, token)
}

func isDefaultHost(host string) bool {
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
