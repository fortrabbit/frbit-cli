package cmdutil

import (
	"fmt"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
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

func TrimToken(token string) string {
	return strings.TrimSpace(token)
}
