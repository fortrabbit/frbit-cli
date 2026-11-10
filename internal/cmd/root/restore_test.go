package root

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
)

// restoreAPI serves a backup of en-abc123 (or one without environment) and records every request.
func restoreAPI(t *testing.T, backup string, calls *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/backups/ba-abc123":
			_, _ = w.Write([]byte(backup))
		case "GET /v1/environments/en-abc123":
			_, _ = w.Write([]byte(`{"publicId":"en-abc123","name":"production"}`))
		case "POST /v1/backups/ba-abc123/restore":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"publicId":"ba-abc123","status":"Pending"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

const restoreBackup = `{"publicId":"ba-abc123","environment":"en-abc123","status":"completed","createdAt":"2026-11-09T03:00:00+00:00"}`

func runRestore(t *testing.T, server *httptest.Server, tty bool, stdin string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	factory := testFactory(out)
	factory.IOStreams.ErrOut = errOut
	factory.IOStreams.IsTTY = tty
	factory.IOStreams.In = strings.NewReader(stdin)
	command := NewCmdRoot(factory)
	command.SetOut(out)
	command.SetErr(errOut)
	command.SetArgs(append([]string{"--host", server.URL, "backups", "restore", "ba-abc123"}, args...))
	err := command.Execute()
	return out.String(), errOut.String(), err
}

func posted(calls []string) bool {
	for _, call := range calls {
		if strings.HasPrefix(call, "POST") {
			return true
		}
	}
	return false
}

func TestBackupsRestoreNonInteractiveWithEnvironmentID(t *testing.T) {
	var calls []string
	stdout, _, err := runRestore(t, restoreAPI(t, restoreBackup, &calls), false, "", "--confirm", "en-abc123", "--json")
	if err != nil || !posted(calls) || strings.TrimSpace(stdout) != `{"publicId":"ba-abc123","status":"Pending"}` {
		t.Fatalf("err %v, calls %v, stdout %q", err, calls, stdout)
	}
}

func TestBackupsRestoreRefusesBackupIDAsConfirm(t *testing.T) {
	var calls []string
	_, _, err := runRestore(t, restoreAPI(t, restoreBackup, &calls), false, "", "--confirm", "ba-abc123")
	if err == nil || !strings.Contains(err.Error(), "--confirm takes the environment ID; pass --confirm en-abc123") || posted(calls) {
		t.Fatalf("err %v, calls %v", err, calls)
	}
}

func TestBackupsRestoreWithoutTTYOrConfirmSendsNothing(t *testing.T) {
	var calls []string
	_, _, err := runRestore(t, restoreAPI(t, restoreBackup, &calls), false, "")
	if err == nil || !strings.Contains(err.Error(), "pass --confirm en-abc123") || posted(calls) {
		t.Fatalf("err %v, calls %v", err, calls)
	}
}

func TestBackupsRestoreInteractive(t *testing.T) {
	var calls []string
	server := restoreAPI(t, restoreBackup, &calls)
	stdout, stderr, err := runRestore(t, server, true, "en-abc123\n")
	for _, want := range []string{"production (en-abc123)", "2026-11-09T03:00:00+00:00", "Type en-abc123 to confirm:"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q: %q", want, stderr)
		}
	}
	if err != nil || !posted(calls) || !strings.Contains(stdout, "Restore of ba-abc123 to en-abc123 requested.") {
		t.Fatalf("yes: err %v, calls %v, stdout %q", err, calls, stdout)
	}
	calls = nil
	_, stderr, err = runRestore(t, server, true, "no\n")
	if !errors.Is(err, cmdutil.ErrAborted) || posted(calls) || !strings.HasSuffix(stderr, "Aborted.\n") {
		t.Fatalf("no: err %v, calls %v, stderr %q", err, calls, stderr)
	}
}

func TestBackupsRestoreWithoutEnvironmentStops(t *testing.T) {
	var calls []string
	_, _, err := runRestore(t, restoreAPI(t, `{"publicId":"ba-abc123","environment":null}`, &calls), true, "en-abc123\n", "--confirm", "en-abc123")
	if err == nil || !strings.Contains(err.Error(), "names no environment") || posted(calls) {
		t.Fatalf("err %v, calls %v", err, calls)
	}
}
