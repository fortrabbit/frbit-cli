package sshexec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

// KeyState is what frbit found out about an SSH key.
type KeyState int

const (
	KeyUnknown KeyState = iota
	KeyMissing
	KeyPresent
)

// SSHKeyStep is the optional /v1/you step that is done once the account has an SSH key.
const SSHKeyStep = "add_ssh_key"

// SSHKeyInstruction stands in when the API gives the add_ssh_key step no instruction.
const SSHKeyInstruction = "Add an SSH key"

// AddCommand adds the public key of this machine to the account.
const AddCommand = "frbit ssh-keys add"

// keygen is the command that creates a key the dashboard accepts.
const keygen = "ssh-keygen -t ed25519"

// KeyError is ssh's "Permission denied": the server took none of the keys ssh offered.
type KeyError struct {
	Target string
	// TooMany is ssh offering so many keys that the server disconnected before the right one.
	TooMany bool
	// Account is whether the fortrabbit account has an SSH key, Local whether ssh has one to offer on this machine.
	Account KeyState
	Local   KeyState
	// URL is where to add the key; empty means KeyURL.
	URL string
	Err error
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("ssh to %s failed: %s; %s", e.Target, e.Reason(), e.Fix())
}

func (e *KeyError) Unwrap() error { return e.Err }

// Reason says which key is missing, as far as Explain found out.
func (e *KeyError) Reason() string {
	switch {
	case e.TooMany:
		return "ssh offered too many keys and the server disconnected before the right one"
	case e.Account == KeyMissing && e.Local == KeyMissing:
		return "your fortrabbit account has no SSH key, and ssh has no key to offer on this machine"
	case e.Account == KeyMissing:
		return "your fortrabbit account has no SSH key"
	case e.Local == KeyMissing:
		return "ssh has no key to offer on this machine"
	case e.Account == KeyPresent && e.Local == KeyPresent:
		return "the server took none of the keys ssh offered: the key on this machine is not on your fortrabbit account, or it has a passphrase and is not in the SSH agent"
	}
	return "the server rejected the SSH key"
}

// Fix names the next step: create a key when this machine has none, then add its public key to the account.
func (e *KeyError) Fix() string {
	url := e.URL
	if url == "" {
		url = KeyURL
	}
	switch {
	case e.TooMany:
		return "set `IdentitiesOnly yes` and the `IdentityFile` of your fortrabbit key for this host in ~/.ssh/config"
	case e.Local == KeyMissing:
		return fmt.Sprintf("create one with `%s`, then add it with `%s` or at %s", keygen, AddCommand, url)
	case e.Account == KeyPresent && e.Local == KeyPresent:
		return fmt.Sprintf("load it with `ssh-add`, or add the key of this machine with `%s` or at %s", AddCommand, url)
	}
	return fmt.Sprintf("add the key of this machine with `%s` or at %s", AddCommand, url)
}

// connectionError is ssh losing the connection: DNS, a refused or timed-out connection, a network that dropped before or during the session.
type connectionError struct {
	target string
	line   string
	err    error
}

func (e *connectionError) Error() string {
	return fmt.Sprintf("ssh to %s failed: the connection to %s failed (%s); check the network, whether a firewall or VPN blocks outgoing SSH, and https://status.fortrabbit.com", e.target, host(e.target), e.line)
}

func (e *connectionError) Unwrap() error { return e.err }

// connectionPrefixes start ssh's own lines for a failed or dropped connection; a remote program printing "Connection refused" does not match.
var connectionPrefixes = []string{
	"ssh: ",
	"kex_exchange_identification: ",
	"Connection closed by ",
	"Connection reset by ",
	"Connection timed out during banner exchange",
}

// rejected reports ssh's "Permission denied (publickey…)": the server answered and took none of the offered keys.
func rejected(stderr string) bool {
	return strings.Contains(stderr, "Permission denied (publickey")
}

// tooManyKeys reports the server disconnecting after ssh offered more keys than it allows.
func tooManyKeys(stderr string) bool {
	return strings.Contains(stderr, "Too many authentication failures")
}

// connectionLine returns ssh's first connection failure line in stderr, cleaned for printing.
func connectionLine(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(iostreams.StripControl(line))
		for _, prefix := range connectionPrefixes {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimPrefix(line, "ssh: ")
			}
		}
	}
	return ""
}

// Account reads the setup steps of the signed-in account; *api.Client is one.
type Account interface {
	You(ctx context.Context) (api.YouResponse, error)
}

// explainTimeout bounds the extra API call and local probes after a failed connection.
const explainTimeout = 10 * time.Second

// Explain fills in a KeyError inside err with the account's SSH key step and the keys on this machine, so its message names what is missing. Other errors pass through.
func Explain(ctx context.Context, err error, account Account) error {
	var keyErr *KeyError
	if !errors.As(err, &keyErr) || ctx.Err() != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, explainTimeout)
	defer cancel()
	if account != nil {
		keyErr.Account, keyErr.URL = AccountKey(ctx, account)
	}
	keyErr.Local = LocalKey(ctx, keyErr.Target)
	return err
}

// AccountKey reads the add_ssh_key step from /v1/you: KeyMissing while it is open, with the URL of the step.
func AccountKey(ctx context.Context, account Account) (KeyState, string) {
	you, err := account.You(ctx)
	if err != nil {
		return KeyUnknown, ""
	}
	step := you.You.OptionalStep(SSHKeyStep)
	switch {
	case step == nil:
		return KeyUnknown, ""
	case step.Done:
		return KeyPresent, ""
	}
	return KeyMissing, step.Link()
}

// LocalKey reports whether ssh has a key to offer for target; tests replace it.
var LocalKey = localKey

// localKey asks ssh -G which identity files and agent apply to target: KeyPresent when one file exists or the agent holds a key, KeyMissing when neither, KeyUnknown when ssh's configuration cannot be read with certainty.
func localKey(ctx context.Context, target string) KeyState {
	options, err := Options(target, false)
	if err != nil {
		return KeyUnknown
	}
	// ssh -G evaluates Match exec of the user's own config again, as the failed ssh just did; it opens no connection.
	args := append([]string{"-G"}, options...)
	output, err := exec.CommandContext(ctx, "ssh", append(args, "--", target)...).Output()
	if err != nil {
		return KeyUnknown
	}
	config := parseConfig(output)
	home, _ := os.UserHomeDir()
	for _, file := range config.identityFiles {
		path, ok := expand(file, home)
		if !ok {
			return KeyUnknown
		}
		if _, err := os.Stat(path); err == nil {
			return KeyPresent
		}
	}
	// With IdentitiesOnly, agent keys count only when they match a listed file, which this check cannot see.
	if config.pkcs11 || config.identitiesOnly {
		return KeyUnknown
	}
	return agentKey(ctx, config.agent, home)
}

type sshConfig struct {
	identityFiles  []string
	agent          string
	pkcs11         bool
	identitiesOnly bool
}

// parseConfig reads the keywords of ssh -G output that decide which keys ssh offers.
func parseConfig(output []byte) sshConfig {
	var config sshConfig
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		keyword, value, _ := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		value = strings.TrimSpace(value)
		switch strings.ToLower(keyword) {
		case "identityfile":
			config.identityFiles = append(config.identityFiles, value)
		case "identitiesonly":
			config.identitiesOnly = strings.EqualFold(value, "yes")
		case "identityagent":
			config.agent = value
		case "pkcs11provider":
			config.pkcs11 = value != "" && !strings.EqualFold(value, "none")
		}
	}
	return config
}

// expand resolves ~ in an ssh path; ok is false for %-tokens and other variables ssh would expand on its own.
func expand(path, home string) (string, bool) {
	if strings.ContainsAny(path, "%$") {
		return "", false
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home == "" {
			return "", false
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), true
	}
	if strings.HasPrefix(path, "~") {
		return "", false
	}
	return path, true
}

// agentKey asks the agent ssh would use whether it holds a key.
func agentKey(ctx context.Context, agent, home string) KeyState {
	socket := os.Getenv("SSH_AUTH_SOCK")
	switch {
	case strings.EqualFold(agent, "none"):
		return KeyMissing
	case agent == "" || agent == "SSH_AUTH_SOCK" || agent == "$SSH_AUTH_SOCK":
	default:
		path, ok := expand(agent, home)
		if !ok {
			return KeyUnknown
		}
		socket = path
	}
	if socket == "" {
		// Windows OpenSSH reaches its agent through a named pipe without SSH_AUTH_SOCK.
		if runtime.GOOS == "windows" {
			return KeyUnknown
		}
		return KeyMissing
	}
	command := exec.CommandContext(ctx, "ssh-add", "-L")
	command.Env = append(os.Environ(), "SSH_AUTH_SOCK="+socket)
	err := command.Run()
	switch ExitCode(err) {
	case 0:
		return KeyPresent
	// 1 is an agent without keys; 2, an agent ssh-add cannot reach, may only be locked or stale, so it proves nothing.
	case 1:
		return KeyMissing
	}
	return KeyUnknown
}
