package apps

import (
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/spf13/cobra"
)

func NewCmdApps(factory *app.Factory) *cobra.Command {
	return resource.NewCmdGroup(factory, resource.Spec{
		Use:            "apps",
		Singular:       "app",
		Path:           "/apps",
		Short:          "List and inspect apps",
		SupportsPage:   true,
		SupportsFilter: true,
		Fields: []resource.Field{
			{Header: "ID", Key: "publicId"},
			{Header: "NAME", Key: "name"},
			{Header: "DESCRIPTION", Key: "description"},
			{Header: "TRIAL", Key: "trial"},
			{Header: "UPDATED", Key: "updatedAt"},
		},
	})
}
