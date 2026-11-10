package root

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rootViaLabel = "→ en-abc123 (production, from .fortrabbit/environments.yml)\n"

// runTarget runs args in a temp checkout whose link file maps production to en-abc123, with FORTRABBIT_ENVIRONMENT set to envVar.
func runTarget(t *testing.T, envVar string, args ...string) (stdout, stderr string, paths []string, err error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".fortrabbit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit", "environments.yml"), []byte("production: en-abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("FORTRABBIT_ENVIRONMENT", envVar)
	t.Setenv("FRBIT_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/v1/environments/en-abc123":
			_, _ = w.Write([]byte(`{"publicId":"en-abc123","name":"production"}`))
		case strings.HasSuffix(r.URL.Path, "/jobs") && r.Method == http.MethodGet, strings.HasSuffix(r.URL.Path, "/backups") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/v1/domains/do-abc123/dns":
			_, _ = w.Write([]byte(`{"status":"ok","expected":{"type":"CNAME","host":"www","value":"x"},"actual":null}`))
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"publicId":"do-abc123","status":"pending"}`))
		default:
			_, _ = w.Write([]byte(`{"host":"db","port":3306}`))
		}
	}))
	defer server.Close()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	factory := testFactory(out)
	factory.IOStreams.ErrOut = errOut
	command := NewCmdRoot(factory)
	command.SetOut(out)
	command.SetErr(errOut)
	command.SetArgs(append([]string{"--host", server.URL}, args...))
	err = command.Execute()
	return out.String(), errOut.String(), paths, err
}

func hits(paths []string, fragment string) bool {
	for _, path := range paths {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}

func TestEnvironmentTargetsOfOtherGroups(t *testing.T) {
	cases := []struct {
		words, flags []string
		write        bool
		usage        string
	}{
		{words: []string{"jobs", "list"}},
		{words: []string{"backups", "list"}},
		{words: []string{"mysql", "get"}},
		{words: []string{"jobs", "create"}, flags: []string{"--name", "q", "--command", "php artisan queue:work", "--type", "worker"}, write: true},
		{words: []string{"backups", "create"}, write: true},
		{words: []string{"mysql", "rotate-password"}, flags: []string{"--confirm", "en-abc123"}, write: true},
	}
	for _, tc := range cases {
		name := strings.Join(tc.words, " ")
		t.Run(name, func(t *testing.T) {
			run := func(envVar string, env ...string) (string, string, []string, error) {
				return runTarget(t, envVar, append(append(append([]string{}, tc.words...), env...), tc.flags...)...)
			}
			_, stderr, paths, err := run("", "en-abc123")
			if err != nil || strings.Contains(stderr, "→") || !hits(paths, "/v1/environments/en-abc123/") {
				t.Fatalf("ID: %v, %q, %v", err, stderr, paths)
			}
			_, stderr, paths, err = run("", "production")
			if err != nil || !strings.Contains(stderr, rootViaLabel) || !hits(paths, "/v1/environments/en-abc123/") {
				t.Fatalf("label: %v, %q, %v", err, stderr, paths)
			}
			stdout, stderr, paths, err := run("en-abc123")
			if tc.write {
				want := "name the environment: frbit " + name + " <env-id|label> (the checkout resolves to en-abc123 via FORTRABBIT_ENVIRONMENT)"
				if err == nil || err.Error() != want || len(paths) != 0 {
					t.Fatalf("omitted: %v, %v", err, paths)
				}
				return
			}
			if err != nil || !strings.Contains(stderr, "→ en-abc123 (from FORTRABBIT_ENVIRONMENT)\n") || strings.Contains(stdout, "→") {
				t.Fatalf("implicit: %v, %q, %q", err, stdout, stderr)
			}
			stdout, stderr, _, err = run("en-abc123", "--json")
			if err != nil || strings.Contains(stdout, "→") || !strings.Contains(stderr, "→") {
				t.Fatalf("json: %v, %q, %q", err, stdout, stderr)
			}
		})
	}
}

func TestMysqlRotateConfirmComparesResolvedID(t *testing.T) {
	_, _, paths, err := runTarget(t, "", "mysql", "rotate-password", "production", "--confirm", "production")
	if err == nil || !strings.Contains(err.Error(), "--confirm must exactly match en-abc123") || hits(paths, "regenerate") {
		t.Fatalf("%v, %v", err, paths)
	}
}

func TestDomainsCreateTakesLabelAndNeedsEnvironment(t *testing.T) {
	_, stderr, paths, err := runTarget(t, "", "domains", "create", "production", "www.example.com")
	if err != nil || !strings.Contains(stderr, rootViaLabel) || !hits(paths, "POST /v1/domains") {
		t.Fatalf("label: %v, %q, %v", err, stderr, paths)
	}
	_, _, paths, err = runTarget(t, "en-abc123", "domains", "create", "www.example.com")
	want := "name the environment: frbit domains create <env-id|label> <name> (the checkout resolves to en-abc123 via FORTRABBIT_ENVIRONMENT)"
	if err == nil || err.Error() != want || len(paths) != 0 {
		t.Fatalf("omitted: %v, %v", err, paths)
	}
}

func TestDeploymentsListEnvironmentFilter(t *testing.T) {
	query := func(args ...string) (string, string, string) {
		t.Helper()
		var raw string
		dir := t.TempDir()
		_ = os.MkdirAll(filepath.Join(dir, ".fortrabbit"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, ".fortrabbit", "environments.yml"), []byte("production: en-abc123\nstaging: en-def456\n"), 0o644)
		t.Chdir(dir)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/deployments" {
				raw = r.URL.RawQuery
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`{"publicId":"en-abc123"}`))
		}))
		defer server.Close()
		t.Setenv("FRBIT_TOKEN", "test-token")
		out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
		factory := testFactory(out)
		command := NewCmdRoot(factory)
		command.SetOut(out)
		command.SetErr(errOut)
		command.SetArgs(append([]string{"--host", server.URL, "deployments", "list"}, args...))
		if err := command.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return raw, out.String(), errOut.String()
	}
	t.Setenv("FORTRABBIT_ENVIRONMENT", "")
	if raw, _, stderr := query("-e", "production", "--json"); raw != "environment=en-abc123&page=1" || !strings.Contains(stderr, rootViaLabel) {
		t.Fatalf("label: %q, %q", raw, stderr)
	}
	if raw, _, stderr := query("--environment", "en-zzz999"); raw != "environment=en-zzz999&page=1" || strings.Contains(stderr, "→") {
		t.Fatalf("ID: %q, %q", raw, stderr)
	}
	// Two entries and no branch: nothing resolves, so every deployment is listed.
	if raw, _, stderr := query(); raw != "page=1" || strings.Contains(stderr, "→") {
		t.Fatalf("none: %q, %q", raw, stderr)
	}
	t.Setenv("FORTRABBIT_ENVIRONMENT", "en-abc123")
	if raw, stdout, stderr := query("--json"); raw != "environment=en-abc123&page=1" || !strings.Contains(stderr, "→ en-abc123 (from FORTRABBIT_ENVIRONMENT)") || strings.Contains(stdout, "→") {
		t.Fatalf("implicit: %q, %q, %q", raw, stdout, stderr)
	}
}
