package auth

import (
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth/login"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth/logout"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth/status"
	"github.com/spf13/cobra"
)

func NewCmdAuth(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "Manage public API credentials",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(
		login.NewCmdLogin(factory, nil),
		status.NewCmdStatus(factory, nil),
		logout.NewCmdLogout(factory, nil),
	)
	return command
}
