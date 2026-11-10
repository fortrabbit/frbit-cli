package root

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestJobsListShowsCountAndLimit(t *testing.T) {
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/environments/en-abc123/jobs" {
			t.Fatalf("unexpected %s", r.URL)
		}
		w.Header().Set("X-Jobs-Count", "1")
		w.Header().Set("X-Jobs-Limit", "3")
		_, _ = w.Write([]byte(`[{"publicId":"jo-abc123","name":"queue","type":"worker","command":"php artisan queue:work","isActive":true}]`))
	}, "jobs", "list", "en-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "jo-abc123") || !strings.Contains(output, "Jobs: 1 of 3") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestJobsCreateCustomCronChecksExpression(t *testing.T) {
	var calls []string
	var posted map[string]any
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/jobs/check-cron":
			if r.URL.Query().Get("expression") != "0 */2 * * *" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"expression":"0 */2 * * *","valid":true,"nextRuns":[]}`))
		case "POST /v1/environments/en-abc123/jobs":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"publicId":"jo-abc123"}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "jobs", "create", "en-abc123", "--name", "sync", "--type", "cron", "--command", "php artisan schedule:run", "--interval", "custom", "--expression", "0 */2 * * *", "--max-runtime", "300")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "GET /v1/jobs/check-cron,POST /v1/environments/en-abc123/jobs" || posted["expression"] != "0 */2 * * *" || posted["maxRuntime"] != float64(300) || posted["interval"] != "custom" {
		t.Fatalf("calls = %v, posted = %#v", calls, posted)
	}

	_, err = runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"expression":"nope","valid":false,"nextRuns":[]}`))
	}, "jobs", "create", "en-abc123", "--name", "sync", "--type", "cron", "--command", "php x", "--interval", "custom", "--expression", "nope", "--max-runtime", "300")
	if err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("err = %v", err)
	}
}

func TestJobsCreateWorkerRejectsCronFlags(t *testing.T) {
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") },
		"jobs", "create", "en-abc123", "--name", "q", "--type", "worker", "--command", "php x", "--interval", "day")
	if err == nil || !strings.Contains(err.Error(), "only applies to --type cron") {
		t.Fatalf("err = %v", err)
	}
}

func TestJobsUpdateSendsOnlyChangedFlags(t *testing.T) {
	var posted map[string]any
	var method string
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&posted)
		_, _ = w.Write([]byte(`{"publicId":"jo-abc123"}`))
	}, "jobs", "update", "jo-abc123", "--inactive", "--exit-timeout", "10")
	if err != nil {
		t.Fatal(err)
	}
	if method != "PATCH /v1/jobs/jo-abc123" || len(posted) != 2 || posted["isActive"] != false || posted["exitTimeout"] != float64(10) {
		t.Fatalf("%s %#v", method, posted)
	}
}

func TestJobsDeleteNeedsConfirm(t *testing.T) {
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "jobs", "delete", "jo-abc123")
	if err == nil || !strings.Contains(err.Error(), "--confirm jo-abc123") {
		t.Fatalf("err = %v", err)
	}
	var deleted bool
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"publicId":"jo-abc123","name":"queue"}`))
	}, "jobs", "delete", "jo-abc123", "--confirm", "jo-abc123")
	if err != nil || !deleted || !strings.Contains(output, "Deleted job jo-abc123.") {
		t.Fatalf("err = %v, deleted = %v\n%s", err, deleted, output)
	}
}
