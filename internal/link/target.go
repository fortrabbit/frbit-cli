package link

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

// Target finds the environment a command works on and says on stderr where a label or the checkout led.
type Target struct {
	Resolver Resolver
	// Dir is the folder the checkout is read from; empty means the current folder.
	Dir string
	// Stderr gets the line naming the resolved environment, and the picker.
	Stderr io.Writer
	// In answers the picker; it only runs when Interactive is set.
	In          *bufio.Reader
	Interactive bool
	// NoSave skips the offer to save a pick, for commands that may still abort at their confirmation.
	NoSave bool
}

// NewTarget reads the checkout in dir and picks on a terminal when the command runs on one.
func NewTarget(cmd *cobra.Command, dir string) Target {
	return Target{
		Dir:         dir,
		Stderr:      cmd.ErrOrStderr(),
		In:          bufio.NewReader(cmd.InOrStdin()),
		Interactive: iostreams.IsTerminal(cmd.InOrStdin()) && iostreams.IsTerminal(cmd.ErrOrStderr()),
	}
}

// Resolve returns the environment named by arg, else the one of the checkout.
func (t Target) Resolve(ctx context.Context, client *api.Client, arg string) (string, error) {
	dir := t.Dir
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	resolution, err := t.Resolver.Find(ctx, client, dir, arg)
	if errors.Is(err, ErrNoEnvironment) {
		return t.discover(ctx, client, dir)
	}
	if err != nil {
		return "", err
	}
	t.announce(resolution)
	return resolution.ID, nil
}

// announce prints where the environment came from, unless it was given as an ID.
func (t Target) announce(resolution Resolution) {
	if t.Stderr == nil {
		return
	}
	var from string
	switch resolution.Source {
	case SourceFile:
		from = resolution.Label + ", from " + FilePath
	case SourceEnv:
		from = "from " + EnvVar
	case SourceName:
		from = iostreams.SingleLine(resolution.Label)
	case SourceRepository:
		from = fmt.Sprintf("from the repository %s, branch %s", resolution.Match.Repository, resolution.Match.Branch)
	default:
		return
	}
	_, _ = fmt.Fprintf(t.Stderr, "→ %s (%s)\n", resolution.ID, from)
}

// ReadEnvironment resolves the optional environment argument of a read-only command.
func ReadEnvironment(cmd *cobra.Command, client *api.Client, arg string) (string, error) {
	return NewTarget(cmd, "").Resolve(cmd.Context(), client, arg)
}

// WriteEnvironment resolves the required environment argument of a command that changes something; usage defaults to the command path plus <env-id|label>.
func WriteEnvironment(cmd *cobra.Command, client *api.Client, arg, usage string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		if usage == "" {
			usage = cmd.CommandPath() + " <env-id|label>"
		}
		return "", errors.New("name the environment: " + usage)
	}
	return ReadEnvironment(cmd, client, arg)
}

// FirstArg returns args[0], or "" without arguments.
func FirstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// ReadHelp and WriteHelp state in the long help how a command takes its environment.
const (
	ReadHelp  = "The environment is an ID, a label from .fortrabbit/environments.yml, or app/environment by name; without one, the environment of the checkout is used (see frbit link)."
	WriteHelp = "The environment is required: an ID, a label from .fortrabbit/environments.yml, or app/environment by name."
)

// Documented adds ReadHelp, or WriteHelp when write is set, to the long help of command.
func Documented(command *cobra.Command, write bool) *cobra.Command {
	help := ReadHelp
	if write {
		help = WriteHelp
	}
	if strings.Contains(command.Long, help) {
		return command
	}
	long := command.Long
	if long == "" {
		long = command.Short + "."
	}
	command.Long = long + "\n\n" + help
	return command
}
