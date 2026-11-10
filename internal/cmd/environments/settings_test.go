package environments

import (
	"bytes"
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
	method, path string
	body         map[string]any
}

func run(t *testing.T, stdin string, handler func(r request, w http.ResponseWriter), args ...string) (string, []request, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	var seen []request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		req := request{r.Method, r.URL.Path, body}
		seen = append(seen, req)
		handler(req, w)
	}))
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(stdin), Out: output, ErrOut: &bytes.Buffer{}},
		ConfigStore:     configStore{},
		CredentialStore: credentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(output)
	root.SetErr(&bytes.Buffer{})
	root.AddCommand(NewCmdEnvironments(factory))
	root.SetArgs(append([]string{"--host", server.URL, "environments"}, args...))
	err := root.Execute()
	return output.String(), seen, err
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

	if _, _, err := run(t, "", func(request, http.ResponseWriter) {}, "basic-auth", "update", "en-a1b2c3", "--password-stdin"); err == nil {
		t.Fatal("empty stdin must fail")
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
	if _, _, err := run(t, "", func(request, http.ResponseWriter) {}, "firewall-rules", "add", "en-a1b2c3", "--description", "x", "--port", "web"); err == nil {
		t.Fatal("bad port must fail")
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

func componentsHandler(r request, w http.ResponseWriter) {
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

func TestComponentPriceSummaryTotals(t *testing.T) {
	var plans []map[string]any
	_ = json.Unmarshal([]byte(catalog), &plans)
	resources := make([]api.Resource, 0, len(plans))
	for _, plan := range plans {
		resources = append(resources, plan)
	}
	summary, err := componentPriceSummary(resources, map[string]string{"php": "md", "database": "sm"})
	if err != nil || !strings.Contains(summary, "32.00 EUR/month") {
		t.Fatalf("summary = %q, err = %v", summary, err)
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
