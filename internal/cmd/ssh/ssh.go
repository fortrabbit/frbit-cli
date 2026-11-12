// Package ssh opens a shell on an environment, or runs one command there.
package ssh

import (
	"fmt"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

var (
	runner    sshexec.Runner = sshexec.System{}
	checkTool                = sshexec.Check
)

func NewCmdSSH(factory *app.Factory) *cobra.Command {
	var printOnly bool
	command := &cobra.Command{
		Use:   "ssh [environment] [-- command...]",
		Short: "Open a shell on an environment, or run one command there",
		Long: `Open an interactive shell on the environment, or run the command after -- and exit with its exit code.

The connection uses your own SSH key and the same SSH settings as frbit db and frbit files: no agent or X11 forwarding, and the pinned host key of fortrabbit's SSH endpoints. With a terminal, ssh gets one too, so prompts and tools like tinker work; without one, as in scripts, it runs in batch mode.

--print prints the ssh command instead of running it, for scripts and other tools.`,
		Example: `  frbit ssh
  frbit ssh en-a1b2c3
  frbit ssh -- php artisan migrate --force
  frbit ssh staging -- php craft project-config/apply
  frbit ssh --print`,
		Args: func(cmd *cobra.Command, args []string) error {
			before := args
			if dash := cmd.ArgsLenAtDash(); dash >= 0 {
				before = args[:dash]
			}
			if len(before) > 1 {
				return fmt.Errorf("put the command after --: %s [environment] -- <command>", cmd.CommandPath())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			envArg, remote := split(cmd, args)
			if !printOnly {
				if err := checkTool("ssh"); err != nil {
					return err
				}
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			id, err := link.ReadEnvironment(cmd, client, envArg)
			if err != nil {
				return err
			}
			env, err := link.Environment(cmd.Context(), client, id)
			if err != nil {
				return err
			}
			target, err := sshexec.Target(env, cmdutil.IsDefaultHost(client.Host()))
			if err != nil {
				return err
			}
			terminal := factory.IOStreams.IsTTY
			sshArgs, err := arguments(target, remote, terminal)
			if err != nil {
				return err
			}
			if printOnly {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), quote(append([]string{"ssh"}, sshArgs...)))
				return err
			}
			tail := sshexec.NewTail(cmd.ErrOrStderr())
			err = runner.Run(cmd.Context(), "ssh", sshArgs, factory.IOStreams.In, cmd.OutOrStdout(), tail)
			if err == nil || cmd.Context().Err() != nil {
				return err
			}
			code := sshexec.ExitCode(err)
			if code == 255 || code < 0 {
				return sshexec.MapOutput(target, err, tail.String())
			}
			return &cmdutil.ExitError{Code: code}
		},
	}
	command.Flags().BoolVar(&printOnly, "print", false, "Print the ssh command instead of running it")
	return link.Documented(command, false)
}

// split returns the environment argument and the remote command after --.
func split(cmd *cobra.Command, args []string) (string, []string) {
	dash := cmd.ArgsLenAtDash()
	if dash < 0 {
		return link.FirstArg(args), nil
	}
	return link.FirstArg(args[:dash]), args[dash:]
}

// arguments builds the ssh arguments: a terminal on both sides when there is one here, batch mode without.
func arguments(target string, remote []string, terminal bool) ([]string, error) {
	options, err := sshexec.Options(target, terminal)
	if err != nil {
		return nil, err
	}
	if terminal {
		for index, option := range options {
			if option == "-T" {
				options[index] = "-t"
			}
		}
	}
	args := append(options, "--", target)
	if len(remote) > 0 {
		args = append(args, strings.Join(remote, " "))
	}
	return args, nil
}

// quote renders args as one POSIX shell line.
func quote(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		if arg != "" && strings.Trim(arg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./_-") == "" {
			quoted[index] = arg
			continue
		}
		quoted[index] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
