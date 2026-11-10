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
	for _, env := range []api.Resource{{"publicId": "en-abc123"}, {"publicId": "en-abc123", "sshHost": ""}, {"sshHost": "ssh.eu-w1a.frbit.app"}} {
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
	if !slices.Contains(batch, "BatchMode=yes") || batch[len(batch)-2] != "en-abc123@ssh.x" || batch[len(batch)-1] != "uptime" || !slices.Contains(batch, "-T") {
		t.Fatalf("Args = %q", batch)
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
	if err == nil || !strings.Contains(err.Error(), KeyURL) || ExitCode(err) != 255 {
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
