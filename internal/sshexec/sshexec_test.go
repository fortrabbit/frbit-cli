package sshexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

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
		got, err := Target(c.env)
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
		if _, err := Target(env); err == nil {
			t.Fatalf("Target(%v) accepted", env)
		}
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
	batch := Args("en-abc123@ssh.x", "uptime", false)
	if !slices.Contains(batch, "BatchMode=yes") || batch[len(batch)-3] != "--" || batch[len(batch)-2] != "en-abc123@ssh.x" || batch[len(batch)-1] != "uptime" || !slices.Contains(batch, "-T") {
		t.Fatalf("Args = %q", batch)
	}
	if !slices.Equal(Options(false), batch[:len(batch)-3]) {
		t.Fatalf("Options = %q", Options(false))
	}
	if slices.Contains(Args("en-abc123@ssh.x", "uptime", true), "BatchMode=yes") {
		t.Fatal("BatchMode on a terminal")
	}
	for _, arg := range batch {
		if strings.Contains(arg, "StrictHostKeyChecking") {
			t.Fatal("host key checking disabled")
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

func TestMapExit(t *testing.T) {
	if MapExit("t", nil) != nil {
		t.Fatal("nil")
	}
	if err := MapExit("en-abc123@ssh.x", exitError(255)); !strings.Contains(err.Error(), KeyURL) {
		t.Fatalf("255: %v", err)
	}
	if err := MapExit("en-abc123@ssh.x", exitError(12)); ExitCode(err) != 12 || strings.Contains(err.Error(), KeyURL) {
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

func TestRunUnknownHostKeyNamesTheFix(t *testing.T) {
	var shown bytes.Buffer
	unknown := "No ED25519 host key is known for ssh.eu-w1a.frbit.app and you have requested strict checking.\r\nHost key verification failed.\r\n"
	err := Run(context.Background(), stderrRunner{unknown, exitError(255)}, "en-abc123@ssh.eu-w1a.frbit.app", "true", false, nil, &bytes.Buffer{}, &shown)
	for _, want := range []string{"ssh en-abc123@ssh.eu-w1a.frbit.app exit", "ssh-keyscan ssh.eu-w1a.frbit.app >> ~/.ssh/known_hosts"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q", err, want)
		}
	}
	if ExitCode(err) != 255 || shown.String() != unknown {
		t.Fatalf("exit %d, stderr %q", ExitCode(err), shown.String())
	}

	changed := "@@@@@@@@\r\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\r\nHost key verification failed.\r\n"
	err = Run(context.Background(), stderrRunner{changed, exitError(255)}, "en-abc123@ssh.eu-w1a.frbit.app", "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || strings.Contains(err.Error(), "ssh-keyscan") || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed: %v", err)
	}

	err = Run(context.Background(), stderrRunner{"Permission denied (publickey).\r\n", exitError(255)}, "en-abc123@ssh.eu-w1a.frbit.app", "true", false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), KeyURL) || strings.Contains(err.Error(), "ssh-keyscan") {
		t.Fatalf("rejected key: %v", err)
	}
}
