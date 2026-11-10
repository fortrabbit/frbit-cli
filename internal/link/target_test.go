package link

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTargetResolveAnnouncesImplicitSources(t *testing.T) {
	fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
	cases := []struct {
		name, arg, file, envVar, remote, branch, want, line string
	}{
		{name: "ID", arg: "en-arg111", want: "en-arg111"},
		{name: "label", arg: "production", file: "production: en-file11\n", want: "en-file11", line: "→ en-file11 (production, from .fortrabbit/environments.yml)\n"},
		{name: "env var", envVar: "en-env111", want: "en-env111", line: "→ en-env111 (from FORTRABBIT_ENVIRONMENT)\n"},
		{name: "repository", remote: "git@github.com:acme/site.git", branch: "main", want: "en-prod11", line: "→ en-prod11 (from the repository acme/site, branch main)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeEnvironments(t, dir, tc.file)
			var stderr bytes.Buffer
			target := Target{Dir: dir, Stderr: &stderr, Resolver: Resolver{Git: fakeGit(tc.remote, tc.branch), Getenv: func(string) string { return tc.envVar }}}
			got, err := target.Resolve(context.Background(), fake.client(t), tc.arg)
			if err != nil || got != tc.want || stderr.String() != tc.line {
				t.Fatalf("got %q, %v, stderr %q", got, err, stderr.String())
			}
		})
	}
}

func TestTargetRequireRefusesImplicit(t *testing.T) {
	fake := &fakeAPI{apps: connectedApps, environments: connectedEnvironments}
	dir := t.TempDir()
	target := Target{Dir: dir, Resolver: Resolver{Git: fakeGit("git@github.com:acme/site.git", "main"), Getenv: func(string) string { return "" }}}
	_, err := target.Require(context.Background(), fake.client(t), "", "frbit jobs create <env-id|label>")
	want := "name the environment: frbit jobs create <env-id|label> (the checkout resolves to en-prod11 via the repository acme/site, branch main)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	target.Resolver.Git = fakeGit("", "")
	if _, err := target.Require(context.Background(), fake.client(t), "", "frbit jobs create <env-id|label>"); err == nil || err.Error() != "name the environment: frbit jobs create <env-id|label>" {
		t.Fatalf("no checkout: %v", err)
	}
	writeEnvironments(t, dir, "staging: en-file22\n")
	if got, err := target.Require(context.Background(), fake.client(t), "staging", "x"); err != nil || got != "en-file22" {
		t.Fatalf("label: %q, %v", got, err)
	}
	if _, err := target.Require(context.Background(), fake.client(t), "nope", "x"); err == nil || !strings.Contains(err.Error(), `no environment labelled "nope"`) {
		t.Fatalf("unknown label: %v", err)
	}
}

func TestTargetOptionalAndIsEnvironmentArg(t *testing.T) {
	fake := &fakeAPI{apps: `[]`, environments: `[]`}
	dir := t.TempDir()
	target := Target{Dir: dir, Resolver: Resolver{Git: fakeGit("", ""), Getenv: func(string) string { return "" }, IsTTY: true}}
	if got, err := target.Optional(context.Background(), fake.client(t), ""); err != nil || got != "" {
		t.Fatalf("optional: %q, %v", got, err)
	}
	writeEnvironments(t, dir, "production: en-file11\n")
	if !target.IsEnvironmentArg(context.Background(), "production") || !target.IsEnvironmentArg(context.Background(), "en-abc123") || target.IsEnvironmentArg(context.Background(), "cpu") {
		t.Fatal("IsEnvironmentArg")
	}
}
