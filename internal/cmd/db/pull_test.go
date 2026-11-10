package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/sshexec"
	"github.com/spf13/cobra"
)

type configStore struct{}

func (configStore) Load() (config.Config, error) { return config.Config{}, nil }
func (configStore) Save(config.Config) error     { return nil }

type credentialStore struct{}

func (credentialStore) Get(string) (credentials.Credential, error) {
	return credentials.Credential{}, credentials.ErrNotFound
}
func (credentialStore) Set(string, credentials.Credential) error { return nil }
func (credentialStore) Delete(string) error                      { return nil }

type exitError int

func (e exitError) Error() string { return "exit status" }
func (e exitError) ExitCode() int { return int(e) }

type fakeRunner struct {
	name      string
	args      []string
	stdout    []byte
	err       error
	realCheck bool
	onRun     func(stdout io.Writer)
	ctx       context.Context
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	f.name, f.args = name, args
	if f.onRun != nil {
		f.onRun(stdout)
	}
	if _, err := stdout.Write(f.stdout); err != nil {
		return err
	}
	return f.err
}

const dump = "-- MySQL dump 10.13  Distrib 8.0\n-- Host: mysql    Database: app\nCREATE TABLE `users` (`id` int NOT NULL);\nINSERT INTO `users` VALUES (1);\n"

func gzipped(t *testing.T, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// run executes frbit db pull in dir against a fake API and the fake runner.
func run(t *testing.T, dir string, fake *fakeRunner, args ...string) (string, string, error) {
	t.Helper()
	checkTool := func(string) error { return nil }
	if fake.realCheck {
		checkTool = sshexec.Check
	}
	return execute(t, dir, invocation{runner: fake, check: checkTool, ctx: fake.ctx, args: append([]string{"db", "pull"}, args...)})
}

type invocation struct {
	runner   sshexec.Runner
	check    func(string) error
	ctx      context.Context
	stdin    io.Reader
	tty      bool
	requests *[]string
	args     []string
}

// execute runs the frbit db command of in inside dir against a fake API.
func execute(t *testing.T, dir string, in invocation) (string, string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	t.Setenv("FORTRABBIT_ENVIRONMENT", "")
	t.Chdir(dir)
	previousRunner, previousCheck := runner, checkTool
	runner, checkTool = in.runner, in.check
	t.Cleanup(func() { runner, checkTool = previousRunner, previousCheck })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if in.requests != nil {
			*in.requests = append(*in.requests, r.Method+" "+r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/environments/en-abc123" {
			_, _ = w.Write([]byte(`{"publicId":"en-abc123","sshHost":"en-abc123@ssh.us-e1a.frbit.app"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	}))
	t.Cleanup(server.Close)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	stdin := in.stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: stdin, Out: stdout, ErrOut: stderr, IsTTY: in.tty},
		ConfigStore:     configStore{},
		CredentialStore: credentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(NewCmdDb(factory))
	root.SetArgs(append([]string{"--host", server.URL}, in.args...))
	ctx := in.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range list {
		names = append(names, entry.Name())
	}
	return names
}

func TestDbPullRunsExpectedSSHCommand(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeRunner{stdout: gzipped(t, dump)}
	_, stderr, err := run(t, dir, fake, "en-abc123")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-T", "-a", "-x", "-o", "ConnectTimeout=10", "-o", "BatchMode=yes", "--", "en-abc123@ssh.us-e1a.frbit.app", `set -o pipefail; mysqldump --single-transaction --set-gtid-purged=OFF --no-tablespaces "$FORTRABBIT_DB_NAME" | gzip`}
	if fake.name != "ssh" || !slices.Equal(fake.args, want) {
		t.Fatalf("ran %s %q", fake.name, fake.args)
	}
	target := filepath.Join(dir, "en-abc123.sql.gz")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"en-abc123.sql.gz"}) {
		t.Fatalf("files %v", got)
	}
	if !strings.Contains(stderr, "en-abc123.sql.gz") {
		t.Fatalf("stderr %q", stderr)
	}

	if _, _, err := run(t, dir, &fakeRunner{stdout: gzipped(t, dump)}, "en-abc123"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwrite: %v", err)
	}
	if _, _, err := run(t, dir, &fakeRunner{stdout: gzipped(t, dump)}, "en-abc123", "--force"); err != nil {
		t.Fatalf("--force: %v", err)
	}

	stdout, _, err := run(t, dir, &fakeRunner{stdout: gzipped(t, dump)}, "en-abc123", "--stdout")
	if err != nil || stdout != dump {
		t.Fatalf("--stdout = %q, %v", stdout, err)
	}

	if _, _, err := run(t, dir, &fakeRunner{stdout: gzipped(t, dump)}, "en-zzz999"); err == nil || !strings.Contains(err.Error(), "en-zzz999 is not in your account; check the ID with frbit environments list") {
		t.Fatalf("foreign env: %v", err)
	}
}

func TestDbPullEmptyDumpFailsAndLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := run(t, dir, &fakeRunner{stdout: gzipped(t, "")}, "en-abc123"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty dump: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
	if _, _, err := run(t, dir, &fakeRunner{stdout: gzipped(t, "-- partial"), err: exitError(2)}, "en-abc123"); err == nil || !strings.Contains(err.Error(), "mysqldump failed") {
		t.Fatalf("dump failure: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
}

func TestDbPullSSHKeyRejectedPointsToDashboard(t *testing.T) {
	dir := t.TempDir()
	_, _, err := run(t, dir, &fakeRunner{err: exitError(255)}, "en-abc123")
	if err == nil || !strings.Contains(err.Error(), "https://dash.fortrabbit.com/new/ssh-key") {
		t.Fatalf("key rejected: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
}

func TestDbPullMissingSSHBinary(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeRunner{realCheck: true}
	t.Setenv("PATH", t.TempDir())
	_, _, err := run(t, dir, fake, "en-abc123")
	if err == nil || !strings.Contains(err.Error(), "ssh not found") {
		t.Fatalf("missing ssh: %v", err)
	}
	if fake.name != "" {
		t.Fatal("ran without ssh")
	}
}

func TestDbPullCtrlCRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fakeRunner{ctx: ctx, err: errors.New("signal: killed"), onRun: func(stdout io.Writer) {
		_, _ = stdout.Write(gzipped(t, dump))
		cancel()
	}}
	_, _, err := run(t, dir, fake, "en-abc123")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ctrl-C: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
}

func TestDbPullOutputToMissingFolder(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeRunner{stdout: gzipped(t, dump)}
	_, _, err := run(t, dir, fake, "en-abc123", "--output", "nope/")
	if err == nil || err.Error() != "folder nope/ does not exist" {
		t.Fatalf("missing folder: %v", err)
	}
	if fake.name != "" {
		t.Fatal("ran ssh")
	}
}
