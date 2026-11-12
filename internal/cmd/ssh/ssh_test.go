package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/fortrabbit/frbit-cli/internal/testutil"
	"github.com/spf13/cobra"
)

// TestMain keeps the pinned known_hosts file in a temp folder instead of the user's config dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "frbit-ssh-test-")
	if err != nil {
		panic(err)
	}
	sshexec.KnownHostsPath = func() (string, error) { return filepath.Join(dir, "known_hosts"), nil }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type exitCode int

func (e exitCode) Error() string { return "exit" }
func (e exitCode) ExitCode() int { return int(e) }

type fakeRunner struct {
	args []string
	err  error
}

func (f *fakeRunner) Run(_ context.Context, _ string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.args = args
	return f.err
}

func execute(t *testing.T, fake *fakeRunner, tty bool, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	t.Setenv("FORTRABBIT_ENVIRONMENT", "")
	t.Chdir(t.TempDir())
	previousRunner, previousCheck := runner, checkTool
	runner, checkTool = fake, func(string) error { return nil }
	t.Cleanup(func() { runner, checkTool = previousRunner, previousCheck })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/environments/en-abc123" {
			_, _ = w.Write([]byte(`{"publicId":"en-abc123","sshHost":"en-abc123@ssh.us-e1a.frbit.app"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	stdout := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: &bytes.Buffer{}, IsTTY: tty},
		ConfigStore:     testutil.ConfigStore{},
		CredentialStore: testutil.CredentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(stdout)
	root.AddCommand(NewCmdSSH(factory))
	root.SetArgs(append([]string{"--host", server.URL, "ssh"}, args...))
	err := root.Execute()
	return stdout.String(), err
}

func TestAShellGetsATerminalWhenThereIsOne(t *testing.T) {
	fake := &fakeRunner{}
	if _, err := execute(t, fake, true, "en-abc123"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(fake.args, " ")
	if fake.args[0] != "-t" || strings.Contains(joined, "BatchMode") || !strings.HasSuffix(joined, "-- en-abc123@ssh.us-e1a.frbit.app") {
		t.Fatalf("args = %q", joined)
	}
}

func TestACommandRunsInBatchModeAndPassesItsExitCode(t *testing.T) {
	fake := &fakeRunner{err: exitCode(3)}
	_, err := execute(t, fake, false, "en-abc123", "--", "php", "artisan", "migrate", "--force")
	var exit *cmdutil.ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("err = %v, want exit code 3", err)
	}
	joined := strings.Join(fake.args, " ")
	if !strings.Contains(joined, "-T") || !strings.Contains(joined, "BatchMode=yes") || fake.args[len(fake.args)-1] != "php artisan migrate --force" {
		t.Fatalf("args = %q", fake.args)
	}
}

func TestAnSSHFailurePointsToTheKey(t *testing.T) {
	_, err := execute(t, &fakeRunner{err: exitCode(255)}, false, "en-abc123", "--", "true")
	if err == nil || errors.As(err, new(*cmdutil.ExitError)) {
		t.Fatalf("err = %v, want the mapped ssh error", err)
	}
}

func TestPrintShowsTheCommandWithoutRunningIt(t *testing.T) {
	fake := &fakeRunner{}
	out, err := execute(t, fake, false, "en-abc123", "--print", "--", "echo", "it's")
	if err != nil {
		t.Fatal(err)
	}
	if fake.args != nil {
		t.Fatal("--print ran ssh")
	}
	if !strings.HasPrefix(out, "ssh -T ") || !strings.HasSuffix(strings.TrimSpace(out), `en-abc123@ssh.us-e1a.frbit.app 'echo it'\''s'`) {
		t.Fatalf("output = %q", out)
	}
}

func TestTwoArgumentsBeforeTheDashAreRefused(t *testing.T) {
	if _, err := execute(t, &fakeRunner{}, false, "en-abc123", "php"); err == nil || !strings.Contains(err.Error(), "after --") {
		t.Fatalf("err = %v", err)
	}
}
