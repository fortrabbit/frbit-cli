package files

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

const remoteRoot = "/data/www/"

// runner, checkTool and git are replaced in tests.
var (
	runner    sshexec.Runner = sshexec.System{}
	checkTool                = sshexec.Check
	git       link.GitRunner = link.SystemGit
)

// segment allows only characters no remote shell interprets, since older rsync passes the remote path through it unquoted.
var segment = regexp.MustCompile(`^[A-Za-z0-9._@+-]+$`)

var rsyncVersion = regexp.MustCompile(`version v?(\d+)\.(\d+)`)

func NewCmdFiles(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "files", Short: "Sync files between an environment and the current folder", Args: cobra.NoArgs}
	command.AddCommand(newCmdSync(factory, false), newCmdSync(factory, true))
	return command
}

type options struct {
	env, confirmation string
	dryRun, delete    bool
}

func newCmdSync(factory *app.Factory, push bool) *cobra.Command {
	var opts options
	command := &cobra.Command{
		Use:   "pull [path]",
		Short: "Copy files from an environment to the current folder",
		Long: `Copy files from /data/www/ on an environment to the current folder with rsync over SSH, using your own SSH key.

The path is relative to /data/www/ and to the current folder; without one, all of /data/www/ is synced into the current folder. A folder path syncs that folder onto the same-named local folder, so web/uploads syncs /data/www/web/uploads into ./web/uploads. Absolute paths, .. and characters outside letters, digits and . _ - + @ / are refused.

Files that changed are overwritten locally. --delete also removes local files that do not exist on the environment. Without a path, and with --delete, it asks you to type the environment ID on a terminal; without one, pass --confirm <environment>. --dry-run lists what would change and needs no confirmation.

Without a path, .git/, .fortrabbit/, .env, .env.*, the default outputs of db pull and backups download (en-*.sql.gz, ba-*-database.*, ba-*-files.*) and .frbit-* temp files in the current folder and in /data/www/ are never copied or deleted. These excludes are anchored at the top of the transfer, so they apply to a whole-folder sync, not inside a synced subfolder.

A sync without a path, and any --delete, refuses to run from /, your home folder, or a subfolder of a git checkout: run it from the top of the project.

The environment is an ID or a label from .fortrabbit/environments.yml passed with --env; without one, the environment of the checkout is used (see frbit link).`,
		Example: `  frbit files pull web/uploads
  frbit files pull web/uploads --env en-a1b2c3
  frbit files pull storage --dry-run --delete`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, factory, push, args, opts)
		},
	}
	if push {
		command.Use = "push [path]"
		command.Short = "Copy files from the current folder to an environment"
		command.Long = `Copy files from the current folder to /data/www/ on an environment with rsync over SSH, using your own SSH key.

The path is relative to the current folder and to /data/www/; without one, the whole current folder is synced onto /data/www/. A folder path syncs that folder onto the same-named remote folder, whose parent must exist on the environment. Absolute paths, .. and characters outside letters, digits and . _ - + @ / are refused.

Files that changed are overwritten on the environment. --delete also removes files on the environment that do not exist locally; --dry-run lists what would change. When the environment deploys from a Git repository, the next deployment overwrites code you push this way.

It asks you to type the environment ID on a terminal; without one, pass --confirm <environment>. A dry run needs no confirmation.

Without a path, .git/, .fortrabbit/, .env, .env.*, the default outputs of db pull and backups download (en-*.sql.gz, ba-*-database.*, ba-*-files.*) and .frbit-* temp files in the current folder and in /data/www/ are never copied or deleted. These excludes are anchored at the top of the transfer, so they apply to a whole-folder sync, not inside a synced subfolder.

A sync without a path, and any --delete, refuses to run from /, your home folder, or a subfolder of a git checkout: run it from the top of the project.

The environment is an ID or a label from .fortrabbit/environments.yml passed with --env; without one, the environment of the checkout is used (see frbit link).`
		command.Example = `  frbit files push web/uploads --env en-a1b2c3
  frbit files push web/uploads --dry-run
  frbit files push web/uploads --env en-a1b2c3 --confirm en-a1b2c3`
	}
	command.Flags().StringVar(&opts.confirmation, "confirm", "", "Confirm by repeating the environment ID")
	command.Flags().StringVar(&opts.env, "env", "", "Environment ID or label (default: the environment of the checkout)")
	command.Flags().BoolVar(&opts.dryRun, "dry-run", false, "List what would change without copying")
	command.Flags().BoolVar(&opts.delete, "delete", false, "Remove files on the receiving side that the sending side does not have")
	return command
}

// cleanPath returns path relative to /data/www/ and the current folder, "." for all of it.
func cleanPath(raw string) (string, error) {
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) || strings.HasPrefix(raw, "~") || (len(raw) > 1 && raw[1] == ':') {
		return "", fmt.Errorf("%s is not a relative path; give a path relative to %s and the current folder", raw, remoteRoot)
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", fmt.Errorf("%s leaves %s; .. is not allowed", raw, remoteRoot)
		}
		if !segment.MatchString(part) {
			return "", fmt.Errorf("%s contains characters other than letters, digits and . _ - + @ /", raw)
		}
	}
	return path.Clean("./" + raw), nil
}

// progressFlag picks --info=progress2 for rsync 3.1 and later; older rsync and openrsync only know --progress.
func progressFlag(version string) string {
	match := rsyncVersion.FindStringSubmatch(version)
	if strings.Contains(version, "openrsync") || match == nil {
		return "--progress"
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major > 3 || (major == 3 && minor >= 1) {
		return "--info=progress2"
	}
	return "--progress"
}

// excludes keep the repository, its link file, local secrets, and the default outputs and temp files of db pull and backups download out of a sync and a --delete.
var excludes = []string{"--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=/.frbit-*", "--exclude=/en-*.sql.gz", "--exclude=/ba-*-database.*", "--exclude=/ba-*-files.*"}

// rsyncArgs builds the rsync arguments; "--" ends the options before the paths.
func rsyncArgs(progress string, interactive, dryRun, del bool, src, dst string) []string {
	args := append([]string{"-az", progress, "-e", strings.Join(append([]string{"ssh"}, sshexec.Options(interactive)...), " ")}, excludes...)
	if dryRun {
		args = append(args, "--dry-run", "-v")
	}
	if del {
		args = append(args, "--delete")
	}
	return append(args, "--", src, dst)
}

// endpoints returns the source and destination: the path without a trailing slash into its parent, so a folder lands on the same-named folder and a file on the same-named file.
func endpoints(target, rel string, push bool) (src, dst string) {
	remote, local := target+":"+remoteRoot, "./"
	if rel == "." {
		if push {
			return local, remote
		}
		return remote, local
	}
	parent := path.Dir(rel)
	remoteParent, localParent := remote, local
	if parent != "." {
		remoteParent += parent + "/"
		localParent += parent + "/"
	}
	if push {
		return local + rel, remoteParent
	}
	return remote + rel, localParent
}

func run(cmd *cobra.Command, factory *app.Factory, push bool, args []string, opts options) error {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()
	raw := ""
	if len(args) == 1 {
		raw = args[0]
	}
	rel, err := cleanPath(raw)
	if err != nil {
		return err
	}
	if push {
		if _, err := os.Lstat(rel); err != nil {
			return fmt.Errorf("nothing to push: %w", err)
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if rel == "." || opts.delete {
		if err := checkRoot(ctx, dir); err != nil {
			return err
		}
	}
	for _, tool := range []string{"rsync", "ssh"} {
		if err := checkTool(tool); err != nil {
			return err
		}
	}
	client, err := cmdutil.APIClient(cmd, factory)
	if err != nil {
		return err
	}
	id, err := link.ForStreams(factory.IOStreams).Resolve(ctx, client, dir, opts.env)
	if err != nil {
		return err
	}
	destructive := !opts.dryRun && (push || opts.delete || rel == ".")
	action := "files pull"
	if opts.delete {
		action += " --delete"
	}
	if push {
		action = "files push"
	}
	if destructive {
		if err := cmdutil.CheckDestructive(factory, action, id, opts.confirmation); err != nil {
			return err
		}
	}
	env, err := link.Environment(ctx, client, id)
	if err != nil {
		return err
	}
	target, err := sshexec.Target(env)
	if err != nil {
		return err
	}
	remotePath := remoteRoot
	if rel != "." {
		remotePath += rel
	}
	if destructive {
		local := filepath.Join(dir, rel)
		warning := fmt.Sprintf("Local files under %s that are not on %s are deleted.", local, id)
		if rel == "." {
			warning = fmt.Sprintf("Local files under %s are overwritten by the copies on %s.", local, id)
			if opts.delete {
				warning += fmt.Sprintf(" Local files there that are not on %s are deleted.", id)
			}
		}
		if push {
			warning = fmt.Sprintf("Files under %s on %s are overwritten by the local copies from %s.", remotePath, id, local)
			if opts.delete {
				warning += " Files there that do not exist locally are deleted."
			}
			if branch := gitBranch(env); branch != "" {
				warning += fmt.Sprintf(" %s deploys from the Git branch %s: the next deployment overwrites code changes made this way.", id, branch)
			}
		}
		confirmed, err := cmdutil.ConfirmDestructiveContext(ctx, factory, stderr, action, id, opts.confirmation, warning)
		if err != nil {
			return err
		}
		if !confirmed {
			_, err = fmt.Fprintln(stderr, "Aborted.")
			return err
		}
	}
	if !push && !opts.dryRun && path.Dir(rel) != "." {
		if err := os.MkdirAll(path.Dir(rel), 0o755); err != nil {
			return err
		}
	}

	var version bytes.Buffer
	_ = runner.Run(ctx, "rsync", []string{"--version"}, nil, &version, io.Discard)
	interactive := factory.IOStreams.IsTTY
	src, dst := endpoints(target, rel, push)
	var stdin io.Reader
	if interactive {
		stdin = factory.IOStreams.In
	}
	tail := sshexec.NewTail(stderr)
	err = runner.Run(ctx, "rsync", rsyncArgs(progressFlag(version.String()), interactive, opts.dryRun, opts.delete, src, dst), stdin, cmd.OutOrStdout(), tail)
	if err != nil && ctx.Err() != nil {
		_, _ = fmt.Fprintf(stderr, "Cancelled: the sync of %s stopped partway; some files may be copied and others not.\n", remotePath)
		return ctx.Err()
	}
	if mapped := sshexec.MapOutput(target, err, tail.String()); mapped != err {
		return mapped
	}
	if code := sshexec.ExitCode(err); err != nil && code > 0 {
		return fmt.Errorf("rsync failed with exit code %d; see the message above", code)
	} else if err != nil {
		return fmt.Errorf("rsync: %w", err)
	}
	return nil
}

// gitBranch returns the branch the environment deploys from, "" when no repository is connected.
func gitBranch(env map[string]any) string {
	git, _ := env["git"].(map[string]any)
	branch, _ := git["branch"].(string)
	return branch
}

// checkRoot refuses a whole-folder sync or a --delete from a folder that cannot be the project: / , the home folder, or a subfolder of a git checkout.
func checkRoot(ctx context.Context, dir string) error {
	real := resolved(dir)
	if real == filepath.Dir(real) {
		return fmt.Errorf("refusing to sync the root folder %s; run frbit files from the project folder", dir)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && real == resolved(home) {
		return fmt.Errorf("refusing to sync your home folder %s; run frbit files from the project folder", dir)
	}
	if top, ok := (link.Resolver{Git: git}).TopLevel(ctx, dir); ok && resolved(top) != real {
		return fmt.Errorf("%s is a subfolder of the git checkout; run from the project root %s", dir, top)
	}
	return nil
}

// resolved returns path with symlinks resolved, or the cleaned path when that fails.
func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}
