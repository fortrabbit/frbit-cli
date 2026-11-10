package link

import (
	"bytes"
	"context"
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
