package root

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/apps"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/spf13/cobra"
)

func NewCmdRoot(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{
		Use:           "frbit",
		Short:         "The command line interface for the fortrabbit public API",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       factory.Version,
		Long:          "Manage your fortrabbit resources using the public API.",
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			notifyUpdate(cmd, factory)
		},
	}
	command.SetOut(factory.IOStreams.Out)
	command.SetErr(factory.IOStreams.ErrOut)
	command.PersistentFlags().String("host", "", "Public API origin (or FRBIT_HOST)")
	command.PersistentFlags().String("profile", app.DefaultProfile, "Credential profile")

	command.AddCommand(
		auth.NewCmdAuth(factory),
		apps.NewCmdApps(factory),
		resource.NewCmdGroup(factory, environmentSpec()),
		resource.NewCmdGroup(factory, deploymentSpec()),
		resource.NewCmdGroup(factory, domainSpec()),
		resource.NewCmdGroup(factory, personSpec()),
		resource.NewCmdGroup(factory, teamSpec()),
		resource.NewCmdGroup(factory, paymentMethodSpec()),
		newCmdVersion(factory),
	)

	return command
}

func notifyUpdate(command *cobra.Command, factory *app.Factory) {
	if !factory.IOStreams.IsErrTTY || factory.CheckForUpdate == nil || os.Getenv("FRBIT_NO_UPDATE_NOTIFIER") != "" {
		return
	}

	ctx, cancel := context.WithTimeout(command.Context(), 1200*time.Millisecond)
	defer cancel()
	latest, err := factory.CheckForUpdate(ctx, factory.Version)
	if err != nil || latest == "" {
		return
	}

	_, _ = fmt.Fprintf(
		command.ErrOrStderr(),
		"\nUpdate available: v%s → %s. See https://docs.fortrabbit.com/platform/concepts/cli\n",
		strings.TrimPrefix(factory.Version, "v"),
		latest,
	)
}

func environmentSpec() resource.Spec {
	return resource.Spec{
		Use: "environments", Singular: "environment", Path: "/environments", Short: "List and inspect environments", SupportsPage: true, SupportsFilter: true,
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "STATE", Key: "state"}, {Header: "SOFTWARE", Key: "softwareVersion"}, {Header: "UPDATED", Key: "updatedAt"}},
	}
}

func deploymentSpec() resource.Spec {
	return resource.Spec{
		Use: "deployments", Singular: "deployment", Path: "/deployments", Short: "List and inspect deployments", Logs: true,
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "ENVIRONMENT", Key: "environment"}, {Header: "BRANCH", Key: "branch"}, {Header: "COMMIT", Key: "commitHash"}, {Header: "STATUS", Key: "status"}, {Header: "COMMITTED", Key: "committedAt"}},
	}
}

func domainSpec() resource.Spec {
	return resource.Spec{
		Use: "domains", Singular: "domain", Path: "/domains", Short: "List and inspect domains", SupportsPage: true, SupportsFilter: true,
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "TYPE", Key: "type"}, {Header: "MAIN", Key: "isMain"}, {Header: "UPDATED", Key: "updatedAt"}},
	}
}

func personSpec() resource.Spec {
	return resource.Spec{
		Use: "people", Singular: "person", Path: "/people", Short: "List and inspect people", SupportsFilter: true,
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "EMAIL", Key: "email"}, {Header: "TYPE", Key: "type"}, {Header: "ACTIVE", Key: "active"}},
	}
}

func teamSpec() resource.Spec {
	return resource.Spec{
		Use: "teams", Singular: "team", Path: "/teams", Short: "List and inspect teams",
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "ROLE", Key: "role"}, {Header: "CREATED", Key: "createdAt"}},
	}
}

func paymentMethodSpec() resource.Spec {
	return resource.Spec{
		Use: "payment-methods", Singular: "payment method", Path: "/payment-methods", Short: "List and inspect payment methods",
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "LAST 4", Key: "last4"}, {Header: "EMAIL", Key: "email"}},
	}
}

func newCmdVersion(factory *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "frbit %s\ncommit: %s\nbuilt: %s\n", factory.Version, factory.Commit, factory.Date)
			return err
		},
	}
}
