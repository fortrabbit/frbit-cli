package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		remote, want string
		ok           bool
	}{
		{"git@github.com:Acme/Site.git", "Acme/Site", true},
		{"git@GitHub.com:acme/site.git", "acme/site", true},
		{"ssh://git@github.com/acme/site.git", "acme/site", true},
		{"https://github.com/acme/site", "acme/site", true},
		{"https://github.com/acme/site.git/", "acme/site", true},
		{"https://token@github.com/acme/site.git", "acme/site", true},
		{"git@gitlab.com:acme/site.git", "", false},
		{"https://bitbucket.org/acme/site", "", false},
		{"https://github.com/acme", "", false},
		{"https://github.com/acme/site/extra", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizeRemote(tc.remote)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeRemote(%q) = %q, %v; want %q, %v", tc.remote, got, ok, tc.want, tc.ok)
		}
	}
}

// fakeGit answers git commands with a fixed remote and branch; empty means the command fails.
func fakeGit(remote, branch string) GitRunner {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "remote get-url origin":
			if remote != "" {
				return remote, nil
			}
		case "symbolic-ref --short HEAD":
			if branch != "" {
				return branch, nil
			}
		}
		return "", errors.New("git failed")
	}
}

type fakeAPI struct {
	apps, environments string
	missing            map[string]bool
	paths              []string
	appsQuery          string
}

func (f *fakeAPI) client(t *testing.T) *api.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			f.appsQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(f.apps))
		case r.URL.Path == "/v1/environments":
			_, _ = w.Write([]byte(f.environments))
		case strings.HasPrefix(r.URL.Path, "/v1/environments/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/environments/")
			if f.missing[id] {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"title":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"publicId":"` + id + `","name":"x"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client, err := api.NewClient(server.URL, "test-token", &http.Client{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func find(t *testing.T, resolver Resolver, fake *fakeAPI, dir, arg string) (string, error) {
	t.Helper()
	resolution, err := resolver.Find(context.Background(), fake.client(t), dir, arg)
	return resolution.ID, err
}

func writeEnvironments(t *testing.T, dir, content string) {
	t.Helper()
	if content == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, ".fortrabbit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit", "environments.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const connectedApps = `[{"publicId":"ap-aaa111","gitRepository":"acme/site","environments":["en-prod11","en-stag11"]},{"publicId":"ap-bbb222","gitRepository":null,"environments":["en-other1"]}]`
const connectedEnvironments = `[{"publicId":"en-prod11","name":"production","git":{"branch":"main","directory":null}},{"publicId":"en-stag11","name":"staging","git":{"branch":"develop","directory":null}}]`

func TestResolveOrder(t *testing.T) {
	cases := []struct {
		name        string
		arg, envVar string
		file        string
		remote      string
		branch      string
		want        string
		wantErr     string
	}{
		{name: "argument ID", arg: "en-arg111", envVar: "en-env111", file: "main: en-file11\n", remote: "git@github.com:acme/site.git", branch: "main", want: "en-arg111"},
		{name: "argument label", arg: "staging", file: "main: en-file11\nstaging: en-file22\n", branch: "main", want: "en-file22"},
		{name: "unknown label", arg: "nope", file: "main: en-file11\n", wantErr: `no environment labelled "nope"`},
		{name: "environment variable", envVar: "en-env111", file: "main: en-file11\n", branch: "main", want: "en-env111"},
		{name: "file entry for branch", file: "develop: en-file22\nmain: en-file11\n", remote: "git@github.com:acme/site.git", branch: "main", want: "en-file11"},
		{name: "server lookup", file: "other: en-file11\nmore: en-file22\n", remote: "git@github.com:ACME/site.git", branch: "develop", want: "en-stag11"},
		{name: "only entry", file: "staging: en-file22\n", remote: "git@github.com:acme/unknown.git", branch: "feature", want: "en-file22"},
		{name: "nothing found fails", remote: "git@github.com:acme/site.git", branch: "feature", wantErr: "no environment for this checkout; pass the environment ID or run frbit link <env-id>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeEnvironments(t, dir, tc.file)
			fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
			resolver := Resolver{
				Git:    fakeGit(tc.remote, tc.branch),
				Getenv: func(name string) string { return map[string]string{"FORTRABBIT_ENVIRONMENT": tc.envVar}[name] },
			}
			got, err := find(t, resolver, fake, dir, tc.arg)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveAmbiguousLookupNamesTheCandidates(t *testing.T) {
	apps := `[{"publicId":"ap-eu1111","gitRepository":"acme/site","environments":["en-eu1111"]},{"publicId":"ap-us1111","gitRepository":"acme/site","environments":["en-us1111"]}]`
	environments := `[{"publicId":"en-eu1111","name":"production","git":{"branch":"main"}},{"publicId":"en-us1111","name":"main","git":{"branch":"main"}}]`
	resolver := Resolver{Git: fakeGit("git@github.com:acme/site.git", "main"), Getenv: func(string) string { return "" }}

	dir := t.TempDir()
	_, err := find(t, resolver, &fakeAPI{apps: apps, environments: environments}, dir, "")
	want := "acme/site branch main deploys to more than one environment: en-eu1111 (production), en-us1111 (main); pass the environment ID or run frbit link <env-id>"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}

	writeEnvironments(t, dir, "staging: en-file22\n")
	got, err := find(t, resolver, &fakeAPI{apps: apps, environments: environments}, dir, "")
	if err != nil || got != "en-file22" {
		t.Fatalf("only file entry: got %q, %v", got, err)
	}
}

func TestResolveStaleFileEntryNamesTheFix(t *testing.T) {
	dir := t.TempDir()
	writeEnvironments(t, dir, "main: en-fork11\n")
	fake := &fakeAPI{apps: `[]`, environments: `[]`, missing: map[string]bool{"en-fork11": true}}
	resolver := Resolver{Git: fakeGit("", "main"), Getenv: func(string) string { return "" }}
	for _, arg := range []string{"", "main"} {
		_, err := find(t, resolver, fake, dir, arg)
		want := `en-fork11 (entry "main" in .fortrabbit/environments.yml) is not in your account; relink with frbit link <env-id> --label main, or edit .fortrabbit/environments.yml`
		if err == nil || err.Error() != want {
			t.Fatalf("arg %q: err = %v", arg, err)
		}
	}
	_, err := find(t, resolver, fake, dir, "en-fork11")
	if err != nil {
		t.Fatalf("an ID argument is not verified here: %v", err)
	}
	if _, err := Environment(context.Background(), fake.client(t), "en-fork11"); !errors.As(err, new(*NotInAccountError)) || strings.Contains(err.Error(), "run frbit link <") {
		t.Fatalf("Environment: %v", err)
	}
}

func TestResolveDetachedHeadFallsThrough(t *testing.T) {
	dir := t.TempDir()
	writeEnvironments(t, dir, "main: en-file11\n")
	fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
	resolver := Resolver{Git: fakeGit("git@github.com:acme/site.git", ""), Getenv: func(string) string { return "" }}
	got, err := find(t, resolver, fake, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "en-file11" {
		t.Fatalf("got %q", got)
	}
	for _, path := range fake.paths {
		if path == "/v1/apps" {
			t.Fatal("server lookup ran without a branch")
		}
	}
}

func TestResolveLegacyFileFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".fortrabbit"), []byte("app-env-id=en-old111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
	resolver := Resolver{Git: fakeGit("git@github.com:acme/site.git", "main"), Getenv: func(string) string { return "" }}
	for _, arg := range []string{"", "staging"} {
		if _, err := find(t, resolver, fake, dir, arg); !errors.Is(err, ErrLegacyFile) || err.Error() != ".fortrabbit is a link file from an older fortrabbit skill; delete it and run frbit link <env-id>" {
			t.Fatalf("arg %q: err = %v", arg, err)
		}
	}
	if got, err := find(t, resolver, fake, dir, "en-arg111"); err != nil || got != "en-arg111" {
		t.Fatalf("ID: %q, %v", got, err)
	}
}

func TestLookupChunksEnvironmentIDs(t *testing.T) {
	ids := make([]string, 31)
	for i := range ids {
		ids[i] = fmt.Sprintf(`"en-x%05d"`, i)
	}
	var queries []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/apps" {
			_, _ = w.Write([]byte(`[{"publicId":"ap-aaa111","gitRepository":"acme/site","environments":[` + strings.Join(ids, ",") + `]}]`))
			return
		}
		queries = append(queries, len(r.URL.Query()["publicId[]"]))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL, "test-token", &http.Client{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	resolver := Resolver{Git: fakeGit("git@github.com:acme/site.git", "main")}
	if _, _, err := resolver.Lookup(context.Background(), client, t.TempDir(), "main"); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != 30 || queries[1] != 1 {
		t.Fatalf("publicId[] per request = %v, want [30 1]", queries)
	}
}

func TestLookupFiltersAppsByRepository(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
	resolver := Resolver{Git: fakeGit("git@github.com:ACME/site.git", "develop"), Getenv: func(string) string { return "" }}
	match, found, err := resolver.Lookup(context.Background(), fake.client(t), dir, "develop")
	if err != nil || !found || match.EnvironmentID != "en-stag11" {
		t.Fatalf("match %+v, %v, %v", match, found, err)
	}
	if fake.appsQuery != "gitRepository=ACME%2Fsite" {
		t.Fatalf("apps query %q", fake.appsQuery)
	}
}

func TestEntriesSetAndWriteFileValidate(t *testing.T) {
	entries := newEntries()
	for _, tc := range [][2]string{{"en-label", "en-abc111"}, {"bad label", "en-abc111"}, {"main", "ap-abc111"}, {"main", "en-abc111\nx: en-def222"}} {
		if err := entries.Set(tc[0], tc[1]); err == nil {
			t.Fatalf("Set(%q, %q) accepted", tc[0], tc[1])
		}
	}
	if entries.Len() != 0 {
		t.Fatalf("rejected entries stored: %v", entries.Labels())
	}
	if err := entries.Set("main", "en-abc111"); err != nil {
		t.Fatal(err)
	}
	entries.ids["main"] = "not-an-id"
	dir := t.TempDir()
	if err := WriteFile(dir, entries); err == nil {
		t.Fatal("WriteFile wrote an invalid ID")
	}
	if _, err := os.Stat(filepath.Join(dir, ".fortrabbit", "environments.yml")); err == nil {
		t.Fatal("file written")
	}
}

func TestFileKeepsOtherEntriesAndRejectsNesting(t *testing.T) {
	dir := t.TempDir()
	writeEnvironments(t, dir, "# environments of this project\nmain: en-file11\nstaging: \"en-file22\"\n")
	entries, err := ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := entries.Get("staging"); id != "en-file22" {
		t.Fatalf("staging = %q", id)
	}
	if err := entries.Set("main", "en-new111"); err != nil {
		t.Fatal(err)
	}
	if err := entries.Set("feature/x", "en-new222"); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dir, entries); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".fortrabbit", "environments.yml"))
	want := "# environments of this project\nmain: en-new111\nstaging: \"en-file22\"\nfeature/x: en-new222\n"
	if string(raw) != want {
		t.Fatalf("file:\n%s", raw)
	}

	writeEnvironments(t, dir, "main:\n  id: en-file11\n")
	if _, err := ReadFile(dir); err == nil || !strings.Contains(err.Error(), "label: en-xxxxxx") {
		t.Fatalf("nested map accepted: %v", err)
	}
}

func TestResolveReadsFileAtGitTopLevel(t *testing.T) {
	top := t.TempDir()
	sub := filepath.Join(top, "web", "uploads")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEnvironments(t, top, "production: en-file11\n")
	git := func(ctx context.Context, dir string, args ...string) (string, error) {
		if strings.Join(args, " ") == "rev-parse --show-toplevel" {
			return top, nil
		}
		return fakeGit("", "main")(ctx, dir, args...)
	}
	resolver := Resolver{Git: git, Getenv: func(string) string { return "" }}
	got, err := resolver.Find(context.Background(), (&fakeAPI{}).client(t), sub, "")
	if err != nil || got.ID != "en-file11" || got.Source != SourceFile || got.Label != "production" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFindReportsSource(t *testing.T) {
	fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
	dir := t.TempDir()
	resolver := Resolver{Git: fakeGit("git@github.com:acme/site.git", "develop"), Getenv: func(string) string { return "en-env111" }}
	if got, err := resolver.Find(context.Background(), fake.client(t), dir, ""); err != nil || got.Source != SourceEnv || got.ID != "en-env111" {
		t.Fatalf("env: %+v, %v", got, err)
	}
	resolver.Getenv = func(string) string { return "" }
	got, err := resolver.Find(context.Background(), fake.client(t), dir, "")
	if err != nil || got.Source != SourceRepository || got.ID != "en-stag11" || got.Match.AppID != "ap-aaa111" {
		t.Fatalf("repository: %+v, %v", got, err)
	}
	resolver.Git = fakeGit("", "feature")
	if _, err := resolver.Find(context.Background(), fake.client(t), dir, ""); !errors.Is(err, ErrNoEnvironment) {
		t.Fatalf("none: %v", err)
	}
}

func TestLookupIgnoresInvalidIDs(t *testing.T) {
	fake := &fakeAPI{
		apps:         `[{"publicId":"ap-aaa111","gitRepository":"acme/site","environments":["../../x"]}]`,
		environments: `[{"publicId":"../../x","name":"evil","git":{"branch":"main","directory":null}}]`,
	}
	resolver := Resolver{Git: fakeGit("git@github.com:acme/site.git", "main"), Getenv: func(string) string { return "" }}
	if _, found, err := resolver.Lookup(context.Background(), fake.client(t), t.TempDir(), "main"); err != nil || found {
		t.Fatalf("lookup: found %v, %v", found, err)
	}
}
