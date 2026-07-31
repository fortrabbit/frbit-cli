package apps

import (
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/apps/list"
	"github.com/spf13/cobra"
)

func NewCmdApps(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{
		Use:   "apps",
		Short: "List and inspect apps",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(list.NewCmdList(factory, nil))
	return command
}
