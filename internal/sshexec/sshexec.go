// Package sshexec runs commands on an environment through the local ssh client and the user's own SSH key.
package sshexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

// KeyURL is where a person adds the SSH key the environment accepts.
const KeyURL = "https://dash.fortrabbit.com/new/ssh-key"

// Runner starts a local program; tests replace it with a fake.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// System runs the program on this machine and kills it when ctx ends.
type System struct{}

func (System) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = 5 * time.Second
	return command.Run()
}

// destination is user@host with neither part starting with "-", so a host from the API can never become an ssh option.
var destination = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9-]*@[a-z0-9][a-z0-9.-]*$`)

// Target returns the SSH destination of an environment: sshHost as-is when it holds a user, else <publicId>@<sshHost>.
func Target(env api.Resource) (string, error) {
	host, _ := env["sshHost"].(string)
	host = strings.TrimSpace(host)
	id, _ := env["publicId"].(string)
	if host == "" {
		return "", fmt.Errorf("the API returned no SSH host for %s", id)
	}
	target := host
	if !strings.Contains(host, "@") {
		if id == "" {
			return "", errors.New("the API returned no environment ID for the SSH host")
		}
		target = id + "@" + host
	}
	if !destination.MatchString(target) {
		return "", fmt.Errorf("the API returned an invalid SSH destination %q", target)
	}
	return target, nil
}

var installHints = map[string]string{
	"ssh":   "install an OpenSSH client (macOS and most Linux distributions ship one; on Windows add the OpenSSH Client optional feature)",
	"rsync": "install rsync with your package manager (macOS ships one; on Windows use WSL)",
}

// Check reports a missing local tool by name, with how to get it.
func Check(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		hint, ok := installHints[name]
		if !ok {
			hint = "install " + name
		}
		return fmt.Errorf("%s not found in PATH; %s", name, hint)
	}
	return nil
}

// Options are the ssh options every caller uses; without a terminal BatchMode stops ssh from hanging on a password prompt.
func Options(interactive bool) []string {
	options := []string{"-T", "-o", "ConnectTimeout=10"}
	if !interactive {
		options = append(options, "-o", "BatchMode=yes")
	}
	return options
}

// Args builds the ssh arguments; "--" ends the options before the destination.
func Args(target, remote string, interactive bool) []string {
	return append(Options(interactive), "--", target, remote)
}

// Run runs remote on target over ssh and maps the exit as MapExit does.
func Run(ctx context.Context, runner Runner, target, remote string, interactive bool, stdin io.Reader, stdout, stderr io.Writer) error {
	err := runner.Run(ctx, "ssh", Args(target, remote, interactive), stdin, stdout, stderr)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return MapExit(target, err)
}

// MapExit turns ssh's own exit code 255 into an error pointing at the SSH key page; other errors pass through.
func MapExit(target string, err error) error {
	if ExitCode(err) == 255 {
		return &keyError{target: target, err: err}
	}
	return err
}

type keyError struct {
	target string
	err    error
}

func (e *keyError) Error() string {
	return fmt.Sprintf("ssh to %s failed: the SSH key was rejected, the connection failed, or the host key is unknown or changed; add your public key at %s, or check the network and ~/.ssh/known_hosts", e.target, KeyURL)
}

func (e *keyError) Unwrap() error { return e.err }

// ExitCode returns the exit code inside err: 0 for nil, -1 when err carries none.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return -1
}
