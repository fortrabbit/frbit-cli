package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

// Target finds the environment a command works on and says on stderr where a label or the checkout led.
type Target struct {
	Resolver Resolver
	// Dir is the folder the checkout is read from; empty means the current folder.
	Dir string
	// Stderr gets the line naming the resolved environment.
	Stderr io.Writer
}

// TargetFor returns a Target that asks and reports on the terminal of streams.
func TargetFor(streams iostreams.IOStreams) Target {
	return Target{Resolver: ForStreams(streams), Stderr: streams.ErrOut}
}

// Resolve returns the environment named by arg, else the one of the checkout (asking on a terminal); for commands that only read.
func (t Target) Resolve(ctx context.Context, client *api.Client, arg string) (string, error) {
	dir, err := t.dir()
	if err != nil {
		return "", err
	}
	resolution, err := t.Resolver.Find(ctx, client, dir, arg)
	if errors.Is(err, ErrNoEnvironment) && t.Resolver.IsTTY {
		id, err := t.Resolver.Pick(ctx, client)
		return id, err
	}
	if err != nil {
		return "", err
	}
	t.Announce(resolution)
	return resolution.ID, nil
}

// Optional is Resolve without asking: no environment in the checkout gives "" and no error.
func (t Target) Optional(ctx context.Context, client *api.Client, arg string) (string, error) {
	dir, err := t.dir()
	if err != nil {
		return "", err
	}
	resolution, err := t.Resolver.Find(ctx, client, dir, arg)
	if errors.Is(err, ErrNoEnvironment) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	t.Announce(resolution)
	return resolution.ID, nil
}

// Require returns the environment named by arg, an ID or a label; commands that change something never fall back to the checkout.
// usage is the command line to show, with <env-id|label> where the environment goes.
func (t Target) Require(ctx context.Context, client *api.Client, arg, usage string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg != "" {
		return t.Resolve(ctx, client, arg)
	}
	message := "name the environment: " + usage
	if dir, err := t.dir(); err == nil {
		quiet := t.Resolver
		quiet.IsTTY = false
		if resolution, err := quiet.Find(ctx, client, dir, ""); err == nil {
			message += fmt.Sprintf(" (the checkout resolves to %s via %s)", resolution.ID, via(resolution))
		}
	}
	return "", errors.New(message)
}

// Announce prints where the environment came from, unless it was given as an ID or picked by hand.
func (t Target) Announce(resolution Resolution) {
	if t.Stderr == nil {
		return
	}
	var from string
	switch resolution.Source {
	case SourceFile:
		from = resolution.Label + ", from " + FilePath
	case SourceEnv:
		from = "from " + EnvVar
	case SourceRepository:
		from = fmt.Sprintf("from the repository %s, branch %s", resolution.Match.Repository, resolution.Match.Branch)
	default:
		return
	}
	_, _ = fmt.Fprintf(t.Stderr, "→ %s (%s)\n", resolution.ID, from)
}

func via(resolution Resolution) string {
	switch resolution.Source {
	case SourceFile:
		return fmt.Sprintf("%s (%s)", FilePath, resolution.Label)
	case SourceEnv:
		return EnvVar
	case SourceRepository:
		return fmt.Sprintf("the repository %s, branch %s", resolution.Match.Repository, resolution.Match.Branch)
	}
	return resolution.Source
}

// IsEnvironmentArg reports whether value names an environment: an ID, or a label in the link file of the checkout.
func (t Target) IsEnvironmentArg(ctx context.Context, value string) bool {
	if IsID(value) {
		return true
	}
	dir, err := t.dir()
	if err != nil {
		return false
	}
	entries, err := ReadFile(t.Resolver.Root(ctx, dir))
	if err != nil {
		return false
	}
	_, ok := entries.Get(value)
	return ok
}

func (t Target) dir() (string, error) {
	if t.Dir != "" {
		return t.Dir, nil
	}
	return os.Getwd()
}

// commandTarget is a Target on the terminal of f that reports on the command's stderr.
func commandTarget(cmd *cobra.Command, f *app.Factory) Target {
	target := TargetFor(f.IOStreams)
	target.Resolver.Out = cmd.ErrOrStderr()
	target.Stderr = cmd.ErrOrStderr()
	return target
}

// ReadEnvironment resolves the optional environment argument of a read-only command.
func ReadEnvironment(cmd *cobra.Command, f *app.Factory, client *api.Client, arg string) (string, error) {
	return commandTarget(cmd, f).Resolve(cmd.Context(), client, arg)
}

// WriteEnvironment resolves the required environment argument of a command that changes something; usage defaults to the command path plus <env-id|label>.
func WriteEnvironment(cmd *cobra.Command, f *app.Factory, client *api.Client, arg, usage string) (string, error) {
	if usage == "" {
		usage = cmd.CommandPath() + " <env-id|label>"
	}
	return commandTarget(cmd, f).Require(cmd.Context(), client, arg, usage)
}

// FirstArg returns args[0], or "" without arguments.
func FirstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
