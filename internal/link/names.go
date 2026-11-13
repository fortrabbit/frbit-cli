package link

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

// Candidate is an environment of the account with the name of its app.
type Candidate struct {
	AppName         string
	EnvironmentID   string
	EnvironmentName string
}

// Name is the app/environment form that Find accepts; an environment whose app is not visible shows by its own name.
func (c Candidate) Name() string {
	if c.AppName == "" {
		return c.EnvironmentName
	}
	return c.AppName + "/" + c.EnvironmentName
}

// AppNames maps each environment ID to the name of its app.
func AppNames(ctx context.Context, client *api.Client) (map[string]string, error) {
	apps, err := client.ListAll(ctx, "/v1/apps", nil, 0)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	names := map[string]string{}
	for _, app := range apps.Resources {
		name, _ := app["name"].(string)
		environments, _ := app["environments"].([]any)
		for _, value := range environments {
			if id, ok := value.(string); ok {
				names[id] = name
			}
		}
	}
	return names, nil
}

// Candidates lists every environment of the account with its app, sorted by name.
func Candidates(ctx context.Context, client *api.Client) ([]Candidate, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type listed struct {
		response api.ResourcesResponse
		err      error
	}
	// Both lists in parallel: completion waits on this call.
	environmentsDone := make(chan listed, 1)
	go func() {
		response, err := client.ListAll(ctx, "/v1/environments", nil, 0)
		environmentsDone <- listed{response, err}
	}()
	appNames, err := AppNames(ctx, client)
	if err != nil {
		return nil, err
	}
	environments := <-environmentsDone
	if environments.err != nil {
		return nil, fmt.Errorf("list environments: %w", environments.err)
	}
	candidates := make([]Candidate, 0, len(environments.response.Resources))
	for _, environment := range environments.response.Resources {
		id, _ := environment["publicId"].(string)
		if !IsID(id) {
			continue
		}
		name, _ := environment["name"].(string)
		candidates = append(candidates, Candidate{AppName: appNames[id], EnvironmentID: id, EnvironmentName: name})
	}
	slices.SortFunc(candidates, func(a, b Candidate) int {
		if c := strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())); c != 0 {
			return c
		}
		return strings.Compare(a.EnvironmentID, b.EnvironmentID)
	})
	return candidates, nil
}

// matchName returns the candidates named app/environment, or app alone for its environments; exact matches win over case-insensitive ones.
func matchName(candidates []Candidate, name string) []Candidate {
	appName, envName, split := cutLast(name, "/")
	for _, equal := range []func(a, b string) bool{func(a, b string) bool { return a == b }, strings.EqualFold} {
		var matches []Candidate
		for _, candidate := range candidates {
			// The whole argument as an app name covers apps whose name holds a slash; both forms count, so neither wins silently.
			if (split && equal(candidate.AppName, appName) && equal(candidate.EnvironmentName, envName)) || equal(candidate.AppName, name) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) > 0 {
			return matches
		}
	}
	return nil
}

// cutLast splits on the last sep: environment names cannot hold a slash, app names may.
func cutLast(value, sep string) (string, string, bool) {
	index := strings.LastIndex(value, sep)
	if index < 0 {
		return value, "", false
	}
	return value[:index], value[index+len(sep):], true
}

// maxListed caps the candidates printed in an error, so a large account stays readable.
const maxListed = 20

// ListCandidates prints one line per candidate, app/environment and ID, up to maxListed.
func ListCandidates(candidates []Candidate) string {
	var b strings.Builder
	table := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, candidate := range candidates[:min(len(candidates), maxListed)] {
		_, _ = fmt.Fprintf(table, "  %s\t%s\n", iostreams.SingleLine(candidate.Name()), candidate.EnvironmentID)
	}
	_ = table.Flush()
	if len(candidates) > maxListed {
		_, _ = fmt.Fprintf(&b, "  … and %d more: frbit environments list --all\n", len(candidates)-maxListed)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func byName(ctx context.Context, client *api.Client, name string) (Candidate, error) {
	candidates, err := Candidates(ctx, client)
	if err != nil {
		return Candidate{}, err
	}
	matches := matchName(candidates, name)
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return Candidate{}, fmt.Errorf("%q matches more than one environment; pass the ID:\n%s", name, ListCandidates(matches))
	case len(candidates) == 0:
		return Candidate{}, fmt.Errorf("no environment labelled %q in %s or named %q; this account has no environments", name, FilePath, name)
	}
	return Candidate{}, fmt.Errorf("no environment labelled %q in %s or named %q; pass one of:\n%s", name, FilePath, name, ListCandidates(candidates))
}
