package link

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var named = []Candidate{
	{AppName: "shop", EnvironmentID: "en-shop11", EnvironmentName: "main"},
	{AppName: "shop", EnvironmentID: "en-shop22", EnvironmentName: "staging"},
	{AppName: "Blog", EnvironmentID: "en-blog11", EnvironmentName: "main"},
	{AppName: "acme/old", EnvironmentID: "en-old111", EnvironmentName: "production"},
	{AppName: "Twin", EnvironmentID: "en-twin11", EnvironmentName: "main"},
	{AppName: "Twin", EnvironmentID: "en-twin22", EnvironmentName: "main"},
	{AppName: "Case", EnvironmentID: "en-case11", EnvironmentName: "main"},
	{AppName: "case", EnvironmentID: "en-case22", EnvironmentName: "main"},
}

func TestMatchName(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"shop/main", []string{"en-shop11"}},
		{"blog/main", []string{"en-blog11"}},
		{"Blog", []string{"en-blog11"}},
		{"shop", []string{"en-shop11", "en-shop22"}},
		{"acme/old/production", []string{"en-old111"}},
		{"acme/old", []string{"en-old111"}},
		{"Twin/main", []string{"en-twin11", "en-twin22"}},
		{"case/main", []string{"en-case22"}},
		{"CASE/main", []string{"en-case11", "en-case22"}},
		{"shop/nope", nil},
		{"nope", nil},
	}
	for _, tc := range cases {
		var got []string
		for _, match := range matchName(named, tc.name) {
			got = append(got, match.EnvironmentID)
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("matchName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

const namedApps = `[{"publicId":"ap-shop11","name":"shop","environments":["en-shop11","en-shop22"]},{"publicId":"ap-blog11","name":"Blog","environments":["en-blog11"]}]`
const namedEnvironments = `[{"publicId":"en-shop22","name":"staging"},{"publicId":"en-shop11","name":"main"},{"publicId":"en-blog11","name":"main"}]`

func TestFindByName(t *testing.T) {
	dir := t.TempDir()
	writeEnvironments(t, dir, "shop/main: en-file11\n")
	resolver := Resolver{Git: fakeGit("", "feature"), Getenv: func(string) string { return "" }}
	fake := &fakeAPI{apps: namedApps, environments: namedEnvironments}
	got, err := resolver.Find(context.Background(), fake.client(t), dir, "shop/staging")
	if err != nil || got.ID != "en-shop22" || got.Source != SourceName || got.Label != "shop/staging" {
		t.Fatalf("name: %+v, %v", got, err)
	}
	if got, err := resolver.Find(context.Background(), fake.client(t), dir, "shop/main"); err != nil || got.ID != "en-file11" || got.Source != SourceFile {
		t.Fatalf("the label wins: %+v, %v", got, err)
	}
	_, err = resolver.Find(context.Background(), fake.client(t), dir, "shop")
	want := "\"shop\" matches more than one environment; pass the ID:\n  shop/main     en-shop11\n  shop/staging  en-shop22"
	if err == nil || err.Error() != want {
		t.Fatalf("ambiguous: %v", err)
	}
	_, err = resolver.Find(context.Background(), fake.client(t), dir, "shop/live")
	want = "no environment labelled \"shop/live\" in .fortrabbit/environments.yml or named \"shop/live\"; pass one of:\n  Blog/main     en-blog11\n  shop/main     en-shop11\n  shop/staging  en-shop22"
	if err == nil || err.Error() != want {
		t.Fatalf("unknown: %v", err)
	}
}

func TestListCandidatesCaps(t *testing.T) {
	many := make([]Candidate, maxListed+3)
	for i := range many {
		many[i] = Candidate{AppName: "a", EnvironmentName: "e", EnvironmentID: "en-x"}
	}
	got := ListCandidates(many)
	if lines := strings.Count(got, "\n") + 1; lines != maxListed+1 || !strings.HasSuffix(got, "… and 3 more: frbit environments list --all") {
		t.Fatalf("%d lines:\n%s", lines, got)
	}
}

func targetIn(t *testing.T, dir, branch, stdin string, interactive bool) (Target, *bytes.Buffer) {
	t.Helper()
	stderr := &bytes.Buffer{}
	return Target{
		Resolver:    Resolver{Git: fakeGit("", branch), Getenv: func(string) string { return "" }},
		Dir:         dir,
		Stderr:      stderr,
		In:          bufio.NewReader(strings.NewReader(stdin)),
		Interactive: interactive,
	}, stderr
}

func TestResolveWithoutTerminalListsCandidates(t *testing.T) {
	target, _ := targetIn(t, t.TempDir(), "feature", "1\n", false)
	_, err := target.Resolve(context.Background(), (&fakeAPI{apps: namedApps, environments: namedEnvironments}).client(t), "")
	want := ErrNoEnvironment.Error() + ":\n  Blog/main     en-blog11\n  shop/main     en-shop11\n  shop/staging  en-shop22"
	if !errors.Is(err, ErrNoEnvironment) || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

func TestResolvePicksAndSaves(t *testing.T) {
	cases := []struct {
		name, branch, stdin, file string
		noSave                    bool
		want, wantFile, wantErr   string
	}{
		{name: "number, saved under the branch", branch: "feature", stdin: "3\ny\n", want: "en-shop22", wantFile: "feature: en-shop22\n"},
		{name: "name, not saved", branch: "feature", stdin: "blog/main\nn\n", want: "en-blog11"},
		{name: "retry after a typo, by ID", branch: "feature", stdin: "9\nen-shop11\ny\n", want: "en-shop11", wantFile: "feature: en-shop11\n"},
		{name: "retry after an ambiguous name", branch: "feature", stdin: "shop\n2\nn\n", want: "en-shop11"},
		{name: "outside git, saved under the environment name", stdin: "3\ny\n", want: "en-shop22", wantFile: "staging: en-shop22\n"},
		{name: "outside git with entries, no offer", stdin: "3\n", file: "a: en-file11\nb: en-file22\n", want: "en-shop22", wantFile: "a: en-file11\nb: en-file22\n"},
		{name: "branch that is no label, no offer", branch: "en-translations", stdin: "3\n", file: "a: en-file11\nb: en-file22\n", want: "en-shop22", wantFile: "a: en-file11\nb: en-file22\n"},
		{name: "no offer for a command that confirms later", branch: "feature", stdin: "3\ny\n", noSave: true, want: "en-shop22"},
		{name: "empty answer aborts", branch: "feature", stdin: "\n", wantErr: "aborted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeEnvironments(t, dir, tc.file)
			target, stderr := targetIn(t, dir, tc.branch, tc.stdin, true)
			target.NoSave = tc.noSave
			got, err := target.Resolve(context.Background(), (&fakeAPI{apps: namedApps, environments: namedEnvironments}).client(t), "")
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v\n%s", got, err, stderr)
			}
			raw, _ := os.ReadFile(filepath.Join(dir, ".fortrabbit", "environments.yml"))
			if string(raw) != tc.wantFile {
				t.Fatalf("environments.yml = %q", raw)
			}
			if !strings.Contains(stderr.String(), "→ "+tc.want) {
				t.Fatalf("no announce line:\n%s", stderr)
			}
		})
	}
}

func TestResolveDiscoveryKeepsAPIErrors(t *testing.T) {
	target, _ := targetIn(t, t.TempDir(), "feature", "", false)
	_, err := target.Resolve(context.Background(), (&fakeAPI{}).client(t), "")
	if !errors.Is(err, ErrNoEnvironment) || !strings.Contains(err.Error(), "list apps") {
		t.Fatalf("err = %v", err)
	}
}

func TestMatchNameSlashAppIsAmbiguous(t *testing.T) {
	candidates := []Candidate{
		{AppName: "shop", EnvironmentID: "en-real11", EnvironmentName: "production"},
		{AppName: "shop/production", EnvironmentID: "en-fake11", EnvironmentName: "main"},
	}
	if got := matchName(candidates, "shop/production"); len(got) != 2 {
		t.Fatalf("matches = %v, want both", got)
	}
}

func TestListCandidatesKeepsOneLinePerName(t *testing.T) {
	got := ListCandidates([]Candidate{{AppName: "a\n  2  shop/main\ten-fake11‮", EnvironmentName: "main", EnvironmentID: "en-real11"}})
	if strings.Count(got, "\n") != 0 || strings.Contains(got, "‮") {
		t.Fatalf("list:\n%q", got)
	}
}
