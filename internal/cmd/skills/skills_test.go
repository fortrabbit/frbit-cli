package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

type fixtureTransport struct {
	archive      []byte
	releaseCalls int
	archiveCalls int
}

func (transport *fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var status = http.StatusOK
	var body []byte
	switch request.URL.Path {
	case "/release":
		transport.releaseCalls++
		body = []byte(`{"tag_name":"v1.2.3"}`)
	case "/archive/v1.2.3":
		transport.archiveCalls++
		body = transport.archive
	default:
		status = http.StatusNotFound
		body = []byte("not found")
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}, nil
}

func TestProjectSkillsLifecycle(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	transport := &fixtureTransport{archive: skillsArchive(t, "1.2.3")}
	service := fixtureService(home, project, transport)
	factory := &app.Factory{Version: "9.8.7", IOStreams: iostreams.IOStreams{In: strings.NewReader("n\n")}}

	installOutput := executeSkills(t, factory, service, "install", "--project")
	for _, expected := range []string{
		"Installed fortrabbit agent skills v1.2.3",
		filepath.Join(project, ".claude", "skills", "fortrabbit"),
		filepath.Join(project, ".agents", "skills", "fortrabbit-api-access"),
		filepath.Join(project, ".github", "instructions", "fortrabbit.instructions.md"),
	} {
		if !strings.Contains(installOutput, expected) {
			t.Fatalf("install output %q does not contain %q", installOutput, expected)
		}
	}
	assertFileContents(t, filepath.Join(project, ".claude", "skills", "fortrabbit", "SKILL.md"), "# fortrabbit\n")
	assertFileContents(t, filepath.Join(project, ".agents", "skills", "fortrabbit", "references", "deploy.md"), "deploy\n")
	assertFileContents(t, filepath.Join(project, ".claude", "skills", "fortrabbit-api-access", ".version"), "1.2.3\n")
	assertFileContents(t, filepath.Join(project, ".github", "instructions", "fortrabbit.instructions.md.version"), "1.2.3\n")
	if info, err := os.Stat(filepath.Join(project, ".agents", "skills", "fortrabbit", "update.sh")); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o755 {
		t.Fatalf("update script mode = %o", info.Mode().Perm())
	}

	listOutput := executeSkills(t, factory, service, "list", "--project")
	for _, expected := range []string{"frbit CLI version: 9.8.7", "AGENT", "claude-code", "codex", "copilot", "fortrabbit-api-access", "1.2.3", "project"} {
		if !strings.Contains(listOutput, expected) {
			t.Fatalf("list output %q does not contain %q", listOutput, expected)
		}
	}

	archiveCalls := transport.archiveCalls
	updateOutput := executeSkills(t, factory, service, "update", "--project")
	if !strings.Contains(updateOutput, "up to date (v1.2.3)") {
		t.Fatalf("update output = %q", updateOutput)
	}
	if transport.archiveCalls != archiveCalls {
		t.Fatalf("no-op update downloaded archive: calls %d -> %d", archiveCalls, transport.archiveCalls)
	}

	abortOutput := executeSkills(t, factory, service, "remove", "--project")
	if !strings.Contains(abortOutput, "The following paths will be removed:") || !strings.Contains(abortOutput, "Aborted.") {
		t.Fatalf("abort output = %q", abortOutput)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "fortrabbit")); err != nil {
		t.Fatalf("aborted removal changed install: %v", err)
	}

	removeOutput := executeSkills(t, factory, service, "remove", "--project", "--yes")
	if !strings.Contains(removeOutput, "Removed ") {
		t.Fatalf("remove output = %q", removeOutput)
	}
	for _, path := range []string{
		filepath.Join(project, ".claude", "skills", "fortrabbit"),
		filepath.Join(project, ".agents", "skills", "fortrabbit-api-access"),
		filepath.Join(project, ".github", "instructions", "fortrabbit.instructions.md"),
		filepath.Join(project, ".github", "instructions", "fortrabbit.instructions.md.version"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists or stat failed unexpectedly: %v", path, err)
		}
	}
}

func TestInstallTargetsOnlyExplicitAgent(t *testing.T) {
	project := t.TempDir()
	transport := &fixtureTransport{archive: skillsArchive(t, "1.2.3")}
	service := fixtureService(t.TempDir(), project, transport)
	factory := &app.Factory{IOStreams: iostreams.IOStreams{In: strings.NewReader("")}}

	executeSkills(t, factory, service, "install", "--project", "--agent", "codex")
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "fortrabbit", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	for _, unexpected := range []string{filepath.Join(project, ".claude"), filepath.Join(project, ".github")} {
		if _, err := os.Stat(unexpected); !os.IsNotExist(err) {
			t.Fatalf("unexpected target %s was created", unexpected)
		}
	}
}

func TestUpdateMigratesExistingScriptInstall(t *testing.T) {
	project := t.TempDir()
	legacySkill := filepath.Join(project, ".agents", "skills", "fortrabbit")
	if err := os.MkdirAll(legacySkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacySkill, "SKILL.md"), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacySkill, ".version"), []byte("0.2.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	transport := &fixtureTransport{archive: skillsArchive(t, "1.2.3")}
	service := fixtureService(t.TempDir(), project, transport)
	factory := &app.Factory{Version: "9.8.7", IOStreams: iostreams.IOStreams{In: strings.NewReader("")}}
	output := executeSkills(t, factory, service, "update", "--project", "--agent", "codex")
	if !strings.Contains(output, "Updated fortrabbit agent skills v1.2.3") {
		t.Fatalf("update output = %q", output)
	}
	if transport.archiveCalls != 1 {
		t.Fatalf("archive calls = %d, want 1", transport.archiveCalls)
	}
	assertFileContents(t, filepath.Join(legacySkill, "SKILL.md"), "# fortrabbit\n")
	assertFileContents(t, filepath.Join(legacySkill, ".version"), "1.2.3\n")
	assertFileContents(t, filepath.Join(project, ".agents", "skills", "fortrabbit-api-access", ".version"), "1.2.3\n")
}

func TestInstallRejectsArchiveVersionMismatch(t *testing.T) {
	project := t.TempDir()
	transport := &fixtureTransport{archive: skillsArchive(t, "9.9.9")}
	service := fixtureService(t.TempDir(), project, transport)
	factory := &app.Factory{IOStreams: iostreams.IOStreams{In: strings.NewReader("")}}
	command := NewCmdSkills(factory, service)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"install", "--project"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "does not match release") {
		t.Fatalf("error = %v", err)
	}
}

func executeSkills(t *testing.T, factory *app.Factory, service *agentskills.Service, arguments ...string) string {
	t.Helper()
	output := &bytes.Buffer{}
	command := NewCmdSkills(factory, service)
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(arguments)
	if err := command.Execute(); err != nil {
		t.Fatalf("frbit skills %s: %v", strings.Join(arguments, " "), err)
	}
	return output.String()
}

func fixtureService(home string, project string, transport *fixtureTransport) *agentskills.Service {
	return agentskills.NewService(agentskills.Options{
		HTTPClient: &http.Client{Transport: transport},
		Source: agentskills.Source{
			ReleaseURL: "https://skills.test/release",
			ArchiveURL: func(tag string) string { return "https://skills.test/archive/" + tag },
		},
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return project, nil },
		Now:        func() time.Time { return time.Unix(123456789, 0) },
		UserAgent:  "frbit/test",
	})
}

func skillsArchive(t *testing.T, version string) []byte {
	t.Helper()
	files := map[string]string{
		"agent-skills-fixture/VERSION":                                         version + "\n",
		"agent-skills-fixture/update.sh":                                       "#!/bin/sh\n",
		"agent-skills-fixture/uninstall.sh":                                    "#!/bin/sh\n",
		"agent-skills-fixture/skills/fortrabbit/SKILL.md":                      "# fortrabbit\n",
		"agent-skills-fixture/skills/fortrabbit/references/deploy.md":          "deploy\n",
		"agent-skills-fixture/skills/fortrabbit-api-access/SKILL.md":           "# API access\n",
		"agent-skills-fixture/.github/instructions/fortrabbit.instructions.md": "# Copilot\n",
	}
	buffer := &bytes.Buffer{}
	gzipWriter := gzip.NewWriter(buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:       "pax_global_header",
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": "git archive fixture"},
	}); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func assertFileContents(t *testing.T, path string, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != expected {
		t.Fatalf("%s = %q, want %q", path, contents, expected)
	}
}
