package sshexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

const rejectedStderr = "en-abc123@ssh.eu-w1a.frbit.app: Permission denied (publickey).\r\n"

type fakeAccount struct {
	you api.You
	err error
}

func (f fakeAccount) You(context.Context) (api.YouResponse, error) {
	return api.YouResponse{You: f.you}, f.err
}

func withSSHKeyStep(done bool, url string) api.You {
	return api.You{Optional: []api.OptionalStep{{Step: SSHKeyStep, Done: done, Instruction: "Add an SSH key", URL: &url}}}
}

func useLocalKey(t *testing.T, state KeyState) {
	t.Helper()
	previous := LocalKey
	LocalKey = func(context.Context, string) KeyState { return state }
	t.Cleanup(func() { LocalKey = previous })
}

func TestMapOutputSplitsKeyAndConnectionFailures(t *testing.T) {
	const target = "en-abc123@ssh.eu-w1a.frbit.app"
	var keyErr *KeyError
	if err := MapOutput(target, exitError(255), rejectedStderr); !errors.As(err, &keyErr) || ExitCode(err) != 255 {
		t.Fatalf("rejected key: %v", err)
	}
	for _, stderr := range []string{
		"ssh: Could not resolve hostname ssh.eu-w1a.frbit.app: nodename nor servname provided\r\n",
		"ssh: connect to host ssh.eu-w1a.frbit.app port 22: Connection refused\r\n",
		"ssh: connect to host ssh.eu-w1a.frbit.app port 22: Operation timed out\r\n",
		"kex_exchange_identification: Connection closed by remote host\r\nConnection closed by 1.2.3.4 port 22\r\n",
	} {
		err := MapOutput(target, exitError(255), stderr)
		if err == nil || !strings.Contains(err.Error(), "the connection to ssh.eu-w1a.frbit.app failed") || strings.Contains(err.Error(), KeyURL) || ExitCode(err) != 255 {
			t.Fatalf("%q: %v", stderr, err)
		}
	}
	tooMany := "Received disconnect from 1.2.3.4 port 22:2: Too many authentication failures\r\nDisconnected from 1.2.3.4 port 22\r\n"
	if err := MapOutput(target, exitError(255), tooMany); !errors.As(err, &keyErr) || !keyErr.TooMany || !strings.Contains(err.Error(), "IdentitiesOnly yes") {
		t.Fatalf("too many keys: %v", err)
	}
	// A remote PHP fatal exits 255 too; its "Connection refused" is not ssh's.
	if err := MapOutput(target, exitError(255), "SQLSTATE[HY000] [2002] Connection refused\n"); errors.As(err, &keyErr) || strings.Contains(err.Error(), "connection to") || !strings.Contains(err.Error(), KeyURL) {
		t.Fatalf("remote failure: %v", err)
	}
	// Only ssh's own exit code makes the words ssh's.
	if err := MapOutput(target, exitError(2), rejectedStderr); err.Error() != "exit status" {
		t.Fatalf("exit 2: %v", err)
	}
	if err := MapOutput(target, exitError(255), "ssh: connect to host x port 22: Connection refused\x1b[2J\r\n"); strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("control characters kept: %q", err)
	}
}

func TestExplainNamesTheMissingKey(t *testing.T) {
	const target = "en-abc123@ssh.eu-w1a.frbit.app"
	cases := []struct {
		name    string
		account fakeAccount
		local   KeyState
		want    string
	}{
		{"neither", fakeAccount{you: withSSHKeyStep(false, "https://dash.example/ssh")}, KeyMissing,
			"ssh to " + target + " failed: your fortrabbit account has no SSH key, and ssh has no key to offer on this machine; create one with `ssh-keygen -t ed25519`, then add the public key (~/.ssh/id_ed25519.pub) at https://dash.example/ssh"},
		{"account", fakeAccount{you: withSSHKeyStep(false, "https://dash.example/ssh")}, KeyPresent,
			"ssh to " + target + " failed: your fortrabbit account has no SSH key; add the public key of this machine at https://dash.example/ssh"},
		{"machine", fakeAccount{you: withSSHKeyStep(true, "")}, KeyMissing,
			"ssh to " + target + " failed: ssh has no key to offer on this machine; create one with `ssh-keygen -t ed25519`, then add the public key (~/.ssh/id_ed25519.pub) at " + KeyURL},
		{"mismatch", fakeAccount{you: withSSHKeyStep(true, "")}, KeyPresent,
			"ssh to " + target + " failed: the server took none of the keys ssh offered: the key on this machine is not on your fortrabbit account, or it has a passphrase and is not in the SSH agent; load it with `ssh-add`, or add the public key of this machine at " + KeyURL},
		{"api down", fakeAccount{err: errors.New("dial tcp: timeout")}, KeyUnknown,
			"ssh to " + target + " failed: the server rejected the SSH key; add the public key of this machine at " + KeyURL},
		{"no step", fakeAccount{}, KeyPresent,
			"ssh to " + target + " failed: the server rejected the SSH key; add the public key of this machine at " + KeyURL},
		{"step url not https", fakeAccount{you: withSSHKeyStep(false, "javascript:x")}, KeyPresent,
			"ssh to " + target + " failed: your fortrabbit account has no SSH key; add the public key of this machine at " + KeyURL},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useLocalKey(t, c.local)
			err := Explain(context.Background(), MapOutput(target, exitError(255), rejectedStderr), c.account)
			if err == nil || err.Error() != c.want || ExitCode(err) != 255 {
				t.Fatalf("Explain:\n got %v\nwant %s", err, c.want)
			}
		})
	}
}

func TestExplainLeavesOtherErrorsAlone(t *testing.T) {
	called := false
	previous := LocalKey
	LocalKey = func(context.Context, string) KeyState { called = true; return KeyMissing }
	t.Cleanup(func() { LocalKey = previous })
	other := errors.New("other")
	if err := Explain(context.Background(), other, fakeAccount{}); err != other || called {
		t.Fatalf("Explain = %v, probed = %v", err, called)
	}
	if err := Explain(context.Background(), nil, fakeAccount{}); err != nil {
		t.Fatalf("Explain(nil) = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	keyErr := &KeyError{Target: "t"}
	if Explain(ctx, keyErr, fakeAccount{}); called || keyErr.Local != KeyUnknown {
		t.Fatal("probed after cancel")
	}
}

func TestParseConfigAndExpand(t *testing.T) {
	config := parseConfig([]byte("user x\nidentityfile ~/.ssh/id_rsa\nidentityfile /keys/work key\nidentityagent /Users/a/Library/Group Containers/agent.sock\npkcs11provider none\n"))
	if len(config.identityFiles) != 2 || config.identityFiles[1] != "/keys/work key" || config.agent != "/Users/a/Library/Group Containers/agent.sock" || config.pkcs11 {
		t.Fatalf("parseConfig = %+v", config)
	}
	if got, ok := expand("~/.ssh/id_rsa", "/home/a"); !ok || got != filepath.Join("/home/a", ".ssh/id_rsa") {
		t.Fatalf("expand ~ = %q, %v", got, ok)
	}
	for _, path := range []string{"~/.ssh/%h", "$HOME/.ssh/id", "~other/.ssh/id"} {
		if _, ok := expand(path, "/home/a"); ok {
			t.Fatalf("expand(%q) claimed to know the path", path)
		}
	}
}

// fakeTools puts ssh and ssh-add scripts first on PATH; ssh -G prints config, ssh-add exits with agentExit.
func fakeTools(t *testing.T, config string, agentExit string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	useKnownHosts(t)
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("ssh", "/bin/cat <<'EOF'\n"+config+"EOF\n")
	write("ssh-add", "exit "+agentExit+"\n")
	t.Setenv("PATH", dir)
}

func TestLocalKeyReadsSSHConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	const target = "en-abc123@ssh.eu-w1a.frbit.app"

	fakeTools(t, "identityfile ~/.ssh/id_ed25519\n", "0")
	if got := localKey(context.Background(), target); got != KeyMissing {
		t.Fatalf("no file, no agent = %v", got)
	}

	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent")
	if got := localKey(context.Background(), target); got != KeyPresent {
		t.Fatalf("agent with a key = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\n", "1")
	if got := localKey(context.Background(), target); got != KeyMissing {
		t.Fatalf("empty agent = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\nidentityagent none\n", "0")
	if got := localKey(context.Background(), target); got != KeyMissing {
		t.Fatalf("IdentityAgent none = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\n", "2")
	if got := localKey(context.Background(), target); got != KeyUnknown {
		t.Fatalf("unreachable agent = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\nidentitiesonly yes\n", "0")
	if got := localKey(context.Background(), target); got != KeyUnknown {
		t.Fatalf("IdentitiesOnly = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\n", "127")
	if got := localKey(context.Background(), target); got != KeyUnknown {
		t.Fatalf("broken ssh-add = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/%h\n", "1")
	if got := localKey(context.Background(), target); got != KeyUnknown {
		t.Fatalf("token in path = %v", got)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\npkcs11provider /usr/lib/opensc.so\n", "1")
	if got := localKey(context.Background(), target); got != KeyUnknown {
		t.Fatalf("PKCS#11 = %v", got)
	}

	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A certificate without its private key offers nothing.
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519-cert.pub"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\ncertificatefile ~/.ssh/id_ed25519-cert.pub\n", "1")
	if got := localKey(context.Background(), target); got != KeyMissing {
		t.Fatalf("certificate only = %v", got)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeTools(t, "identityfile ~/.ssh/id_ed25519\n", "1")
	if got := localKey(context.Background(), target); got != KeyPresent {
		t.Fatalf("key file = %v", got)
	}

	t.Setenv("PATH", t.TempDir())
	if got := localKey(context.Background(), target); got != KeyUnknown {
		t.Fatalf("no ssh = %v", got)
	}
}
