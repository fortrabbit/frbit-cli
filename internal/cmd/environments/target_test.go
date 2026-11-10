package environments

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const viaLabel = "→ en-a1b2c3 (production, from .fortrabbit/environments.yml)\n"
const viaEnvVar = "→ en-a1b2c3 (from FORTRABBIT_ENVIRONMENT)\n"

// targetHandler answers the environment lookup and any other request with a small valid body.
func targetHandler(next func(r request, w http.ResponseWriter)) func(r request, w http.ResponseWriter) {
	return func(r request, w http.ResponseWriter) {
		switch {
		case r.method == "GET" && r.path == "/v1/environments/en-a1b2c3":
			echo(w, 200, `{"publicId":"en-a1b2c3","name":"production"}`)
		case next != nil:
			next(r, w)
		case strings.HasSuffix(r.path, "/firewall-rules") && r.method == "GET", strings.HasSuffix(r.path, "/logs"):
			echo(w, 200, `[]`)
		case strings.HasSuffix(r.path, "/basic-auth") && r.method == "GET":
			echo(w, 200, `{"enabled":true,"username":"preview"}`)
		default:
			echo(w, 200, `{"publicId":"x"}`)
		}
	}
}

// inCheckout runs in a temp folder outside git whose link file maps production to en-a1b2c3.
func inCheckout(t *testing.T, envVar string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".fortrabbit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit", "environments.yml"), []byte("production: en-a1b2c3\nstaging: en-other1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("FORTRABBIT_ENVIRONMENT", envVar)
}

type targetCase struct {
	args    []string // command words; the environment goes right after them
	flags   []string
	write   bool
	stdin   string
	handler func(r request, w http.ResponseWriter)
}

var targetCases = []targetCase{
	{args: []string{"get"}},
	{args: []string{"logs"}},
	{args: []string{"metrics"}},
	{args: []string{"variables", "get"}},
	{args: []string{"php-settings", "get"}},
	{args: []string{"basic-auth", "get"}},
	{args: []string{"firewall-rules", "list"}},
	{args: []string{"components", "get"}, handler: componentsHandler},
	{args: []string{"update"}, flags: []string{"--name", "x"}, write: true},
	{args: []string{"variables", "update"}, flags: []string{"--set", "A=1"}, write: true},
	{args: []string{"restart"}, write: true},
	{args: []string{"deploy"}, write: true},
	{args: []string{"php-settings", "update"}, flags: []string{"--set", "memory_limit=256M"}, write: true},
	{args: []string{"basic-auth", "update"}, flags: []string{"--enabled=false"}, write: true},
	{args: []string{"firewall-rules", "add"}, flags: []string{"--description", "x", "--port", "8080"}, write: true},
	{args: []string{"components", "update"}, flags: []string{"--component", "php=md", "--yes"}, write: true, handler: componentsHandler},
}

func hitsEnvironment(seen []request) bool {
	for _, r := range seen {
		if strings.HasPrefix(r.path, "/v1/environments/en-a1b2c3/") || (r.path == "/v1/environments/en-a1b2c3" && r.method != "GET") {
			return true
		}
	}
	return false
}

func TestEnvironmentArgumentPaths(t *testing.T) {
	for _, tc := range targetCases {
		name := strings.Join(tc.args, " ")
		t.Run(name, func(t *testing.T) {
			handler := targetHandler(tc.handler)
			run := func(envArg []string) (string, string, []request, error) {
				args := append(append(append([]string{}, tc.args...), envArg...), tc.flags...)
				return runStreams(t, context.Background(), tc.stdin, handler, args...)
			}
			inCheckout(t, "")
			// ID: no line.
			_, stderr, seen, err := run([]string{"en-a1b2c3"})
			if err != nil || strings.Contains(stderr, "→") || (name != "get" && !hitsEnvironment(seen)) {
				t.Fatalf("ID: %v, stderr %q, seen %+v", err, stderr, seen)
			}
			// Label: resolved and announced on stderr.
			_, stderr, seen, err = run([]string{"production"})
			if err != nil || !strings.Contains(stderr, viaLabel) || (name != "get" && !hitsEnvironment(seen)) {
				t.Fatalf("label: %v, stderr %q", err, stderr)
			}
			inCheckout(t, "en-a1b2c3")
			stdout, stderr, seen, err := run(nil)
			if tc.write {
				want := "name the environment: frbit environments " + name + " <env-id|label>"
				if err == nil || err.Error() != want || len(seen) != 0 {
					t.Fatalf("omitted: %v, seen %+v", err, seen)
				}
				return
			}
			if err != nil || !strings.Contains(stderr, viaEnvVar) || strings.Contains(stdout, "→") {
				t.Fatalf("implicit: %v, stdout %q, stderr %q", err, stdout, stderr)
			}
		})
	}
}

func TestEnvironmentReadJSONKeepsStdoutClean(t *testing.T) {
	inCheckout(t, "")
	for _, args := range [][]string{{"get", "production", "--json"}, {"metrics", "production", "--json"}, {"basic-auth", "get", "production", "--json"}} {
		stdout, stderr, _, err := runStreams(t, context.Background(), "", targetHandler(nil), args...)
		if err != nil || strings.Contains(stdout, "→") || !strings.Contains(stderr, viaLabel) {
			t.Fatalf("%v: %v, stdout %q, stderr %q", args, err, stdout, stderr)
		}
	}
}

func TestMetricsArguments(t *testing.T) {
	inCheckout(t, "en-a1b2c3")
	for args, wantPath := range map[string]string{
		"en-other1":          "/v1/environments/en-other1/metrics",
		"staging":            "/v1/environments/en-other1/metrics",
		"--metric cpu":       "/v1/environments/en-a1b2c3/metrics/cpu",
		"staging --metric x": "/v1/environments/en-other1/metrics/x",
		"production":         "/v1/environments/en-a1b2c3/metrics",
	} {
		handler := targetHandler(func(r request, w http.ResponseWriter) {
			if r.path == "/v1/environments/en-other1" {
				echo(w, 200, `{"publicId":"en-other1"}`)
				return
			}
			echo(w, 200, `{}`)
		})
		_, _, seen, err := runStreams(t, context.Background(), "", handler, append(append([]string{"metrics"}, strings.Fields(args)...), "--json")...)
		if err != nil || len(seen) == 0 || seen[len(seen)-1].path != wantPath {
			t.Fatalf("%s: %v, seen %+v", args, err, seen)
		}
	}
}

func TestEnvironmentFromConnectedRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", "git@github.com:acme/site.git"}} {
		if output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	t.Chdir(dir)
	t.Setenv("FORTRABBIT_ENVIRONMENT", "")
	handler := targetHandler(func(r request, w http.ResponseWriter) {
		switch r.path {
		case "/v1/apps":
			echo(w, 200, `[{"publicId":"ap-aaa111","gitRepository":"acme/site","environments":["en-a1b2c3"]}]`)
		case "/v1/environments":
			echo(w, 200, `[{"publicId":"en-a1b2c3","name":"production","git":{"branch":"main","directory":null}}]`)
		default:
			echo(w, 200, `{"publicId":"en-a1b2c3"}`)
		}
	})
	stdout, stderr, _, err := runStreams(t, context.Background(), "", handler, "get", "--json")
	if err != nil || !strings.Contains(stderr, "→ en-a1b2c3 (from the repository acme/site, branch main)\n") || strings.Contains(stdout, "→") {
		t.Fatalf("%v, stdout %q, stderr %q", err, stdout, stderr)
	}
	_, _, seen, err := runStreams(t, context.Background(), "", handler, "restart")
	if err == nil || err.Error() != "name the environment: frbit environments restart <env-id|label>" || len(seen) != 0 {
		t.Fatalf("restart: %v, seen %+v", err, seen)
	}
	_, _, seen, err = runStreams(t, context.Background(), "", handler, "restart", " ")
	if err == nil || !strings.HasPrefix(err.Error(), "name the environment") || len(seen) != 0 {
		t.Fatalf("blank: %v, seen %+v", err, seen)
	}
}
