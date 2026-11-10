package root

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runDomains(t *testing.T, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	output := &bytes.Buffer{}
	factory := testFactory(output)
	command := NewCmdRoot(factory)
	command.SetArgs(append([]string{"--host", server.URL}, args...))
	err := command.Execute()
	return output.String(), err
}

func TestDomainsCreatePrintsDNSRecords(t *testing.T) {
	var posted map[string]any
	var key string
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/domains":
			key = r.Header.Get("Idempotency-Key")
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"publicId":"do-abc123","name":"www.example.com","type":"subdomain","environment":"en-abc123","isMain":true}`))
		case "GET /v1/domains/do-abc123/dns":
			_, _ = w.Write([]byte(`{"status":"incorrect","expected":{"type":"CNAME","host":"www","value":"en-abc123.eu-w1a.frbit.app"},"actual":null}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "domains", "create", "en-abc123", "www.example.com", "--main")
	if err != nil {
		t.Fatal(err)
	}
	if posted["environment"] != "en-abc123" || posted["name"] != "www.example.com" || posted["type"] != "external" || posted["isMain"] != true || key == "" {
		t.Fatalf("payload = %#v, key = %q", posted, key)
	}
	for _, want := range []string{"do-abc123", "expected", "CNAME", "www", "en-abc123.eu-w1a.frbit.app", "Verification: incorrect"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestDomainsDNSRefreshPostsBeforeGet(t *testing.T) {
	var calls []string
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"status":"correct","expected":{"type":"A","host":"@","value":"1.2.3.4"},"actual":{"type":"A","host":"@","value":"1.2.3.4"}}`))
	}, "domains", "dns", "do-abc123", "--refresh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "POST /v1/domains/do-abc123/dns/refresh,GET /v1/domains/do-abc123/dns" || !strings.Contains(output, "Verification: correct") {
		t.Fatalf("calls = %v\n%s", calls, output)
	}
}
