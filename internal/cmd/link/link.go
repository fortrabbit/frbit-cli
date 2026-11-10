package link

import (
	"errors"
	"fmt"
	"os"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	envlink "github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/spf13/cobra"
)

// git is replaced in tests.
var git envlink.GitRunner = envlink.SystemGit

func NewCmdLink(factory *app.Factory) *cobra.Command {
	var label string
	command := &cobra.Command{
		Use:   "link [environment]",
		Short: "Link the local project to an environment",
		Long: `Link the local project to an environment.

On a repository connected to an app, the environment deploying the current branch is found on the server and nothing is written. Otherwise the environment ID is stored in .fortrabbit/environments.yml under --label, which defaults to the current branch. The environment argument is an ID (en-xxxxxx) or a label from that file.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if label != "" {
				if err := envlink.ValidLabel(label); err != nil {
					return err
				}
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			resolver := envlink.Resolver{Git: git, In: factory.IOStreams.In, Out: cmd.ErrOrStderr(), IsTTY: factory.IOStreams.IsTTY}
			branch := resolver.Branch(ctx, dir)
			entries, err := resolver.Entries(ctx, dir, branch)
			if err != nil {
				return err
			}
			if len(args) == 0 && label == "" {
				if id, ok := entries.Get(branch); ok && branch != "" {
					_, err := fmt.Fprintf(out, "Branch %s is linked to %s in %s.\n", branch, id, envlink.FilePath)
					return err
				}
			}
			match, connected, err := resolver.Lookup(ctx, client, dir, branch)
			if err != nil {
				return err
			}
			confirm := func() error {
				_, err := fmt.Fprintf(out, "%s is connected: branch %s deploys to %s (%s). Nothing to write.\n", match.Repository, match.Branch, match.EnvironmentID, match.EnvironmentName)
				return err
			}
			var id string
			switch {
			case len(args) == 1:
				id = args[0]
				if !envlink.IsID(id) {
					found, ok := entries.Get(id)
					if !ok {
						return fmt.Errorf("no environment labelled %q in %s; pass the environment ID", id, envlink.FilePath)
					}
					id = found
				}
				if _, err := envlink.Verify(ctx, client, id); err != nil {
					return err
				}
			case connected && label == "":
				return confirm()
			case !factory.IOStreams.IsTTY:
				return errors.New("no environment to link; pass the environment ID: frbit link <environment>")
			default:
				if id, err = resolver.Pick(ctx, client); err != nil {
					return err
				}
			}
			if connected && match.EnvironmentID == id && label == "" {
				return confirm()
			}
			name := label
			if name == "" {
				name = branch
			}
			if name == "" {
				return errors.New("no git branch to name the entry; pass --label")
			}
			if err := envlink.ValidLabel(name); err != nil {
				return fmt.Errorf("%w; pass --label", err)
			}
			if err := entries.Set(name, id); err != nil {
				return err
			}
			if err := envlink.WriteFile(dir, entries); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Linked %s to %s in %s.\n", name, id, envlink.FilePath)
			return err
		},
	}
	command.Flags().StringVar(&label, "label", "", "Entry name in .fortrabbit/environments.yml (default: current branch)")
	return command
}
