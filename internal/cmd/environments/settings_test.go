package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

type configStore struct{}

func (configStore) Load() (config.Config, error) { return config.Config{}, nil }
func (configStore) Save(config.Config) error     { return nil }

type credentialStore struct{}

func (credentialStore) Get(string) (credentials.Credential, error) {
	return credentials.Credential{}, credentials.ErrNotFound
}
func (credentialStore) Set(string, credentials.Credential) error { return nil }
func (credentialStore) Delete(string) error                      { return nil }

type request struct {
	method, path, query string
	body                map[string]any
}

func run(t *testing.T, stdin string, handler func(r request, w http.ResponseWriter), args ...string) (string, []request, error) {
	t.Helper()
	return runContext(t, context.Background(), stdin, handler, args...)
}

func runContext(t *testing.T, ctx context.Context, stdin string, handler func(r request, w http.ResponseWriter), args ...string) (string, []request, error) {
	t.Helper()
	out, _, seen, err := runStreams(t, ctx, stdin, handler, args...)
	return out, seen, err
}

// runStreams returns stdout and stderr separately.
func runStreams(t *testing.T, ctx context.Context, stdin string, handler func(r request, w http.ResponseWriter), args ...string) (string, string, []request, error) {
	t.Helper()
	return runStreamsWith(t, ctx, false, stdin, handler, args...)
}

// runStreamsTTY is runStreams on a terminal when tty is set.
func runStreamsTTY(t *testing.T, tty bool, stdin string, handler func(r request, w http.ResponseWriter), args ...string) (string, string, []request, error) {
	t.Helper()
	return runStreamsWith(t, context.Background(), tty, stdin, handler, args...)
}

func runStreamsWith(t *testing.T, ctx context.Context, tty bool, stdin string, handler func(r request, w http.ResponseWriter), args ...string) (string, string, []request, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	var seen []request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		req := request{r.Method, r.URL.Path, r.URL.RawQuery, body}
		seen = append(seen, req)
		handler(req, w)
	}))
	t.Cleanup(server.Close)
	output, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(stdin), Out: output, ErrOut: stderr, IsTTY: tty},
		ConfigStore:     configStore{},
		CredentialStore: credentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(output)
	root.SetErr(stderr)
	root.AddCommand(NewCmdEnvironments(factory))
	root.SetArgs(append([]string{"--host", server.URL, "environments"}, args...))
	err := root.ExecuteContext(ctx)
	return output.String(), stderr.String(), seen, err
}

func echo(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func TestBasicAuthPasswordOnlyFromStdin(t *testing.T) {
	_, _, err := run(t, "", func(request, http.ResponseWriter) {}, "basic-auth", "update", "en-a1b2c3", "--password", "secret")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--password must not exist, err = %v", err)
	}

	out, seen, err := run(t, "s3cret\n", func(r request, w http.ResponseWriter) {
		echo(w, 200, `{"enabled":true,"username":"preview"}`)
	}, "basic-auth", "update", "en-a1b2c3", "--enabled", "--username", "preview", "--password-stdin")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].method != "PUT" || seen[0].path != "/v1/environments/en-a1b2c3/basic-auth" {
		t.Fatalf("requests = %+v", seen)
	}
	body := seen[0].body
	if body["password"] != "s3cret" || body["username"] != "preview" || body["enabled"] != true {
		t.Fatalf("body = %v", body)
	}
	if strings.Contains(out, "s3cret") {
		t.Fatalf("password printed: %q", out)
	}

	_, seen, err = run(t, "", func(request, http.ResponseWriter) {}, "basic-auth", "update", "en-a1b2c3", "--password-stdin")
	if err == nil || !strings.Contains(err.Error(), "empty password") || len(seen) != 0 {
		t.Fatalf("empty stdin: err = %v, requests = %d", err, len(seen))
	}

	out, seen, err = run(t, "s3cret\n", basicAuthAPI(`{"enabled":true,"username":"preview"}`), "basic-auth", "update", "en-a1b2c3", "--password-stdin", "--json")
	if err != nil || strings.Contains(out, "s3cret") {
		t.Fatalf("json output = %q, err = %v", out, err)
	}
	if body := seen[len(seen)-1].body; body["enabled"] != true || body["username"] != "preview" || body["password"] != "s3cret" {
		t.Fatalf("rotate body = %v", body)
	}
}

// basicAuthAPI answers GET with current and rejects a PUT without enabled and username, as the API does.
func basicAuthAPI(current string) func(r request, w http.ResponseWriter) {
	return func(r request, w http.ResponseWriter) {
		if r.method == "GET" {
			echo(w, 200, current)
			return
		}
		_, hasEnabled := r.body["enabled"].(bool)
		username, _ := r.body["username"].(string)
		if !hasEnabled || username == "" {
			echo(w, 422, `{"title":"Unprocessable Entity","detail":"enabled and username are required"}`)
			return
		}
		echo(w, 200, `{"enabled":true,"username":"preview"}`)
	}
}

func TestBasicAuthUpdateSendsFullBody(t *testing.T) {
	_, seen, err := run(t, "", basicAuthAPI(`{"enabled":true,"username":"preview"}`), "basic-auth", "update", "en-a1b2c3", "--enabled=false")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0].method != "GET" || seen[1].method != "PUT" {
		t.Fatalf("requests = %+v", seen)
	}
	if body := seen[1].body; body["enabled"] != false || body["username"] != "preview" || len(body) != 2 {
		t.Fatalf("body = %v", body)
	}

	// Both fields given: no GET needed.
	_, seen, err = run(t, "", basicAuthAPI(`{}`), "basic-auth", "update", "en-a1b2c3", "--enabled", "--username", "x")
	if err != nil || len(seen) != 1 || seen[0].method != "PUT" {
		t.Fatalf("err = %v, requests = %+v", err, seen)
	}

	// Never set up: no username to keep.
	_, seen, err = run(t, "", basicAuthAPI(`{"enabled":false,"username":null}`), "basic-auth", "update", "en-a1b2c3", "--enabled")
	if err == nil || !strings.Contains(err.Error(), "--username") || len(seen) != 1 {
		t.Fatalf("err = %v, requests = %+v", err, seen)
	}
}

func TestFirewallRulePortRange(t *testing.T) {
	cases := map[string]struct {
		start float64
		end   any
	}{"8080-8090": {8080, float64(8090)}, "8080": {8080, nil}}
	for port, want := range cases {
		_, seen, err := run(t, "", func(r request, w http.ResponseWriter) {
			echo(w, 201, `{"publicId":"fr-a1b2c3","description":"x","portStart":8080,"portEnd":8080}`)
		}, "firewall-rules", "add", "en-a1b2c3", "--description", "x", "--port", port)
		if err != nil {
			t.Fatal(err)
		}
		body := seen[0].body
		end, hasEnd := body["portEnd"]
		if seen[0].path != "/v1/environments/en-a1b2c3/firewall-rules" || body["portStart"] != want.start || !hasEnd || end != want.end {
			t.Fatalf("%s: body = %v", port, body)
		}
	}
	for _, bad := range []string{"web", "9000-8000", "0", "70000", "65535-65540", "1000-2000"} {
		_, seen, err := run(t, "", func(request, http.ResponseWriter) {}, "firewall-rules", "add", "en-a1b2c3", "--description", "x", "--port", bad)
		if err == nil || len(seen) != 0 {
			t.Fatalf("port %q: err = %v, requests = %d", bad, err, len(seen))
		}
	}
}

func TestFirewallRuleDeleteNeedsConfirm(t *testing.T) {
	_, seen, err := run(t, "", func(request, http.ResponseWriter) {}, "firewall-rules", "delete", "fr-a1b2c3")
	if err == nil || !strings.Contains(err.Error(), "--confirm fr-a1b2c3") || len(seen) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(seen))
	}
	_, seen, err = run(t, "", func(r request, w http.ResponseWriter) { w.WriteHeader(204) }, "firewall-rules", "delete", "fr-a1b2c3", "--confirm", "fr-a1b2c3")
	if err != nil || len(seen) != 1 || seen[0].method != "DELETE" || seen[0].path != "/v1/firewall-rules/fr-a1b2c3" {
		t.Fatalf("err = %v, requests = %+v", err, seen)
	}
}

const catalog = `[{"slug":"php","optional":false,"sizes":[{"size":"sm","priceInCents":1200,"currency":"EUR"},{"size":"md","priceInCents":2400,"currency":"EUR"}]},{"slug":"database","optional":true,"sizes":[{"size":"off","priceInCents":null,"currency":"EUR"},{"size":"sm","priceInCents":800,"currency":"EUR"}]}]`

const current = `{"components":[{"slug":"php","size":"sm","priceInCents":1200,"currency":"EUR"},{"slug":"database","size":"sm","priceInCents":800,"currency":"EUR"}]}`

func componentsHandler(r request, w http.ResponseWriter) {
	if r.method == "GET" && r.path == "/v1/environments/en-a1b2c3/components" {
		echo(w, 200, current)
		return
	}
	if r.path == "/v1/apps" {
		echo(w, 200, `[{"publicId":"ap-1","region":"eu-w1a","environments":["en-zzz","en-a1b2c3"]}]`)
		return
	}
	if r.path == "/v1/catalog/component-plans" {
		echo(w, 200, catalog)
		return
	}
	echo(w, 202, `{"components":[{"slug":"php","size":"md","priceInCents":2400,"currency":"EUR"}],"pricing":{"monthlyPriceInCents":2400,"currency":"EUR"}}`)
}

func TestComponentsUpdateWithoutTTYRequiresYes(t *testing.T) {
	_, seen, err := run(t, "", componentsHandler, "components", "update", "en-a1b2c3", "--component", "php=md")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	for _, r := range seen {
		if r.method == "PATCH" {
			t.Fatal("PATCH sent without consent")
		}
	}

	_, seen, err = run(t, "", componentsHandler, "components", "update", "en-a1b2c3", "--component", "php=md", "--component", "database=off", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	last := seen[len(seen)-1]
	if last.method != "PATCH" || last.body["php"] != "md" || last.body["database"] != "off" {
		t.Fatalf("last = %+v", last)
	}

	_, seen, err = run(t, "", func(r request, w http.ResponseWriter) {
		if r.path == "/v1/catalog/component-plans" {
			echo(w, 500, `{"title":"boom"}`)
			return
		}
		componentsHandler(r, w)
	}, "components", "update", "en-a1b2c3", "--component", "php=md", "--yes")
	if err == nil {
		t.Fatal("catalog failure must fail")
	}
	for _, r := range seen {
		if r.method == "PATCH" {
			t.Fatal("PATCH sent after catalog failure")
		}
	}

	_, seen, err = run(t, "", componentsHandler, "components", "update", "en-a1b2c3", "--component", "php=xl", "--yes")
	if err == nil || !strings.Contains(err.Error(), "no size") {
		t.Fatalf("err = %v", err)
	}
	for _, r := range seen {
		if r.method == "PATCH" {
			t.Fatal("PATCH sent for unknown size")
		}
	}
}

func TestComponentChangeSummaryShowsDelta(t *testing.T) {
	var plans []api.Resource
	_ = json.Unmarshal([]byte(catalog), &plans)
	var currentResource api.Resource
	_ = json.Unmarshal([]byte(current), &currentResource)
	summary, err := componentChangeSummary(currentResource, plans, map[string]string{"php": "md", "database": "off"})
	for _, want := range []string{"php: sm (12.00 EUR) -> md (24.00 EUR)", "database: sm (8.00 EUR) -> off (0.00 EUR", "Monthly change: +4.00 EUR"} {
		if err != nil || !strings.Contains(summary, want) {
			t.Fatalf("summary = %q, err = %v, want %q", summary, err, want)
		}
	}
	if strings.Contains(summary, "Total") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestPhpSettingsNumericCoercionIsExact(t *testing.T) {
	payload, err := phpSettingsPayload([]string{"a=007", "b=60", "c=+5"}, nil)
	settings, _ := payload["settings"].(map[string]any)
	if err != nil || settings["a"] != "007" || settings["b"] != 60 || settings["c"] != "+5" {
		t.Fatalf("settings = %v, err = %v", settings, err)
	}
}

func TestPhpSettingsSetParsesKeyValue(t *testing.T) {
	_, seen, err := run(t, "", func(r request, w http.ResponseWriter) {
		echo(w, 200, `{"phpVersion":"8.4","settings":{"max_execution_time":120},"extensions":{"apcu":true}}`)
	}, "php-settings", "update", "en-a1b2c3", "--set", "max_execution_time=120", "--set", "upload_max_filesize=256M", "--extension", "apcu=on", "--extension", "newrelic=off")
	if err != nil {
		t.Fatal(err)
	}
	if seen[0].method != "PATCH" || seen[0].path != "/v1/environments/en-a1b2c3/php-settings" {
		t.Fatalf("request = %+v", seen[0])
	}
	settings, _ := seen[0].body["settings"].(map[string]any)
	extensions, _ := seen[0].body["extensions"].(map[string]any)
	if settings["max_execution_time"] != float64(120) || settings["upload_max_filesize"] != "256M" || extensions["apcu"] != true || extensions["newrelic"] != false {
		t.Fatalf("body = %v", seen[0].body)
	}
	if _, _, err := run(t, "", func(request, http.ResponseWriter) {}, "php-settings", "update", "en-a1b2c3", "--extension", "apcu=maybe"); err == nil {
		t.Fatal("bad extension value must fail")
	}
}

func TestEnvironmentUpdateVersions(t *testing.T) {
	_, seen, err := run(t, "", func(r request, w http.ResponseWriter) { echo(w, 200, `{"publicId":"en-a1b2c3"}`) },
		"update", "en-a1b2c3", "--software-version", "11", "--php-version", "8.4")
	if err != nil || seen[0].body["softwareVersion"] != "11" || seen[0].body["phpVersion"] != "8.4" {
		t.Fatalf("err = %v, seen = %+v", err, seen)
	}
}

// changedAnything reports a request other than a read.
func changedAnything(seen []request) bool {
	for _, r := range seen {
		if r.method != "GET" {
			return true
		}
	}
	return false
}

func patched(seen []request) bool {
	for _, r := range seen {
		if r.method == "PATCH" {
			return true
		}
	}
	return false
}

func TestComponentsUpdateSkipsUnchanged(t *testing.T) {
	out, stderr, seen, err := runStreams(t, context.Background(), "", componentsHandler, "components", "update", "en-a1b2c3", "--component", "php=sm")
	if err != nil || out != "" || !strings.Contains(stderr, "No component plan changes.") || patched(seen) {
		t.Fatalf("out = %q, stderr = %q, err = %v, seen = %+v", out, stderr, err, seen)
	}
}

func TestComponentsUpdateResolvesRegion(t *testing.T) {
	var catalogQuery string
	handler := func(r request, w http.ResponseWriter) {
		if r.path == "/v1/catalog/component-plans" {
			catalogQuery = r.query
		}
		componentsHandler(r, w)
	}
	if _, seen, err := run(t, "", handler, "components", "update", "en-a1b2c3", "--component", "php=md", "--yes"); err != nil || !patched(seen) || !strings.Contains(catalogQuery, "region=eu-w1a") || !strings.Contains(catalogQuery, "currency=EUR") {
		t.Fatalf("err = %v, query = %q", err, catalogQuery)
	}

	_, seen, err := run(t, "", handler, "components", "update", "en-a1b2c3", "--component", "php=md", "--yes", "--region", "us-e1a")
	if err != nil || !patched(seen) || !strings.Contains(catalogQuery, "region=us-e1a") {
		t.Fatalf("override: err = %v, query = %q", err, catalogQuery)
	}
	for _, r := range seen {
		if r.path == "/v1/apps" {
			t.Fatal("--region must skip the app lookup")
		}
	}

	_, seen, err = run(t, "", func(r request, w http.ResponseWriter) {
		if r.path == "/v1/apps" {
			echo(w, 200, `[{"publicId":"ap-1","region":"eu-w1a","environments":["en-other"]}]`)
			return
		}
		componentsHandler(r, w)
	}, "components", "update", "en-a1b2c3", "--component", "php=md", "--yes")
	if err == nil || !strings.Contains(err.Error(), "pass --region") || patched(seen) {
		t.Fatalf("no match: err = %v", err)
	}
}

func TestComponentChangeSummaryRejectsCurrencyMismatch(t *testing.T) {
	var plans []api.Resource
	_ = json.Unmarshal([]byte(strings.ReplaceAll(catalog, "EUR", "USD")), &plans)
	var currentResource api.Resource
	_ = json.Unmarshal([]byte(current), &currentResource)
	if _, err := componentChangeSummary(currentResource, plans, map[string]string{"php": "md"}); err == nil || !strings.Contains(err.Error(), "USD") {
		t.Fatalf("err = %v", err)
	}
}

func TestComponentChangeSummaryMissingPriceFails(t *testing.T) {
	var plans []api.Resource
	_ = json.Unmarshal([]byte(`[{"slug":"php","sizes":[{"size":"sm","priceInCents":1200,"currency":"EUR"},{"size":"xl","priceInCents":null,"currency":"EUR"}]}]`), &plans)
	var currentResource api.Resource
	_ = json.Unmarshal([]byte(current), &currentResource)
	_, err := componentChangeSummary(currentResource, plans, map[string]string{"php": "xl"})
	if err == nil || err.Error() != "price for php xl unknown; check frbit plans list or change it in the dashboard" {
		t.Fatalf("err = %v", err)
	}
	_ = json.Unmarshal([]byte(`{"components":[{"slug":"php","size":"md","priceInCents":null,"currency":"EUR"}]}`), &currentResource)
	if _, err := componentChangeSummary(currentResource, plans, map[string]string{"php": "sm"}); err == nil || err.Error() != "price for php md unknown; check frbit plans list or change it in the dashboard" {
		t.Fatalf("booked: %v", err)
	}
}

func TestComponentsUpdateRefusesUnknownPrice(t *testing.T) {
	handler := func(r request, w http.ResponseWriter) {
		if r.path == "/v1/catalog/component-plans" {
			echo(w, 200, `[{"slug":"php","optional":false,"sizes":[{"size":"sm","priceInCents":1200,"currency":"EUR"},{"size":"md","priceInCents":null,"currency":"EUR"}]}]`)
			return
		}
		componentsHandler(r, w)
	}
	for name, tty := range map[string]bool{"--yes": false, "terminal": true} {
		args := []string{"components", "update", "en-a1b2c3", "--component", "php=md"}
		if !tty {
			args = append(args, "--yes")
		}
		_, stderr, seen, err := runStreamsTTY(t, tty, "y\n", handler, args...)
		if err == nil || err.Error() != "price for php md unknown; check frbit plans list or change it in the dashboard" || changedAnything(seen) || strings.Contains(stderr, "[y/N]") {
			t.Fatalf("%s: err %v, stderr %q, seen %+v", name, err, stderr, seen)
		}
	}
}
