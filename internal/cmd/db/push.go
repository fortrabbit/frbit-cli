package db

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

// Remote paths are fixed and the timestamp is digits only, so nothing a person types reaches the remote shell.
const (
	importFile   = "/data/home/frbit-import.sql.gz"
	backupPrefix = "/data/home/frbit-backup-"
	backupLayout = "20060102150405"
	// db-up.sh from the agent skill stages its uploads under this pattern; a dropped connection can leave one behind.
	staleImports = "/data/home/fortrabbit-import-*.sql.gz"
)

var gzipMagic = []byte{0x1f, 0x8b}

// now is replaced in tests.
var now = time.Now

func newCmdPush(factory *app.Factory) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use:   "push <file|-> [environment]",
		Short: "Replace the database of an environment with a local dump",
		Long: `Import a local SQL dump into the database of an environment. Every table in the dump is dropped and recreated; tables that exist only on the environment stay.

The file is plain SQL (gzipped while uploading) or gzipped SQL; - reads the dump from stdin, which must be piped in, not a terminal. Everything runs on the environment over SSH with your own SSH key: first a backup dump of the current database to /data/home/frbit-backup-<UTC time>.sql.gz, then the upload, a gzip check, the import with mysql, and the removal of the upload. When a step after the backup fails, the backup path and the command to restore it are printed.

It asks you to type the environment ID on a terminal; without one, or when the dump comes from stdin, pass --confirm <environment>.

The environment is an ID or a label from .fortrabbit/environments.yml; without one, the environment of the checkout is used (see frbit link).`,
		Example: `  frbit db push dump.sql.gz en-a1b2c3
  frbit db push dump.sql
  gunzip -c dump.sql.gz | frbit db push - en-a1b2c3 --confirm en-a1b2c3`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			stderr := cmd.ErrOrStderr()
			fromStdin := args[0] == "-"
			if fromStdin && factory.IOStreams.IsTTY {
				return errors.New("- reads the dump from stdin; pipe a file in")
			}
			input, err := openInput(args[0], factory.IOStreams.In)
			if err != nil {
				return err
			}
			defer input.Close()
			if err := checkTool("ssh"); err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			arg := ""
			if len(args) == 2 {
				arg = args[1]
			}
			resolver := link.ForStreams(factory.IOStreams)
			if fromStdin {
				resolver = link.Resolver{}
			}
			id, err := resolver.Resolve(ctx, client, dir, arg)
			if err != nil {
				return err
			}
			if fromStdin && confirmation == "" {
				return fmt.Errorf("stdin carries the dump and cannot answer a prompt; pass --confirm %s", id)
			}
			if err := cmdutil.CheckDestructive(factory, "database push", id, confirmation); err != nil {
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
			warning := fmt.Sprintf("This replaces the database of %s with %s: every table in the dump is dropped and recreated. A backup dump of the current database is taken on the environment first.", id, input.name)
			confirmed, err := cmdutil.ConfirmDestructiveContext(ctx, factory, stderr, "database push", id, confirmation, warning)
			if err != nil {
				return err
			}
			if !confirmed {
				_, err = fmt.Fprintln(stderr, "Aborted.")
				return err
			}
			p := pusher{
				id:          id,
				target:      target,
				backup:      backupPrefix + now().UTC().Format(backupLayout) + ".sql.gz",
				interactive: factory.IOStreams.IsTTY && !fromStdin,
				stderr:      stderr,
			}
			return p.push(ctx, input)
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm by repeating the environment ID")
	return command
}

// input is a checked local dump; gzipped tells whether it is uploaded as is or gzipped on the way.
type input struct {
	name    string
	reader  io.Reader
	gzipped bool
	closer  io.Closer
}

func (i *input) Close() error {
	if i.closer == nil {
		return nil
	}
	return i.closer.Close()
}

// openInput checks the dump locally so an empty or broken file fails before any request or SSH call.
func openInput(path string, stdin io.Reader) (*input, error) {
	if path == "-" {
		buffered := bufio.NewReader(stdin)
		head, err := buffered.Peek(len(gzipMagic))
		if len(head) == 0 {
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("read stdin: %w", err)
			}
			return nil, errors.New("stdin is empty; nothing to import")
		}
		return &input{name: "the dump from stdin", reader: buffered, gzipped: bytes.Equal(head, gzipMagic)}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	in, err := checkFile(path, file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return in, nil
}

func checkFile(path string, file *os.File) (*input, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a folder, not a dump", path)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("%s is empty; nothing to import", path)
	}
	head := make([]byte, len(gzipMagic))
	n, _ := io.ReadFull(file, head)
	gzipped := n == len(head) && bytes.Equal(head, gzipMagic)
	if !gzipped && strings.HasSuffix(strings.ToLower(path), ".gz") {
		return nil, fmt.Errorf("%s is not gzip data", path)
	}
	if gzipped {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		n, err := decompress(io.Discard, file, -1)
		if err != nil {
			return nil, fmt.Errorf("%s is a damaged gzip file: %w", path, err)
		}
		if n == 0 {
			return nil, fmt.Errorf("%s is empty; nothing to import", path)
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return &input{name: path, reader: file, gzipped: gzipped, closer: file}, nil
}

type pusher struct {
	id, target, backup string
	interactive        bool
	stderr             io.Writer
}

func (p pusher) run(ctx context.Context, remote string, stdin io.Reader) error {
	return sshexec.Run(ctx, runner, p.target, remote, p.interactive, stdin, p.stderr, p.stderr)
}

func (p pusher) restoreCommand() string {
	return fmt.Sprintf(`ssh %s 'set -o pipefail; gunzip -c %s | mysql "$FORTRABBIT_DB_NAME"'`, p.target, p.backup)
}

// report prints the state of the database and how to restore the backup.
func (p pusher) report(state string) {
	_, _ = fmt.Fprintf(p.stderr, "%s\nBackup of the previous database: %s on %s\nRestore it with:\n  %s\n", state, p.backup, p.id, p.restoreCommand())
}

// stepError keeps a cancellation or a rejected SSH key as is and names the failed step otherwise.
func stepError(err error, step string) error {
	if errors.Is(err, context.Canceled) || sshexec.ExitCode(err) == 255 || sshexec.ExitCode(err) < 0 {
		return err
	}
	return fmt.Errorf("%s (exit %d); see the message above", step, sshexec.ExitCode(err))
}

// cleanup removes the uploaded file even after Ctrl-C; an import still running on the environment keeps reading its open copy.
func (p pusher) cleanup(ctx context.Context) {
	p.remove(ctx, importFile)
}

// remove deletes a fixed remote path, also after Ctrl-C, and says when it could not.
func (p pusher) remove(ctx context.Context, path string) {
	if err := p.removeErr(ctx, path); err != nil {
		_, _ = fmt.Fprintf(p.stderr, "Could not remove %s on %s: %v\n", path, p.id, err)
	}
}

func (p pusher) removeErr(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return p.run(ctx, "rm -f "+path, nil)
}

func (p pusher) push(ctx context.Context, in *input) error {
	_, _ = fmt.Fprintf(p.stderr, "Backing up the database of %s to %s\n", p.id, p.backup)
	// The dump only gets the backup name once complete, so a killed run never leaves a truncated file that looks like a backup.
	backup := fmt.Sprintf(`set -o pipefail; mysqldump --single-transaction --set-gtid-purged=OFF --no-tablespaces "$FORTRABBIT_DB_NAME" | gzip > %[1]s.part && mv %[1]s.part %[1]s || { status=$?; rm -f %[1]s.part; exit $status; }`, p.backup)
	if err := p.run(ctx, backup, nil); err != nil {
		// The dump may have started before the connection dropped; a cleanup that cannot connect either stays quiet.
		if sshexec.ExitCode(err) == 255 {
			_ = p.removeErr(ctx, p.backup+".part")
		} else {
			p.remove(ctx, p.backup+".part")
		}
		if ctx.Err() != nil {
			_, _ = fmt.Fprintf(p.stderr, "Cancelled during the backup; the database of %s was not changed.\n", p.id)
			return err
		}
		if sshexec.ExitCode(err) == 255 {
			return err
		}
		return fmt.Errorf("the backup on %s failed (exit %d), so the database was not changed; see the message above", p.id, sshexec.ExitCode(err))
	}

	_, _ = fmt.Fprintf(p.stderr, "Uploading %s\n", in.name)
	if err := p.upload(ctx, in); err != nil {
		p.cleanup(ctx)
		p.report(fmt.Sprintf("The upload stopped before the import; the database of %s was not changed.", p.id))
		return stepError(err, "the upload failed")
	}
	if err := p.run(ctx, "gzip -t "+importFile, nil); err != nil {
		p.cleanup(ctx)
		p.report(fmt.Sprintf("Nothing was imported; the database of %s was not changed.", p.id))
		if ctx.Err() != nil {
			return err
		}
		return stepError(err, "the upload arrived damaged")
	}

	_, _ = fmt.Fprintf(p.stderr, "Importing into %s\n", p.id)
	if err := p.run(ctx, `set -o pipefail; gunzip -c `+importFile+` | mysql "$FORTRABBIT_DB_NAME"`, nil); err != nil {
		p.cleanup(ctx)
		if ctx.Err() != nil {
			p.report(fmt.Sprintf("Cancelled during the import: the database of %s may be partly imported, and mysql may still be running on the environment.", p.id))
			return err
		}
		p.report(fmt.Sprintf("The import into %s failed part way: the database may be partly overwritten.", p.id))
		return stepError(err, "the import failed")
	}
	p.cleanup(ctx)
	_, err := fmt.Fprintf(p.stderr, "Imported %s into %s. The backup stays at %s on the environment; remove it once you no longer need it.\n", in.name, p.id, p.backup)
	return err
}

// upload streams the dump into importFile, gzipping plain SQL on the way.
func (p pusher) upload(ctx context.Context, in *input) error {
	remote := "rm -f " + staleImports + "; cat > " + importFile
	if in.gzipped {
		return p.run(ctx, remote, in.reader)
	}
	reader, writer := io.Pipe()
	go func() {
		packer := gzip.NewWriter(writer)
		_, err := io.Copy(packer, in.reader)
		if closeErr := packer.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
	}()
	err := p.run(ctx, remote, reader)
	_ = reader.CloseWithError(errors.New("upload ended"))
	return err
}
