package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/fortrabbit/frbit-cli/internal/testutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

const apps = `[{"publicId":"ap-aaa111","gitRepository":"acme/site","environments":["en-prod11"]}]`
const environments = `[{"publicId":"en-prod11","name":"production","git":{"branch":"main","directory":null}}]`

// topLevel is what the fake git reports as the checkout root; empty means outside git.
var topLevel string

// envValue is FORTRABBIT_ENVIRONMENT during run.
var envValue string

// run executes frbit link in dir with a fake git (remote, branch) and a fake API.
func run(t *testing.T, dir, remote, branch string, tty bool, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	t.Setenv("FORTRABBIT_ENVIRONMENT", envValue)
	t.Chdir(dir)
	previous := git
	git = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			if topLevel != "" {
				return topLevel, nil
			}
		case "remote get-url origin":
			if remote != "" {
				return remote, nil
			}
		case "symbolic-ref --short HEAD":
			if branch != "" {
				return branch, nil
			}
		}
		return "", errors.New("git failed")
	}
	t.Cleanup(func() { git = previous })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(apps))
		case r.URL.Path == "/v1/environments":
			_, _ = w.Write([]byte(environments))
		case strings.HasPrefix(r.URL.Path, "/v1/environments/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/environments/")
			if id == "en-gone11" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"publicId":"` + id + `","name":"production","sshHost":"` + id + `@ssh.eu-w1a.frbit.app"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(stdin), Out: output, ErrOut: &bytes.Buffer{}, IsTTY: tty},
		ConfigStore:     testutil.ConfigStore{},
		CredentialStore: testutil.CredentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(output)
	root.SetErr(&bytes.Buffer{})
	root.AddCommand(NewCmdLink(factory))
	root.SetArgs(append([]string{"--host", server.URL, "link"}, args...))
	err := root.ExecuteContext(context.Background())
	return output.String(), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestLinkRefusesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit"), []byte("app-env-id=en-old111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tty := range []bool{false, true} {
		if _, err := run(t, dir, "", "main", tty, "y\n"); err == nil || err.Error() != ".fortrabbit is a link file from an older fortrabbit skill; delete it and run frbit link <env-id>" {
			t.Fatalf("tty %v: %v", tty, err)
		}
	}
}

func TestLinkWithoutEnvironmentFailsOnTerminalToo(t *testing.T) {
	for _, tty := range []bool{false, true} {
		if _, err := run(t, t.TempDir(), "", "main", tty, "1\n"); err == nil || err.Error() != "no environment to link; pass the environment ID: frbit link <env-id>" {
			t.Fatalf("tty %v: %v", tty, err)
		}
	}
}

func TestLinkLeavesDeployFileUntouched(t *testing.T) {
	dir := t.TempDir()
	deploy := filepath.Join(dir, ".fortrabbit", "deploy.yml")
	if err := os.MkdirAll(filepath.Dir(deploy), 0o755); err != nil {
		t.Fatal(err)
	}
	const deployContent = "# deploy\nbuild:\n  - composer install\n"
	if err := os.WriteFile(deploy, []byte(deployContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "", "staging", false, "", "en-stag11"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, deploy); got != deployContent {
		t.Fatalf("deploy.yml changed: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, ".fortrabbit", "environments.yml")); got != "staging: en-stag11\n" {
		t.Fatalf("environments.yml = %q", got)
	}
	if _, err := run(t, dir, "", "", false, "", "en-prod11", "--label", "live"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, ".fortrabbit", "environments.yml")); got != "staging: en-stag11\nlive: en-prod11\n" {
		t.Fatalf("environments.yml = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ".fortrabbit"))
	if len(entries) != 2 {
		t.Fatalf("unexpected files in .fortrabbit: %v", entries)
	}
}

func TestLinkConnectedRepoWritesNothing(t *testing.T) {
	dir := t.TempDir()
	output, err := run(t, dir, "git@github.com:acme/site.git", "main", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "en-prod11") || !strings.Contains(output, "acme/site") {
		t.Fatalf("output: %s", output)
	}
	if _, err := os.Stat(filepath.Join(dir, ".fortrabbit")); !os.IsNotExist(err) {
		t.Fatalf(".fortrabbit created: %v", err)
	}
	if _, err := run(t, dir, "git@github.com:acme/site.git", "main", false, "", "en-prod11"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".fortrabbit")); !os.IsNotExist(err) {
		t.Fatalf(".fortrabbit created for the connected environment: %v", err)
	}
}

func TestLinkWithoutBranchNeedsLabel(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "", "", false, "", "en-prod11"); err == nil || !strings.Contains(err.Error(), "--label") {
		t.Fatalf("err = %v", err)
	}
}

func TestLinkUsesGitTopLevel(t *testing.T) {
	top := t.TempDir()
	sub := filepath.Join(top, "web", "uploads")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	topLevel = top
	t.Cleanup(func() { topLevel = "" })
	if _, err := run(t, sub, "", "staging", false, "", "en-stag11"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(top, ".fortrabbit", "environments.yml")); got != "staging: en-stag11\n" {
		t.Fatalf("environments.yml = %q", got)
	}
	if _, err := os.Stat(filepath.Join(sub, ".fortrabbit")); !os.IsNotExist(err) {
		t.Fatalf(".fortrabbit written in the subfolder: %v", err)
	}
	output, err := run(t, sub, "", "staging", false, "")
	if err != nil || !strings.Contains(output, "en-stag11") {
		t.Fatalf("output %q, err %v", output, err)
	}
}

func TestLinkBareReportsWhatCommandsResolve(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "", "staging", false, "", "en-prod11", "--label", "production"); err != nil {
		t.Fatal(err)
	}
	output, err := run(t, dir, "", "main", false, "")
	if err != nil || !strings.Contains(output, "en-prod11") || !strings.Contains(output, "production") {
		t.Fatalf("single entry: %q, %v", output, err)
	}
	envValue = "en-envv11"
	t.Cleanup(func() { envValue = "" })
	output, err = run(t, t.TempDir(), "", "main", false, "")
	if err != nil || !strings.Contains(output, "en-envv11") || !strings.Contains(output, "FORTRABBIT_ENVIRONMENT") {
		t.Fatalf("env: %q, %v", output, err)
	}
}

func TestLinkJSON(t *testing.T) {
	dir := t.TempDir()
	output, err := run(t, dir, "git@github.com:acme/site.git", "main", false, "", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("%v: %q", err, output)
	}
	want := map[string]any{"environment": "en-prod11", "name": "production", "source": "repository", "repository": "acme/site", "branch": "main", "app": "ap-aaa111", "sshHost": "en-prod11@ssh.eu-w1a.frbit.app", "region": "eu-w1a"}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v (%s)", key, got[key], value, output)
		}
	}
	if _, err := run(t, t.TempDir(), "", "main", false, "", "--json"); err == nil {
		t.Fatal("expected an error without an environment")
	}
}

func TestLinkStaleEntryNamesTheFix(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".fortrabbit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit", "environments.yml"), []byte("main: en-gone11\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"--json"}, {"main"}} {
		_, err := run(t, dir, "", "main", false, "", args...)
		if err == nil || !strings.Contains(err.Error(), "relink with frbit link <env-id> --label main, or edit .fortrabbit/environments.yml") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	_, err := run(t, dir, "", "main", false, "", "en-gone11")
	if err == nil || !strings.Contains(err.Error(), "frbit environments list") || strings.Contains(err.Error(), "run frbit link") {
		t.Fatalf("ID argument: %v", err)
	}
}
