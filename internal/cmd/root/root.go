package root

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/apps"
	"github.com/fortrabbit/frbit-cli/internal/cmd/auth"
	"github.com/fortrabbit/frbit-cli/internal/cmd/backups"
	"github.com/fortrabbit/frbit-cli/internal/cmd/catalog"
	"github.com/fortrabbit/frbit-cli/internal/cmd/db"
	"github.com/fortrabbit/frbit-cli/internal/cmd/doctor"
	"github.com/fortrabbit/frbit-cli/internal/cmd/domains"
	"github.com/fortrabbit/frbit-cli/internal/cmd/environments"
	"github.com/fortrabbit/frbit-cli/internal/cmd/files"
	"github.com/fortrabbit/frbit-cli/internal/cmd/jobs"
	"github.com/fortrabbit/frbit-cli/internal/cmd/link"
	"github.com/fortrabbit/frbit-cli/internal/cmd/mcp"
	"github.com/fortrabbit/frbit-cli/internal/cmd/mysql"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmd/setup"
	"github.com/fortrabbit/frbit-cli/internal/cmd/skills"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	envlink "github.com/fortrabbit/frbit-cli/internal/link"
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
		newCmdCompletion(),
		auth.NewCmdAuth(factory),
		apps.NewCmdApps(factory),
		mcp.NewCmdMCP(factory, nil),
		setup.NewCmdSetup(factory, nil, nil),
		setup.NewCmdUninstall(factory),
		skills.NewCmdSkills(factory, nil),
		environments.NewCmdEnvironments(factory),
		resource.NewCmdGroup(factory, deploymentSpec()),
		domains.NewCmdDomains(factory),
		jobs.NewCmdJobs(factory),
		mysql.NewCmdMysql(factory),
		db.NewCmdDb(factory),
		files.NewCmdFiles(factory),
		backups.NewCmdBackups(factory),
		link.NewCmdLink(factory),
		resource.NewCmdGroup(factory, personSpec()),
		resource.NewCmdGroup(factory, teamSpec()),
		resource.NewCmdGroup(factory, paymentMethodSpec()),
		doctor.NewCmdDoctor(factory, nil),
		newCmdVersion(factory),
	)
	command.AddCommand(catalog.NewCommands(factory)...)

	return command
}

// Execute runs the CLI with args and returns the process exit code.
func Execute(factory *app.Factory, args []string) int {
	command := NewCmdRoot(factory)
	command.SetArgs(args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal restore the default behaviour so a second Ctrl-C kills the process.
	go func() {
		<-ctx.Done()
		stop()
	}()
	executed, err := command.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		_, _ = fmt.Fprintln(factory.IOStreams.ErrOut, "interrupted")
		return 130
	}
	if errors.Is(err, cmdutil.ErrAborted) {
		return 1
	}
	cmdutil.WriteProblemJSON(executed, err, factory.IOStreams.Out)
	_, _ = fmt.Fprintln(factory.IOStreams.ErrOut, "error:", iostreams.StripControl(err.Error()))
	return 1
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

// deploymentsEnvironment resolves -e as an ID or label; without it every deployment is listed, as scripts expect.
func deploymentsEnvironment(cmd *cobra.Command, client *api.Client, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return envlink.ReadEnvironment(cmd, client, value)
}

func deploymentSpec() resource.Spec {
	return resource.Spec{
		Use: "deployments", Singular: "deployment", Path: "/deployments", Short: "List and inspect deployments, newest first", Logs: true, SupportsPage: true,
		QueryFilters: []resource.QueryFilter{{Name: "environment", Shorthand: "e", Usage: "Only deployments of this environment, an ID or a label (default: all deployments)", Resolve: deploymentsEnvironment}},
		Fields:       []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "ENVIRONMENT", Key: "environment"}, {Header: "BRANCH", Key: "branch"}, {Header: "COMMIT", Key: "commitHash"}, {Header: "STATUS", Key: "status"}, {Header: "CREATED", Key: "createdAt"}},
	}
}

func personSpec() resource.Spec {
	return resource.Spec{
		Use: "people", Singular: "person", Path: "/people", Short: "List and inspect people", SupportsFilter: true,
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "EMAIL", Key: "email"}, {Header: "TYPE", Key: "type"}},
	}
}

func teamSpec() resource.Spec {
	return resource.Spec{
		Use: "teams", Singular: "team", Path: "/teams", Short: "List and inspect teams",
		Delete: &resource.DeleteSpec{Warning: "Deleting this team also deletes payment methods owned solely by it and every app booked on those payment methods. All files and database contents will be erased."},
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "ROLE", Key: "role"}, {Header: "CREATED", Key: "createdAt"}},
	}
}

func paymentMethodSpec() resource.Spec {
	return resource.Spec{
		Use: "payment-methods", Singular: "payment method", Path: "/payment-methods", Short: "List and inspect payment methods",
		Delete: &resource.DeleteSpec{Warning: "Deleting this payment method also deletes every app booked on it and all of their environments. All files and database contents will be erased."},
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
