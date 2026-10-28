package setup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return "", nil
}

type uninstallFixture struct {
	configDir string
	cacheDir  string
	binary    string
	runner    *recordingRunner
}

func newUninstallFixture(t *testing.T, binaryPath string) uninstallFixture {
	t.Helper()
	// macOS hands out temp dirs behind the /var symlink, which locateBinary resolves.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := uninstallFixture{
		configDir: filepath.Join(root, "config", "frbit"),
		cacheDir:  filepath.Join(root, "cache"),
		binary:    filepath.Join(root, binaryPath),
		runner:    &recordingRunner{},
	}
	for _, path := range []string{filepath.Join(fixture.configDir, "config.json"), filepath.Join(fixture.cacheDir, "frbit", "update-check.json"), fixture.binary} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (f uninstallFixture) run(t *testing.T, input string, mcp *fakeMCPService, skills fakeSkillsService, goos string, args ...string) string {
	t.Helper()
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:          iostreams.IOStreams{In: strings.NewReader(input), Out: output, ErrOut: output},
		CredentialStore:    missingCredentials{},
		CredentialLockPath: filepath.Join(f.configDir, "credentials.lock"),
		Version:            "test",
	}
	system := uninstallSystem{
		executable: func() (string, error) { return f.binary, nil },
		cacheDir:   func() (string, error) { return f.cacheDir, nil },
		runner:     f.runner,
		goos:       goos,
	}
	root := &cobra.Command{Use: "frbit"}
	root.PersistentFlags().String("profile", app.DefaultProfile, "Credential profile")
	root.AddCommand(newCmdUninstall(factory, mcp, skills, system))
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(append([]string{"uninstall"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestUninstallRemovesEverything(t *testing.T) {
	fixture := newUninstallFixture(t, "bin/frbit")
	removed := []agentskills.Removal{}
	mcp := &fakeMCPService{claudePlugin: true, registered: []agent.Agent{agent.Codex}}
	skills := fakeSkillsService{installed: []agent.Agent{agent.Codex}, removed: &removed}
	output := fixture.run(t, "", mcp, skills, "darwin", "--yes")
	for _, expected := range []string{"plugin fortrabbit@fortrabbit", "binary at " + fixture.binary, "frbit uninstalled.", disconnectHint} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
	if mcp.removedPlugin != "fortrabbit@fortrabbit" || len(mcp.removed) != 1 || len(removed) != 1 {
		t.Fatalf("plugin = %q, mcp removed = %#v, skills removed = %#v", mcp.removedPlugin, mcp.removed, removed)
	}
	for _, path := range []string{fixture.configDir, filepath.Join(fixture.cacheDir, "frbit"), fixture.binary} {
		if exists(path) {
			t.Fatalf("%s still exists", path)
		}
	}
}

func TestUninstallUsesThePackageManager(t *testing.T) {
	for binaryPath, want := range map[string]string{
		"homebrew/Cellar/frbit/1.10.0/bin/frbit":                  "brew uninstall frbit",
		"lib/node_modules/@fortrabbit/cli-darwin-arm64/bin/frbit": "npm uninstall --global @fortrabbit/cli",
	} {
		fixture := newUninstallFixture(t, binaryPath)
		fixture.run(t, "", &fakeMCPService{}, fakeSkillsService{}, "darwin", "--yes")
		if !reflect.DeepEqual(fixture.runner.calls, []string{want}) {
			t.Fatalf("calls = %#v, want %q", fixture.runner.calls, want)
		}
		if !exists(fixture.binary) {
			t.Fatalf("%s was deleted by hand", fixture.binary)
		}
	}
}

func TestUninstallLeavesTheRunningBinaryToWindowsUsers(t *testing.T) {
	fixture := newUninstallFixture(t, "frbit.exe")
	output := fixture.run(t, "", &fakeMCPService{}, fakeSkillsService{}, "windows", "--yes")
	if !strings.Contains(output, "Delete "+fixture.binary+" to finish.") || !exists(fixture.binary) {
		t.Fatalf("output = %q", output)
	}
}

func TestUninstallAbortsWithoutConfirmation(t *testing.T) {
	fixture := newUninstallFixture(t, "bin/frbit")
	mcp := &fakeMCPService{claudePlugin: true}
	output := fixture.run(t, "n\n", mcp, fakeSkillsService{}, "darwin")
	if !strings.Contains(output, "Aborted.") || mcp.removedPlugin != "" || !exists(fixture.binary) || !exists(fixture.configDir) {
		t.Fatalf("output = %q", output)
	}
}
