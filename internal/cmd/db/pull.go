package db

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

// The dump runs on the environment, which holds the database credentials; pipefail keeps a mysqldump failure from hiding behind gzip.
const pullCommand = `set -o pipefail; mysqldump --single-transaction --set-gtid-purged=OFF --no-tablespaces "$FORTRABBIT_DB_NAME" | gzip`

// runner and checkTool are replaced in tests.
var (
	runner    sshexec.Runner = sshexec.System{}
	checkTool                = sshexec.Check
)

func NewCmdDb(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "db", Short: "Copy the database of an environment", Args: cobra.NoArgs}
	command.AddCommand(newCmdPull(factory), newCmdPush(factory))
	return command
}

func newCmdPull(factory *app.Factory) *cobra.Command {
	var output string
	var force bool
	command := &cobra.Command{
		Use:   "pull [environment]",
		Short: "Dump the database of an environment to a local file",
		Long: `Dump the database of an environment to a local gzipped file. It never touches a local database: import the file yourself, or let ddev do it.

mysqldump runs on the environment over SSH with your own SSH key, so no database credentials reach this machine. The default output is <environment>.sql.gz in the current folder; --output sets a file or folder. An existing file is not overwritten unless you pass --force.

The environment is an ID or a label from .fortrabbit/environments.yml; without one, the environment of the checkout is used (see frbit link).`,
		Example: `  frbit db pull en-a1b2c3
  frbit db pull --output ./dumps/`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "-" {
				return errors.New("db pull writes a file; --output - is not supported")
			}
			if err := checkTool("ssh"); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			id, err := link.ReadEnvironment(cmd, client, link.FirstArg(args))
			if err != nil {
				return err
			}
			env, err := link.Environment(ctx, client, id)
			if err != nil {
				return err
			}
			target, err := sshexec.Target(env)
			if err != nil {
				return err
			}
			path, err := cmdutil.OutputPath(output, id+".sql.gz", force)
			if err != nil {
				return err
			}
			p := puller{id: id, target: target, interactive: factory.IOStreams.IsTTY, stderr: cmd.ErrOrStderr()}
			if err := cmdutil.WriteFileAtomic(path, 0o600, func(out io.Writer) error { return p.run(ctx, out) }); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Saved %s\n", path)
			return err
		},
	}
	command.Flags().StringVar(&output, "output", "", "File or folder to write the gzipped dump to (default: ./<environment>.sql.gz)")
	command.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
	return command
}

type puller struct {
	id, target  string
	interactive bool
	stderr      io.Writer
}

func (p puller) run(ctx context.Context, stdout io.Writer) error {
	err := sshexec.Run(ctx, runner, p.target, pullCommand, p.interactive, nil, stdout, p.stderr)
	if err == nil || ctx.Err() != nil || sshexec.ExitCode(err) == 255 || sshexec.ExitCode(err) < 0 {
		return err
	}
	return fmt.Errorf("mysqldump failed on %s (exit %d); see the message above", p.id, sshexec.ExitCode(err))
}
