package sshexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

func TestTarget(t *testing.T) {
	cases := []struct {
		env  api.Resource
		want string
	}{
		{api.Resource{"publicId": "en-abc123", "sshHost": "en-abc123@ssh.us-e1a.frbit.app"}, "en-abc123@ssh.us-e1a.frbit.app"},
		{api.Resource{"publicId": "en-abc123", "sshHost": "ssh.eu-w1a.frbit.app"}, "en-abc123@ssh.eu-w1a.frbit.app"},
	}
	for _, c := range cases {
		got, err := Target(c.env, true)
		if err != nil || got != c.want {
			t.Fatalf("Target(%v) = %q, %v; want %q", c.env, got, err, c.want)
		}
	}
	for _, env := range []api.Resource{
		{"publicId": "en-abc123"},
		{"publicId": "en-abc123", "sshHost": ""},
		{"sshHost": "ssh.eu-w1a.frbit.app"},
		{"publicId": "en-abc123", "sshHost": "-oProxyCommand=x@y"},
		{"publicId": "en-abc123", "sshHost": "en-x@-oFoo"},
		{"publicId": "-oFoo", "sshHost": "ssh.eu-w1a.frbit.app"},
		{"publicId": "en-abc123", "sshHost": "-oProxyCommand=x"},
		{"publicId": "en-abc123", "sshHost": "en-abc123@ssh.x;id"},
	} {
		if _, err := Target(env, false); err == nil {
			t.Fatalf("Target(%v) accepted", env)
		}
	}
	other := api.Resource{"publicId": "en-abc123", "sshHost": "ssh.eu-w1.frbit.app.attacker.example"}
	if _, err := Target(other, true); err == nil || !strings.Contains(err.Error(), "not a fortrabbit endpoint") {
		t.Fatalf("unpinned host with the default API: %v", err)
	}
	if got, err := Target(other, false); err != nil || got != "en-abc123@ssh.eu-w1.frbit.app.attacker.example" {
		t.Fatalf("unpinned host with another API: %q, %v", got, err)
	}
}

func TestCheckNamesTheTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := Check("ssh")
	if err == nil || !strings.Contains(err.Error(), "ssh") || !strings.Contains(err.Error(), "OpenSSH") {
		t.Fatalf("Check: %v", err)
	}
}

func TestArgsBatchModeWithoutTerminal(t *testing.T) {
	batch, _ := Args("en-abc123@ssh.x", "uptime", false)
	if !slices.Contains(batch, "BatchMode=yes") || batch[len(batch)-3] != "--" || batch[len(batch)-2] != "en-abc123@ssh.x" || batch[len(batch)-1] != "uptime" || !slices.Contains(batch, "-T") {
		t.Fatalf("Args = %q", batch)
	}
	if options, _ := Options("en-abc123@ssh.x", false); !slices.Equal(options, batch[:len(batch)-3]) {
		t.Fatalf("Options = %q", options)
	}
	if tty, _ := Args("en-abc123@ssh.x", "uptime", true); slices.Contains(tty, "BatchMode=yes") {
		t.Fatal("BatchMode on a terminal")
	}
	if !slices.Contains(batch, "-a") || !slices.Contains(batch, "-x") {
		t.Fatalf("agent or X11 forwarding not disabled: %q", batch)
	}
	for _, arg := range batch {
		if strings.Contains(arg, "StrictHostKeyChecking=no") || strings.Contains(arg, "accept-new") {
			t.Fatal("host key checking relaxed")
		}
	}
}

type exitError int

func (e exitError) Error() string { return "exit status" }
func (e exitError) ExitCode() int { return int(e) }

type fakeRunner struct{ err error }

func (f fakeRunner) Run(_ context.Context, _ string, _ []string, _ io.Reader, _, _ io.Writer) error {
	return f.err
}

func TestRunExit255PointsToDashboard(t *testing.T) {
	useKnownHosts(t)
	err := Run(context.Background(), fakeRunner{exitError(255)}, "en-abc123@ssh.x", "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), KeyURL) || !strings.Contains(err.Error(), "host key") || ExitCode(err) != 255 {
		t.Fatalf("Run: %v", err)
	}
	err = Run(context.Background(), fakeRunner{exitError(3)}, "en-abc123@ssh.x", "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if ExitCode(err) != 3 || strings.Contains(err.Error(), KeyURL) {
		t.Fatalf("Run: %v", err)
	}
	if ExitCode(errors.New("x")) != -1 || ExitCode(nil) != 0 {
		t.Fatal("ExitCode")
	}
}

func TestMapOutputExitCodes(t *testing.T) {
	if MapOutput("t", nil, "") != nil {
		t.Fatal("nil")
	}
	if err := MapOutput("en-abc123@ssh.x", exitError(255), ""); !strings.Contains(err.Error(), KeyURL) {
		t.Fatalf("255: %v", err)
	}
	if err := MapOutput("en-abc123@ssh.x", exitError(12), ""); ExitCode(err) != 12 || strings.Contains(err.Error(), KeyURL) {
		t.Fatalf("12: %v", err)
	}
}

type stderrRunner struct {
	stderr string
	err    error
}

func (f stderrRunner) Run(_ context.Context, _ string, _ []string, _ io.Reader, _, stderr io.Writer) error {
	_, _ = io.WriteString(stderr, f.stderr)
	return f.err
}

func TestRunHostKeyErrors(t *testing.T) {
	useKnownHosts(t)
	const other = "en-abc123@ssh.dev.frbit.cloud"
	var shown bytes.Buffer
	unknown := "No ED25519 host key is known for ssh.dev.frbit.cloud and you have requested strict checking.\r\nHost key verification failed.\r\n"
	err := Run(context.Background(), stderrRunner{unknown, exitError(255)}, other, "true", false, nil, &bytes.Buffer{}, &shown)
	want := "ssh to en-abc123@ssh.dev.frbit.cloud failed: the host key of ssh.dev.frbit.cloud is not in your known_hosts yet; run `ssh en-abc123@ssh.dev.frbit.cloud exit` once in a terminal to check and accept it, then try again"
	if err == nil || err.Error() != want || ExitCode(err) != 255 || shown.String() != unknown {
		t.Fatalf("unknown: %v", err)
	}

	changed := "@@@@@@@@\r\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\r\nHost key verification failed.\r\n"
	err = Run(context.Background(), stderrRunner{changed, exitError(255)}, other, "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed: %v", err)
	}

	for _, stderr := range []string{changed, unknown} {
		err = Run(context.Background(), stderrRunner{stderr, exitError(255)}, "en-abc123@ssh.eu-w1a.frbit.app", "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
		want := "ssh to en-abc123@ssh.eu-w1a.frbit.app stopped: the server's host key does not match the fortrabbit host key frbit knows for ssh.eu-w1a.frbit.app. Do not continue and do not accept the key by hand: the connection may be intercepted. Update frbit, and contact fortrabbit support if this persists"
		if err == nil || err.Error() != want || ExitCode(err) != 255 {
			t.Fatalf("pinned mismatch: %v", err)
		}
	}

	err = Run(context.Background(), stderrRunner{"Permission denied (publickey).\r\n", exitError(255)}, "en-abc123@ssh.eu-w1a.frbit.app", "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), KeyURL) {
		t.Fatalf("rejected key: %v", err)
	}
	for _, text := range []string{err.Error()} {
		if strings.Contains(text, "keyscan") {
			t.Fatalf("keyscan advice: %v", text)
		}
	}
}

func TestSystemRunInterruptsBeforeKilling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGINT on Windows")
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := &lockedBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- System{}.Run(ctx, "sh", []string{"-c", `trap 'echo interrupted; exit 3' INT; echo ready; while :; do sleep 0.05; done`}, nil, out, io.Discard)
	}()
	for i := 0; i < 100 && !strings.Contains(out.String(), "ready"); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(out.String(), "interrupted") {
		t.Fatalf("output %q", out.String())
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
