package link

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	envlink "github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

// git is replaced in tests.
var git envlink.GitRunner = envlink.SystemGit

func NewCmdLink(factory *app.Factory) *cobra.Command {
	var label string
	var printJSON bool
	command := &cobra.Command{
		Use:   "link [environment]",
		Short: "Link the local project to an environment",
		Long: `Link the local project to an environment.

Without an argument it prints the environment that db and files use here, and which step found it: FORTRABBIT_ENVIRONMENT, the entry for the current branch in .fortrabbit/environments.yml, the environment deploying the current branch of the connected repository, or the only entry in the file. --json prints it as JSON with the SSH host and region.

With an environment, an ID (en-xxxxxx), a label from that file or app/environment by name, it stores the ID in .fortrabbit/environments.yml under --label, which defaults to the current branch. When nothing is found, a terminal lists the environments of the account to pick from. On a repository connected to an app, the environment deploying the current branch needs no entry and nothing is written.

The file lives at the top of the git checkout, or in the current folder outside git.`,
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
			resolver := envlink.Resolver{Git: git}
			dir = resolver.Root(ctx, dir)
			if len(args) == 0 && label == "" {
				resolution, err := resolver.Find(ctx, client, dir, "")
				if err == nil {
					if printJSON {
						return writeJSON(cmd, client, resolution)
					}
					return describe(out, resolver.Branch(ctx, dir), resolution)
				}
				if !errors.Is(err, envlink.ErrNoEnvironment) {
					return err
				}
				if printJSON {
					return errNoLink
				}
			} else if printJSON {
				return errors.New("--json only works without an environment or --label")
			}
			branch := resolver.Branch(ctx, dir)
			entries, err := envlink.ReadFile(dir)
			if err != nil {
				return err
			}
			match, connected, err := resolver.Lookup(ctx, client, dir, branch)
			if ambiguous := (envlink.AmbiguousError{}); err != nil && !errors.As(err, &ambiguous) {
				return err
			}
			confirm := func() error {
				_, err := fmt.Fprintf(out, "%s is connected: branch %s deploys to %s (%s). Nothing to write.\n", match.Repository, match.Branch, match.EnvironmentID, iostreams.StripControl(match.EnvironmentName))
				return err
			}
			var id, environmentName string
			switch {
			case len(args) == 1 && envlink.IsID(args[0]):
				id = args[0]
				if _, err := envlink.Verify(ctx, client, id); err != nil {
					return err
				}
			case len(args) == 1:
				resolution, err := resolver.Find(ctx, client, dir, args[0])
				if err != nil {
					return err
				}
				id = resolution.ID
				if resolution.Source == envlink.SourceName {
					environmentName = resolution.Label[strings.LastIndex(resolution.Label, "/")+1:]
				}
			case connected && label == "":
				return confirm()
			default:
				streams := factory.IOStreams
				target := envlink.Target{In: bufio.NewReader(streams.In), Stderr: cmd.ErrOrStderr(), Interactive: streams.IsTTY && streams.IsErrTTY}
				picked, err := target.Pick(ctx, client)
				if err != nil {
					return err
				}
				id, environmentName = picked.EnvironmentID, picked.EnvironmentName
			}
			if connected && match.EnvironmentID == id && label == "" {
				return confirm()
			}
			name := label
			// Without a branch, Find only reads a file with a single entry, so a second one needs a label the person picks.
			if name == "" && (branch != "" || entries.Len() == 0) {
				name = envlink.SaveLabel(branch, environmentName)
			}
			if name == "" {
				return errors.New("no git branch to name the entry; pass --label")
			}
			if err := envlink.ValidLabel(name); err != nil {
				return fmt.Errorf("%w; pass --label", err)
			}
			return envlink.Save(dir, name, id, out)
		},
	}
	command.Flags().StringVar(&label, "label", "", "Entry name in .fortrabbit/environments.yml (default: current branch)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the environment of the checkout as JSON")
	return command
}

var errNoLink = errors.New("no environment to link; pass the environment ID: frbit link <env-id>")

// describe says which environment the checkout resolves to and why.
func describe(out io.Writer, branch string, resolution envlink.Resolution) error {
	var err error
	switch resolution.Source {
	case envlink.SourceEnv:
		_, err = fmt.Fprintf(out, "%s is set to %s.\n", envlink.EnvVar, resolution.ID)
	case envlink.SourceRepository:
		match := resolution.Match
		_, err = fmt.Fprintf(out, "%s is connected: branch %s deploys to %s (%s). Nothing to write.\n", match.Repository, match.Branch, match.EnvironmentID, iostreams.StripControl(match.EnvironmentName))
	case envlink.SourceFile:
		if resolution.Label == branch {
			_, err = fmt.Fprintf(out, "Branch %s is linked to %s in %s.\n", branch, resolution.ID, envlink.FilePath)
		} else {
			_, err = fmt.Fprintf(out, "%s is the only entry in %s, so %s is used.\n", resolution.Label, envlink.FilePath, resolution.ID)
		}
	default:
		_, err = fmt.Fprintln(out, resolution.ID)
	}
	return err
}

type linkJSON struct {
	Environment string `json:"environment"`
	Source      string `json:"source"`
	Label       string `json:"label,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Branch      string `json:"branch,omitempty"`
	App         string `json:"app,omitempty"`
	Name        string `json:"name"`
	SSHHost     string `json:"sshHost"`
	Region      string `json:"region"`
}

// writeJSON prints the resolution with the name, SSH host and region of the environment.
func writeJSON(cmd *cobra.Command, client *api.Client, resolution envlink.Resolution) error {
	env, err := envlink.Environment(cmd.Context(), client, resolution.ID)
	if err != nil {
		return err
	}
	value := linkJSON{Environment: resolution.ID, Source: resolution.Source, Label: resolution.Label}
	if resolution.Source == envlink.SourceRepository {
		value.Repository, value.Branch, value.App = resolution.Match.Repository, resolution.Match.Branch, resolution.Match.AppID
	}
	value.Name, _ = env["name"].(string)
	if target, err := sshexec.Target(env, cmdutil.IsDefaultHost(client.Host())); err == nil {
		value.SSHHost = target
		value.Region = region(target)
	}
	return cmdutil.WriteJSONLine(cmd.OutOrStdout(), value)
}

// region reads the region from an SSH destination such as en-x@ssh.eu-w1a.frbit.app.
func region(target string) string {
	_, host, _ := strings.Cut(target, "@")
	rest, ok := strings.CutPrefix(host, "ssh.")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, ".")
	return name
}
