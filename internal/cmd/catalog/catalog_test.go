package catalog

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func run(t *testing.T, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: &bytes.Buffer{}},
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
