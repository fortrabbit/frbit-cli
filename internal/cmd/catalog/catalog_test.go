package catalog

import (
	"bytes"
	"github.com/fortrabbit/frbit-cli/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

func run(t *testing.T, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: &bytes.Buffer{}},
		ConfigStore:     testutil.ConfigStore{},
		CredentialStore: testutil.CredentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(output)
	root.SetErr(&bytes.Buffer{})
	root.AddCommand(NewCommands(factory)...)
	root.SetArgs(append([]string{"--host", server.URL}, args...))
	err := root.Execute()
	return output.String(), err
}

func TestPlansListFiltersByComponent(t *testing.T) {
	var query string
	out, err := run(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.URL.Path != "/v1/catalog/component-plans" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"slug":"php","optional":false,"sizes":[]},{"slug":"storage","optional":false,"sizes":[]}]`))
	}, "plans", "list", "--component", "php", "--region", "eu", "--currency", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "php") || strings.Contains(out, "storage") || !strings.Contains(out, "Total: 1") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(query, "region=eu") || !strings.Contains(query, "currency=EUR") || strings.Contains(query, "component") {
		t.Fatalf("query = %q", query)
	}
}

func TestGitBranchesSplitsOwnerAndRepo(t *testing.T) {
	var path string
	out, err := run(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`[{"name":"main","protected":true}]`))
	}, "git", "branches", "acme/site")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/git-repositories/acme/site/branches" || !strings.Contains(out, "main") {
		t.Fatalf("path = %q, output = %q", path, out)
	}
}

func TestGitBranchesRejectsMissingSlash(t *testing.T) {
	_, err := run(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected request")
	}, "git", "branches", "site")
	if err == nil || !strings.Contains(err.Error(), "owner/repo") {
		t.Fatalf("err = %v", err)
	}
}

func TestCatalogCommandsListTheirEndpoints(t *testing.T) {
	cases := map[string]string{
		"regions list":        "/v1/catalog/regions",
		"presets list":        "/v1/catalog/software-presets",
		"software list":       "/v1/catalog/software",
		"php-versions list":   "/v1/catalog/php-versions",
		"php-extensions list": "/v1/catalog/php-extensions",
		"php-settings list":   "/v1/catalog/php-settings",
		"node-versions list":  "/v1/catalog/node-versions",
		"git repositories":    "/v1/git-repositories",
	}
	for command, want := range cases {
		var path string
		_, err := run(t, func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			_, _ = w.Write([]byte(`[]`))
		}, strings.Fields(command)...)
		if err != nil || path != want {
			t.Errorf("%s: path = %q, err = %v, want %s", command, path, err, want)
		}
	}
}

const twoPlans = `[{"slug":"php","optional":false,"sizes":[]},{"slug":"storage","optional":false,"sizes":[]}]`

func TestPlansComponentFilterAppliesBeforeLimit(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(twoPlans)) }
	out, err := run(t, handler, "plans", "list", "--component", "storage", "--limit", "1")
	if err != nil || !strings.Contains(out, "storage") || strings.Contains(out, "No plans") {
		t.Fatalf("table = %q, err = %v", out, err)
	}
	out, err = run(t, handler, "plans", "list", "--component", "storage", "--limit", "1", "--json")
	if err != nil || strings.TrimSpace(out) != `[{"optional":false,"sizes":[],"slug":"storage"}]` {
		t.Fatalf("json = %q, err = %v", out, err)
	}
}

func TestPlansRejectsEmptyComponent(t *testing.T) {
	_, err := run(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") }, "plans", "list", "--component", "")
	if err == nil || !strings.Contains(err.Error(), "--component must not be empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitBranchesRejectsDotSegments(t *testing.T) {
	for _, arg := range []string{"../x", "acme/..", "./x", "acme/."} {
		_, err := run(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") }, "git", "branches", arg)
		if err == nil {
			t.Errorf("%s accepted", arg)
		}
	}
}

func TestNoGetGroupsHaveNoGetSubcommand(t *testing.T) {
	for _, command := range NewCommands(&app.Factory{}) {
		for _, sub := range command.Commands() {
			if sub.Name() == "get" {
				t.Errorf("%s has a get subcommand", command.Name())
			}
		}
	}
}

func TestPlansListShowsSizesReadably(t *testing.T) {
	out, err := run(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"slug":"php","optional":false,"autoscales":true,"sizes":[{"size":"s","name":"Small","specs":{"cpu":1},"priceInCents":500,"currency":"EUR"},{"size":"m","name":null,"specs":{},"priceInCents":1000,"currency":"EUR"}]},{"slug":"backups","optional":true,"autoscales":false,"sizes":[{"size":"off","name":null,"specs":{},"priceInCents":null,"currency":"EUR"}]}]`))
	}, "plans", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "s 5.00 EUR, m 10.00 EUR") || !strings.Contains(out, "false       off\n") || strings.Contains(out, "{") {
		t.Fatalf("output = %q", out)
	}
}

func TestPHPVersionsListShowsSurcharge(t *testing.T) {
	out, err := run(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"version":"8.4","eol":null,"default":true,"surcharge":{"eur":0,"usd":0}},{"version":"7.4","eol":"2022-11-28","default":false,"surcharge":{"eur":1000,"usd":1100}}]`))
	}, "php-versions", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "10.00 EUR, 11.00 USD") || strings.Contains(out, "{") {
		t.Fatalf("output = %q", out)
	}
}
