package root

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// executeOnTTY runs Execute on a terminal with stdin and returns the exit code, stdout and stderr.
func executeOnTTY(t *testing.T, handler http.Handler, stdin string, args ...string) (int, string, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	factory := testFactory(out)
	factory.IOStreams.ErrOut = errOut
	factory.IOStreams.IsTTY = true
	factory.IOStreams.In = strings.NewReader(stdin)
	code := Execute(factory, append([]string{"--host", server.URL}, args...))
	return code, out.String(), errOut.String()
}

func assertAborted(t *testing.T, name string, code int, stdout, stderr string) {
	t.Helper()
	if code != 1 || !strings.HasSuffix(stderr, "Aborted.\n") || strings.Contains(stderr, "error:") || stdout != "" {
		t.Fatalf("%s: exit %d, stdout %q, stderr %q", name, code, stdout, stderr)
	}
}

func TestDeclinedDestructiveConfirmationExitsOne(t *testing.T) {
	deleted := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
		}
		_, _ = w.Write([]byte(`{"publicId":"tm-abc123"}`))
	})
	for name, answer := range map[string]string{"no": "no\n", "empty": "\n", "wrong ID": "tm-other1\n"} {
		code, stdout, stderr := executeOnTTY(t, handler, answer, "teams", "delete", "tm-abc123")
		assertAborted(t, name, code, stdout, stderr)
	}
	if deleted {
		t.Fatal("deleted after a decline")
	}
}

func TestDeclinedBilledCreateExitsOne(t *testing.T) {
	var calls []dryRunCall
	server := billingServer(t, billedPricing, &calls)
	code, stdout, stderr := executeOnTTY(t, server.Config.Handler, "n\n", "apps", "create", "--name", "store", "--region", "eu-w1a")
	assertAborted(t, "billed", code, stdout, stderr)
	if len(calls) != 1 || !calls[0].dryRun {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestDeclinedAppsUpdateConfirmationExitsOne(t *testing.T) {
	code, stdout, stderr := executeOnTTY(t, http.NotFoundHandler(), "no\n", "apps", "update", "ap-abc123", "--disconnect-repository")
	assertAborted(t, "apps update", code, stdout, stderr)
}

func TestRefusalWithoutTerminalStillPrintsError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	factory := testFactory(out)
	factory.IOStreams.ErrOut = errOut
	code := Execute(factory, []string{"--host", server.URL, "teams", "delete", "tm-abc123"})
	if code != 1 || !strings.Contains(errOut.String(), "error: ") || !strings.Contains(errOut.String(), "--confirm tm-abc123") {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
}
