package root

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMysqlGetHidesPassword(t *testing.T) {
	body := `{"host":"db.eu-w1a.frbit.app","port":3306,"user":"en-abc123","database":"en-abc123","password":"x","dsn":"mysql://u:s3cret@h"}`
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments/en-abc123/mysql" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(body))
	}, "mysql", "get", "en-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "db.eu-w1a.frbit.app") || !strings.Contains(output, "3306") || strings.Contains(output, "s3cret") || strings.Contains(output, "password") || strings.Contains(output, "dsn") || strings.Contains(output, "mysql://") {
		t.Fatalf("output:\n%s", output)
	}
	raw, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }, "mysql", "get", "en-abc123", "--json")
	if err != nil || !strings.Contains(raw, "s3cret") {
		t.Fatalf("json output %q, err %v", raw, err)
	}
}

func TestRotatePasswordWithoutTTYRequiresConfirm(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/environments/en-abc123/mysql/regenerate-password" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	run := func(args ...string) (string, error) {
		output := &bytes.Buffer{}
		command := NewCmdRoot(testFactory(output))
		command.SetArgs(append([]string{"--host", server.URL, "mysql", "rotate-password", "en-abc123"}, args...))
		err := command.Execute()
		return output.String(), err
	}
	if _, err := run(); err == nil || !strings.Contains(err.Error(), "--confirm en-abc123") || requests != 0 {
		t.Fatalf("err = %v, requests = %d", err, requests)
	}
	if _, err := run("--confirm", "en-wrong1"); err == nil || requests != 0 {
		t.Fatalf("err = %v, requests = %d", err, requests)
	}
	output, err := run("--confirm", "en-abc123")
	if err != nil || requests != 1 || !strings.Contains(output, "Rotating the MySQL password") {
		t.Fatalf("err = %v, requests = %d, output = %q", err, requests, output)
	}
}
