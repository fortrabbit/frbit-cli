package link

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
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

const apps = `[{"publicId":"ap-aaa111","gitRepository":"acme/site","environments":["en-prod11"]}]`
const environments = `[{"publicId":"en-prod11","name":"production","git":{"branch":"main","directory":null}}]`

// run executes frbit link in dir with a fake git (remote, branch) and a fake API.
func run(t *testing.T, dir, remote, branch string, tty bool, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	t.Chdir(dir)
	previous := git
	git = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
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
			_, _ = w.Write([]byte(`{"publicId":"` + strings.TrimPrefix(r.URL.Path, "/v1/environments/") + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(stdin), Out: output, ErrOut: &bytes.Buffer{}, IsTTY: tty},
		ConfigStore:     configStore{},
		CredentialStore: credentialStore{},
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

func TestLinkMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, ".fortrabbit")
	if err := os.WriteFile(legacy, []byte("app-env-id=en-old111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "", "main", false, ""); err == nil || !strings.Contains(err.Error(), "run frbit link on a terminal to migrate .fortrabbit") {
		t.Fatalf("without TTY: %v", err)
	}
	output, err := run(t, dir, "", "main", true, "y\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(legacy, "environments.yml")); got != "main: en-old111\n" {
		t.Fatalf("environments.yml = %q", got)
	}
	if !strings.Contains(output, "en-old111") {
		t.Fatalf("output: %s", output)
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
