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

func TestJobsCreateSendsWhatWasTyped(t *testing.T) {
	var calls []string
	var posted map[string]any
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"publicId":"jo-abc123"}`))
	}, "jobs", "create", "en-abc123", "--name", "sync", "--type", "cron", "--command", "php artisan schedule:run", "--interval", "custom", "--expression", "0 */2 * * *", "--max-runtime", "301")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "POST /v1/environments/en-abc123/jobs" || posted["expression"] != "0 */2 * * *" || posted["maxRuntime"] != float64(301) || posted["interval"] != "custom" || len(posted) != 6 {
		t.Fatalf("calls = %v, posted = %#v", calls, posted)
	}
}

func TestJobsListFooterWithoutHeaders(t *testing.T) {
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }, "jobs", "list", "en-abc123")
	if err != nil || !strings.Contains(output, "Jobs: 0 of ?") {
		t.Fatalf("err = %v\n%s", err, output)
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
	_, err = runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "jobs", "delete", "jo-abc123", "--confirm", "jo-other")
	if err == nil || !strings.Contains(err.Error(), "must exactly match") {
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

// pagedHandler serves path in two pages of one item each; only the first page carries extra headers.
func pagedHandler(t *testing.T, path string, first, second string, headers map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Fatalf("unexpected %s", r.URL)
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[` + second + `]`))
			return
		}
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.Header().Set("Link", `<`+path+`?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[` + first + `]`))
	}
}

func TestJobsAndBackupsListFollowPages(t *testing.T) {
	jobs := pagedHandler(t, "/v1/environments/en-abc123/jobs", `{"publicId":"jo-aaa111","name":"a"}`, `{"publicId":"jo-bbb222","name":"b"}`, map[string]string{"X-Jobs-Count": "2", "X-Jobs-Limit": "3"})
	backups := pagedHandler(t, "/v1/environments/en-abc123/backups", `{"publicId":"ba-aaa111"}`, `{"publicId":"ba-bbb222"}`, nil)
	for _, test := range []struct {
		handler      http.HandlerFunc
		args         []string
		want, absent []string
	}{
		{jobs, []string{"jobs", "list", "en-abc123"}, []string{"jo-aaa111", "Jobs: 2 of 3"}, []string{"jo-bbb222"}},
		{jobs, []string{"jobs", "list", "en-abc123", "--all"}, []string{"jo-aaa111", "jo-bbb222", "Jobs: 2 of 3"}, nil},
		{jobs, []string{"jobs", "list", "en-abc123", "--limit", "1"}, []string{"jo-aaa111", "Jobs: 2 of 3"}, []string{"jo-bbb222"}},
		{backups, []string{"backups", "list", "en-abc123", "--all"}, []string{"ba-aaa111", "ba-bbb222"}, nil},
		{backups, []string{"backups", "list", "en-abc123", "--limit", "2"}, []string{"ba-aaa111", "ba-bbb222"}, nil},
	} {
		output, err := runDomains(t, test.handler, test.args...)
		if err != nil {
			t.Fatalf("%v: %v", test.args, err)
		}
		for _, want := range test.want {
			if !strings.Contains(output, want) {
				t.Fatalf("%v: output lacks %q:\n%s", test.args, want, output)
			}
		}
		for _, absent := range test.absent {
			if strings.Contains(output, absent) {
				t.Fatalf("%v: output has %q:\n%s", test.args, absent, output)
			}
		}
	}
	for _, args := range [][]string{{"backups", "list", "en-abc123", "--all", "--limit", "1"}, {"jobs", "list", "en-abc123", "--limit", "0"}} {
		if _, err := runDomains(t, backups, args...); err == nil {
			t.Fatalf("%v: no error", args)
		}
	}
}
