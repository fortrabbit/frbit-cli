package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

// fakeRunner answers rsync --version with version and records the sync call.
type fakeRunner struct {
	version string
	syncs   [][]string
	err     error
	stderr  string
	onSync  func()
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if name != "rsync" {
		return errors.New("not rsync: " + name)
	}
	if slices.Equal(args, []string{"--version"}) {
		_, err := io.WriteString(stdout, f.version)
		return err
	}
	f.syncs = append(f.syncs, args)
	_, _ = io.WriteString(stderr, f.stderr)
	if f.onSync != nil {
		f.onSync()
	}
	return f.err
}

const rsync3 = "rsync  version 3.2.7  protocol version 31\n"

type invocation struct {
	runner   sshexec.Runner
	check    func(string) error
	ctx      context.Context
	stdin    io.Reader
	tty      bool
	env      string
	requests *[]string
	args     []string
}

func execute(t *testing.T, dir string, in invocation) (string, string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	t.Setenv("FORTRABBIT_ENVIRONMENT", "")
	t.Chdir(dir)
	previousRunner, previousCheck := runner, checkTool
	runner, checkTool = in.runner, in.check
	if checkTool == nil {
		checkTool = func(string) error { return nil }
	}
	t.Cleanup(func() { runner, checkTool = previousRunner, previousCheck })
	body := in.env
	if body == "" {
		body = `{"publicId":"en-abc123","sshHost":"en-abc123@ssh.us-e1a.frbit.app","git":{"branch":null,"directory":null}}`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if in.requests != nil {
			*in.requests = append(*in.requests, r.Method+" "+r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/environments/en-abc123" {
			_, _ = w.Write([]byte(body))
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
	root.AddCommand(NewCmdFiles(factory))
	root.SetArgs(append([]string{"--host", server.URL}, in.args...))
	ctx := in.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func TestFilesPullBuildsRsyncArgs(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123"},
			[]string{"-az", "--info=progress2", "-e", sshE, "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--", "en-abc123@ssh.us-e1a.frbit.app:/data/www/", "./"}},
		{[]string{"files", "pull", "web/uploads/", "--environment", "en-abc123", "--delete", "--confirm", "en-abc123"},
			[]string{"-az", "--info=progress2", "-e", sshE, "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--delete", "--", "en-abc123@ssh.us-e1a.frbit.app:/data/www/web/uploads", "./web/"}},
		{[]string{"files", "pull", "./storage", "--environment", "en-abc123", "--dry-run"},
			[]string{"-az", "--info=progress2", "-e", sshE, "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--dry-run", "-v", "--", "en-abc123@ssh.us-e1a.frbit.app:/data/www/storage", "./"}},
	}
	for _, c := range cases {
		fake := &fakeRunner{version: rsync3}
		if _, stderr, err := execute(t, dir, invocation{runner: fake, args: c.args}); err != nil {
			t.Fatalf("%v: %v\n%s", c.args, err, stderr)
		}
		if len(fake.syncs) != 1 || !slices.Equal(fake.syncs[0], c.want) {
			t.Fatalf("%v: ran %q", c.args, fake.syncs)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "web")); err != nil {
		t.Fatalf("local parent not created: %v", err)
	}
}

func TestFilesRsyncArgsOldRsync(t *testing.T) {
	for _, version := range []string{"openrsync: protocol version 29\nrsync version 2.6.9 compatible\n", "rsync  version 2.6.9  protocol version 29\n", ""} {
		if got := progressFlag(version); got != "--progress" {
			t.Fatalf("%q: %q", version, got)
		}
	}
	for _, version := range []string{rsync3, "rsync  version 3.1.0  protocol version 31\n", "rsync  version v4.0.0  protocol version 32\n"} {
		if got := progressFlag(version); got != "--info=progress2" {
			t.Fatalf("%q: %q", version, got)
		}
	}
	got := rsyncArgs("--progress", "ssh -T", true, true, "a", "b")
	want := []string{"-az", "--progress", "-e", "ssh -T", "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--dry-run", "-v", "--delete", "--", "a", "b"}
	if !slices.Equal(got, want) {
		t.Fatalf("rsyncArgs = %q", got)
	}
	dir := t.TempDir()
	fake := &fakeRunner{version: "openrsync: protocol version 29\nrsync version 2.6.9 compatible\n"}
	if _, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123"}}); err != nil {
		t.Fatal(err)
	}
	if len(fake.syncs) != 1 || fake.syncs[0][1] != "--progress" {
		t.Fatalf("ran %q", fake.syncs)
	}
}

func TestFilesRejectsParentPath(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"..": ".. is not allowed", "../x": ".. is not allowed", "a/../../b": ".. is not allowed", "a/..": ".. is not allowed", `a\..\b`: "backslash",
		"/etc": "not a relative path", "~": "not a relative path", "~/x": "not a relative path",
		"a;id": "characters other than", "$(id)": "characters other than", "a b": "characters other than",
		`.\./x`: "backslash", `web\uploads`: "backslash",
	}
	for path, want := range cases {
		for _, command := range []string{"pull", "push"} {
			fake := &fakeRunner{version: rsync3}
			requests := []string{}
			args := []string{"files", command, path, "--environment", "en-abc123"}
			if command == "push" {
				args = append(args, "--confirm", "en-abc123")
			}
			_, _, err := execute(t, dir, invocation{runner: fake, requests: &requests, args: args})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s %q: %v; want %q", command, path, err, want)
			}
			if len(fake.syncs) != 0 || len(requests) != 0 {
				t.Fatalf("%s %q: syncs %q, requests %q", command, path, fake.syncs, requests)
			}
		}
	}
	for path, want := range map[string]string{"": ".", ".": ".", "./": ".", "a/b/": "a/b", "a//b": "a/b", "a/./b": "a/b", "web/..x": "web/..x"} {
		got, err := cleanPath(path)
		if err != nil || got != want {
			t.Fatalf("cleanPath(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestFilesPushWithoutTTYRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"files", "push", "--environment", "en-abc123"},
		{"files", "push", "--environment", "en-abc123", "--confirm", "en-zzz999"},
	} {
		fake := &fakeRunner{version: rsync3}
		requests := []string{}
		_, _, err := execute(t, dir, invocation{runner: fake, requests: &requests, args: args})
		if err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("%v: %v", args, err)
		}
		if len(fake.syncs) != 0 || len(requests) != 0 {
			t.Fatalf("%v: syncs %q, requests %q", args, fake.syncs, requests)
		}
	}

	fake := &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "push", "--environment", "en-abc123", "--confirm", "en-abc123", "--delete"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"-az", "--info=progress2", "-e", sshE, "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--delete", "--", "./", "en-abc123@ssh.us-e1a.frbit.app:/data/www/"}
	if len(fake.syncs) != 1 || !slices.Equal(fake.syncs[0], want) {
		t.Fatalf("ran %q", fake.syncs)
	}

	// A dry run changes nothing, so it needs no confirmation.
	fake = &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "push", "--environment", "en-abc123", "--dry-run"}}); err != nil || len(fake.syncs) != 1 {
		t.Fatalf("dry run: %v, %q", err, fake.syncs)
	}

	// A local path that does not exist fails before any request.
	requests := []string{}
	fake = &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, requests: &requests, args: []string{"files", "push", "missing", "--environment", "en-abc123", "--confirm", "en-abc123"}}); err == nil || len(requests) != 0 || len(fake.syncs) != 0 {
		t.Fatalf("missing: %v, %q", err, requests)
	}
}

func TestFilesPushPromptsOnTTY(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web", "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRunner{version: rsync3}
	_, stderr, err := execute(t, dir, invocation{runner: fake, tty: true, stdin: strings.NewReader("nope\n"), args: []string{"files", "push", "web/uploads", "--environment", "en-abc123", "--delete"}})
	if err != nil || len(fake.syncs) != 0 || !strings.Contains(stderr, "Aborted.") {
		t.Fatalf("declined: %v, %q, %q", err, fake.syncs, stderr)
	}
	for _, want := range []string{"/data/www/web/uploads", "en-abc123", "overwritten", "deleted", "Type en-abc123 to confirm:"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q: %q", want, stderr)
		}
	}
	if strings.Contains(stderr, "deployment") {
		t.Fatalf("repo warning without a repo: %q", stderr)
	}

	fake = &fakeRunner{version: rsync3}
	_, _, err = execute(t, dir, invocation{runner: fake, tty: true, stdin: strings.NewReader("en-abc123\n"), args: []string{"files", "push", "web/uploads", "--environment", "en-abc123"}})
	want := []string{"-az", "--info=progress2", "-e", sshTTY, "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--", "./web/uploads", "en-abc123@ssh.us-e1a.frbit.app:/data/www/web/"}
	if err != nil || len(fake.syncs) != 1 || !slices.Equal(fake.syncs[0], want) {
		t.Fatalf("confirmed: %v, %q", err, fake.syncs)
	}
}

func TestFilesPushWarnsOnConnectedRepo(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeRunner{version: rsync3}
	env := `{"publicId":"en-abc123","sshHost":"en-abc123@ssh.us-e1a.frbit.app","git":{"branch":"main","directory":null}}`
	_, stderr, err := execute(t, dir, invocation{runner: fake, env: env, tty: true, stdin: strings.NewReader("no\n"), args: []string{"files", "push", "--environment", "en-abc123"}})
	if err != nil || len(fake.syncs) != 0 {
		t.Fatalf("%v, %q", err, fake.syncs)
	}
	if !strings.Contains(stderr, "next deployment") || !strings.Contains(stderr, "main") {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestFilesMissingRsync(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	fake := &fakeRunner{version: rsync3}
	requests := []string{}
	_, _, err := execute(t, dir, invocation{runner: fake, check: sshexec.Check, requests: &requests, args: []string{"files", "pull", "--environment", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "rsync") || len(requests) != 0 || len(fake.syncs) != 0 {
		t.Fatalf("%v, %q", err, requests)
	}
}

func TestFilesRsyncFailures(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeRunner{version: rsync3, err: exitError(255)}
	_, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), sshexec.KeyURL) {
		t.Fatalf("255: %v", err)
	}
	fake = &fakeRunner{version: rsync3, err: exitError(255), stderr: "Host key verification failed.\r\nrsync: connection unexpectedly closed\n"}
	_, _, err = execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "ssh-keyscan ssh.us-e1a.frbit.app") {
		t.Fatalf("host key: %v", err)
	}
	fake = &fakeRunner{version: rsync3, err: exitError(23)}
	_, _, err = execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "rsync") || !strings.Contains(err.Error(), "23") {
		t.Fatalf("23: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	fake = &fakeRunner{version: rsync3, err: exitError(20), onSync: cancel}
	_, stderr, err := execute(t, dir, invocation{runner: fake, ctx: ctx, args: []string{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123"}})
	if !errors.Is(err, context.Canceled) || !strings.Contains(stderr, "partway") {
		t.Fatalf("cancel: %v, %q", err, stderr)
	}
}

func TestFilesPullDeleteRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"files", "pull", "--environment", "en-abc123", "--delete"},
		{"files", "pull", "--environment", "en-abc123", "--delete", "--confirm", "en-zzz999"},
	} {
		fake := &fakeRunner{version: rsync3}
		requests := []string{}
		_, _, err := execute(t, dir, invocation{runner: fake, requests: &requests, args: args})
		if err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("%v: %v", args, err)
		}
		if len(fake.syncs) != 0 || len(requests) != 0 {
			t.Fatalf("%v: syncs %q, requests %q", args, fake.syncs, requests)
		}
	}

	fake := &fakeRunner{version: rsync3}
	_, stderr, err := execute(t, dir, invocation{runner: fake, tty: true, stdin: strings.NewReader("no\n"), args: []string{"files", "pull", "storage", "--environment", "en-abc123", "--delete"}})
	if err != nil || len(fake.syncs) != 0 || !strings.Contains(stderr, "Local files under "+filepath.Join(dir, "storage")+" that are not on en-abc123 are deleted") {
		t.Fatalf("declined: %v, %q, %q", err, fake.syncs, stderr)
	}

	fake = &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "--environment", "en-abc123", "--delete", "--confirm", "en-abc123"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"-az", "--info=progress2", "-e", sshE, "--exclude=/.git/", "--exclude=/.fortrabbit/", "--exclude=/.env", "--exclude=/.env.*", "--exclude=.frbit-*", "--exclude=en-*.sql.gz", "--exclude=ba-*-database.*", "--exclude=ba-*-files.*", "--delete", "--", "en-abc123@ssh.us-e1a.frbit.app:/data/www/", "./"}
	if len(fake.syncs) != 1 || !slices.Equal(fake.syncs[0], want) {
		t.Fatalf("ran %q", fake.syncs)
	}

	// A dry run deletes nothing, so it needs no confirmation.
	fake = &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "--environment", "en-abc123", "--delete", "--dry-run"}}); err != nil || len(fake.syncs) != 1 {
		t.Fatalf("dry run: %v, %q", err, fake.syncs)
	}
}

func TestFilesPullWithoutPathRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"files", "pull", "--environment", "en-abc123"},
		{"files", "pull", ".", "--environment", "en-abc123", "--confirm", "en-zzz999"},
	} {
		fake := &fakeRunner{version: rsync3}
		requests := []string{}
		_, _, err := execute(t, dir, invocation{runner: fake, requests: &requests, args: args})
		if err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("%v: %v", args, err)
		}
		if len(fake.syncs) != 0 || len(requests) != 0 {
			t.Fatalf("%v: syncs %q, requests %q", args, fake.syncs, requests)
		}
	}

	fake := &fakeRunner{version: rsync3}
	_, stderr, err := execute(t, dir, invocation{runner: fake, tty: true, stdin: strings.NewReader("no\n"), args: []string{"files", "pull", "--environment", "en-abc123"}})
	if err != nil || len(fake.syncs) != 0 || !strings.Contains(stderr, "Aborted.") {
		t.Fatalf("declined: %v, %q, %q", err, fake.syncs, stderr)
	}
	for _, want := range []string{"Local files under " + dir + " are overwritten by the copies on en-abc123.", "Type en-abc123 to confirm:"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q: %q", want, stderr)
		}
	}

	fake = &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, tty: true, stdin: strings.NewReader("en-abc123\n"), args: []string{"files", "pull", "--environment", "en-abc123"}}); err != nil || len(fake.syncs) != 1 {
		t.Fatalf("confirmed: %v, %q", err, fake.syncs)
	}

	// A dry run and a folder path need no confirmation.
	for _, args := range [][]string{
		{"files", "pull", "--environment", "en-abc123", "--dry-run"},
		{"files", "pull", "web", "--environment", "en-abc123"},
	} {
		fake = &fakeRunner{version: rsync3}
		if _, _, err := execute(t, dir, invocation{runner: fake, args: args}); err != nil || len(fake.syncs) != 1 {
			t.Fatalf("%v: %v, %q", args, err, fake.syncs)
		}
	}
}

func TestExcludesKeepDumpsAndTempFilesOutOfAPush(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
	src, dst := t.TempDir(), t.TempDir()
	kept := []string{"index.php", "web/notes.sql.gz", "notes.sql.gz", "web/.env"}
	skipped := []string{"web/en-abc123.sql.gz", "web/uploads/ba-abc123-files.zip", "web/uploads/.frbit-download-1", "en-abc123.sql.gz", "ba-abc123-database.sql.gz", "ba-abc123-files.zip", "ba-abc123-files.tar.gz", ".frbit-db-pull-123", ".frbit-download-456", ".env", ".git/config"}
	for _, name := range append(append([]string{}, kept...), skipped...) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(src, name)), 0o755)
		if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := append([]string{"-r"}, excludes...)
	if output, err := exec.Command("rsync", append(args, src+"/", dst+"/")...).CombinedOutput(); err != nil {
		t.Fatalf("rsync: %v\n%s", err, output)
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s not copied", name)
		}
	}
	for _, name := range skipped {
		if _, err := os.Stat(filepath.Join(dst, name)); err == nil {
			t.Errorf("%s copied", name)
		}
	}
}

func TestFilesRefusesWrongLocalRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, args := range [][]string{
		{"files", "push", "--environment", "en-abc123", "--confirm", "en-abc123"},
		{"files", "pull", "--environment", "en-abc123", "--confirm", "en-abc123", "--dry-run"},
		{"files", "pull", "web", "--delete", "--environment", "en-abc123", "--confirm", "en-abc123"},
	} {
		fake := &fakeRunner{version: rsync3}
		requests := []string{}
		_, _, err := execute(t, home, invocation{runner: fake, requests: &requests, args: args})
		if err == nil || !strings.Contains(err.Error(), "home folder") || len(requests) != 0 || len(fake.syncs) != 0 {
			t.Fatalf("%v: %v, %q, %q", args, err, requests, fake.syncs)
		}
	}

	top := t.TempDir()
	sub := filepath.Join(top, "public")
	if err := os.MkdirAll(filepath.Join(sub, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := git
	git = func(_ context.Context, _ string, args ...string) (string, error) {
		if strings.Join(args, " ") == "rev-parse --show-toplevel" {
			return top, nil
		}
		return "", errors.New("git failed")
	}
	t.Cleanup(func() { git = previous })
	for _, args := range [][]string{
		{"files", "push", "--environment", "en-abc123", "--confirm", "en-abc123"},
		{"files", "push", "web", "--delete", "--environment", "en-abc123", "--confirm", "en-abc123"},
	} {
		fake := &fakeRunner{version: rsync3}
		_, _, err := execute(t, sub, invocation{runner: fake, args: args})
		if err == nil || !strings.Contains(err.Error(), "run from the project root "+top) || len(fake.syncs) != 0 {
			t.Fatalf("%v: %v", args, err)
		}
	}
	// A path sync without --delete is relative to the current folder and allowed anywhere.
	fake := &fakeRunner{version: rsync3}
	if _, _, err := execute(t, sub, invocation{runner: fake, args: []string{"files", "push", "web", "--environment", "en-abc123", "--confirm", "en-abc123"}}); err != nil || len(fake.syncs) != 1 {
		t.Fatalf("path push: %v, %q", err, fake.syncs)
	}
	// From the top level the whole-folder sync runs, and the prompt names the local folder.
	fake = &fakeRunner{version: rsync3}
	_, stderr, err := execute(t, top, invocation{runner: fake, tty: true, stdin: strings.NewReader("no\n"), args: []string{"files", "pull", "--environment", "en-abc123"}})
	if err != nil || !strings.Contains(stderr, top) {
		t.Fatalf("top: %v, %q", err, stderr)
	}
}

func TestExcludesApplyInsideASyncedSubfolder(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
	src, dst := t.TempDir(), t.TempDir()
	for _, name := range []string{"web/uploads/a.jpg", "web/uploads/en-abc123.sql.gz", "web/uploads/.frbit-db-pull-1", "web/uploads/ba-abc123-database.sql.gz"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(src, name)), 0o755)
		if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Same shape as endpoints for a path push: the folder without a trailing slash into its parent.
	args := append([]string{"-r"}, excludes...)
	if output, err := exec.Command("rsync", append(args, src+"/web/uploads", dst+"/")...).CombinedOutput(); err != nil {
		t.Fatalf("rsync: %v\n%s", err, output)
	}
	entries, _ := os.ReadDir(filepath.Join(dst, "uploads"))
	if len(entries) != 1 || entries[0].Name() != "a.jpg" {
		t.Fatalf("copied %v", entries)
	}
}

func TestFilesEnvironmentFlagTakesLabel(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".fortrabbit"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit", "environments.yml"), []byte("production: en-abc123\nstaging: en-other1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRunner{version: rsync3}
	stdout, stderr, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "pull", "web", "-e", "production"}})
	if err != nil || len(fake.syncs) != 1 || !strings.Contains(stderr, "→ en-abc123 (production, from .fortrabbit/environments.yml)\n") || strings.Contains(stdout, "→") {
		t.Fatalf("err %v, stderr %q", err, stderr)
	}
	// --confirm compares with the resolved ID, never the label.
	fake = &fakeRunner{version: rsync3}
	if _, _, err := execute(t, dir, invocation{runner: fake, args: []string{"files", "push", "web", "--environment", "production", "--confirm", "production"}}); err == nil || !strings.Contains(err.Error(), "en-abc123") || len(fake.syncs) != 0 {
		t.Fatalf("label confirm: %v", err)
	}
}
