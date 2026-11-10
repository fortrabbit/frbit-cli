package db

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

// The dump runs on the environment, which holds the database credentials; pipefail keeps a mysqldump failure from hiding behind gzip.
const pullCommand = `set -o pipefail; mysqldump --single-transaction --set-gtid-purged=OFF --no-tablespaces "$FORTRABBIT_DB_NAME" | gzip`

// minDump is less than the header mysqldump writes for an empty database, so anything shorter means no dump arrived.
const minDump = 100

// runner and checkTool are replaced in tests.
var (
	runner    sshexec.Runner = sshexec.System{}
	checkTool                = sshexec.Check
)

func NewCmdDb(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "db", Short: "Copy the database of an environment", Args: cobra.NoArgs}
	command.AddCommand(newCmdPull(factory))
	return command
}

func newCmdPull(factory *app.Factory) *cobra.Command {
	var output string
	var toStdout, force bool
	command := &cobra.Command{
		Use:   "pull [environment]",
		Short: "Dump the database of an environment to a local file",
		Long: `Dump the database of an environment to a local gzipped file. It never touches a local database: import the file yourself, or let ddev do it.

mysqldump runs on the environment over SSH with your own SSH key, so no database credentials reach this machine. The default output is <environment>.sql.gz in the current folder; --output sets a file or folder. An existing file is not overwritten unless you pass --force. --stdout writes plain SQL to stdout instead.

The environment is an ID or a label from .fortrabbit/environments.yml; without one, the environment of the checkout is used (see frbit link).`,
		Example: `  frbit db pull en-a1b2c3
  frbit db pull --output ./dumps/
  frbit db pull en-a1b2c3 --stdout | gzip > db.sql.gz`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if toStdout && output != "" {
				return errors.New("pass --output or --stdout, not both")
			}
			if err := checkTool("ssh"); err != nil {
				return err
			}
			ctx := cmd.Context()
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			id, err := link.ForStreams(factory.IOStreams).Resolve(ctx, client, dir, arg)
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
			p := puller{id: id, target: target, interactive: factory.IOStreams.IsTTY, stderr: cmd.ErrOrStderr()}
			if toStdout {
				return p.toWriter(ctx, cmd.OutOrStdout())
			}
			path := output
			if info, statErr := os.Stat(output); output == "" || (statErr == nil && info.IsDir()) {
				path = filepath.Join(output, id+".sql.gz")
			}
			if !force {
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					return fmt.Errorf("%s exists; pass --force to overwrite", path)
				}
			}
			if err := p.toFile(ctx, path); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Saved %s\n", path)
			return err
		},
	}
	command.Flags().StringVar(&output, "output", "", "File or folder to write the gzipped dump to (default: ./<environment>.sql.gz)")
	command.Flags().BoolVar(&toStdout, "stdout", false, "Write plain SQL to stdout instead of a file")
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

// toFile writes the dump to a temp file next to path and renames it only when the dump is complete and not empty.
func (p puller) toFile(ctx context.Context, path string) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".frbit-db-pull-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmp := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	err = p.run(ctx, file)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = p.checkFile(tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (p puller) checkFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	n, err := decompress(io.Discard, file, minDump)
	if err != nil {
		return err
	}
	return p.checkSize(n)
}

func (p puller) checkSize(n int64) error {
	if n < minDump {
		return fmt.Errorf("the dump from %s is empty", p.id)
	}
	return nil
}

// toWriter streams the dump to out as plain SQL.
func (p puller) toWriter(ctx context.Context, out io.Writer) error {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := p.run(ctx, writer)
		_ = writer.CloseWithError(err)
		done <- err
	}()
	n, copyErr := decompress(out, reader, -1)
	_ = reader.CloseWithError(errors.New("stopped reading the dump"))
	if err := <-done; err != nil {
		return err
	}
	if copyErr != nil {
		return copyErr
	}
	return p.checkSize(n)
}

// decompress copies up to limit bytes (all when negative) of the gzip stream in to out; an empty stream counts as zero bytes.
func decompress(out io.Writer, in io.Reader, limit int64) (int64, error) {
	reader, err := gzip.NewReader(in)
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("the dump is not gzip data: %w", err)
	}
	var source io.Reader = reader
	if limit >= 0 {
		source = io.LimitReader(reader, limit)
	}
	n, err := io.Copy(out, source)
	if err != nil {
		return n, fmt.Errorf("read the dump: %w", err)
	}
	return n, nil
}
