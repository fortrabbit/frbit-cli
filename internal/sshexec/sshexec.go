// Package sshexec runs commands on an environment through the local ssh client and the user's own SSH key.
package sshexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
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

// Target returns the SSH destination of an environment: sshHost as-is when it holds a user, else <publicId>@<sshHost>.
func Target(env api.Resource) (string, error) {
	host, _ := env["sshHost"].(string)
	host = strings.TrimSpace(host)
	id, _ := env["publicId"].(string)
	if host == "" {
		return "", fmt.Errorf("the API returned no SSH host for %s", id)
	}
	if strings.Contains(host, "@") {
		return host, nil
	}
	if id == "" {
		return "", errors.New("the API returned no environment ID for the SSH host")
	}
	return id + "@" + host, nil
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

// Args builds the ssh arguments; without a terminal BatchMode stops ssh from hanging on a password prompt.
func Args(target, remote string, interactive bool) []string {
	args := []string{"-T", "-o", "ConnectTimeout=10"}
	if !interactive {
		args = append(args, "-o", "BatchMode=yes")
	}
	return append(args, target, remote)
}

// Run runs remote on target over ssh; ssh's own exit code 255 becomes an error pointing at the SSH key page.
func Run(ctx context.Context, runner Runner, target, remote string, interactive bool, stdin io.Reader, stdout, stderr io.Writer) error {
	err := runner.Run(ctx, "ssh", Args(target, remote, interactive), stdin, stdout, stderr)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
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
	return fmt.Sprintf("ssh to %s failed: the SSH key was rejected or the connection failed; add your public key at %s, or check the network", e.target, KeyURL)
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
