// Package link finds the fortrabbit environment of a local checkout.
package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

// EnvVar is set by the platform on every environment, so frbit over SSH targets that environment.
const EnvVar = "FORTRABBIT_ENVIRONMENT"

// GitRunner runs git in dir and returns its trimmed stdout.
type GitRunner func(ctx context.Context, dir string, args ...string) (string, error)

// SystemGit runs the git binary.
func SystemGit(ctx context.Context, dir string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Resolver finds the environment of a checkout; zero fields use system git and os.Getenv.
type Resolver struct {
	Git    GitRunner
	Getenv func(string) string
}

// Match is an environment found through the repository connected to its app.
type Match struct {
	Repository      string
	Branch          string
	AppID           string
	EnvironmentID   string
	EnvironmentName string
}

// Sources of a Resolution.
const (
	SourceArgument   = "argument"
	SourceEnv        = "env"
	SourceFile       = "file"
	SourceRepository = "repository"
)

// ErrNoEnvironment means nothing in the checkout names an environment.
var ErrNoEnvironment = errors.New("no environment for this checkout; pass the environment ID or run frbit link <env-id>")

// Resolution is a resolved environment and the step that found it.
type Resolution struct {
	ID     string
	Source string
	// Label is the environments.yml entry for SourceFile.
	Label string
	// Match is set for SourceRepository.
	Match Match
}

// Find tries arg (ID or label), FORTRABBIT_ENVIRONMENT, the file entry for the branch, the server lookup and the only file entry; it returns ErrNoEnvironment when none applies.
func (r Resolver) Find(ctx context.Context, client *api.Client, dir string, arg string) (Resolution, error) {
	arg = strings.TrimSpace(arg)
	if IsID(arg) {
		return Resolution{ID: arg, Source: SourceArgument}, nil
	}
	if arg == "" {
		if value := strings.TrimSpace(r.getenv(EnvVar)); value != "" {
			if !IsID(value) {
				return Resolution{}, fmt.Errorf("%s=%q is not an environment ID", EnvVar, value)
			}
			return Resolution{ID: value, Source: SourceEnv}, nil
		}
	}
	dir = r.Root(ctx, dir)
	branch := r.Branch(ctx, dir)
	entries, err := ReadFile(dir)
	if err != nil {
		return Resolution{}, err
	}
	fromFile := func(label string) (Resolution, error) {
		id, _ := entries.Get(label)
		if _, err := Verify(ctx, client, id); err != nil {
			return Resolution{}, StaleEntry(err, label)
		}
		return Resolution{ID: id, Source: SourceFile, Label: label}, nil
	}
	if arg != "" {
		if _, ok := entries.Get(arg); !ok {
			return Resolution{}, fmt.Errorf("no environment labelled %q in %s; run frbit link", arg, FilePath)
		}
		return fromFile(arg)
	}
	if _, ok := entries.Get(branch); ok && branch != "" {
		return fromFile(branch)
	}
	if branch != "" {
		match, found, err := r.Lookup(ctx, client, dir, branch)
		if err != nil {
			return Resolution{}, err
		}
		if found {
			return Resolution{ID: match.EnvironmentID, Source: SourceRepository, Match: match}, nil
		}
	}
	if entries.Len() == 1 {
		return fromFile(entries.Labels()[0])
	}
	return Resolution{}, ErrNoEnvironment
}

// TopLevel returns the top folder of the git checkout holding dir.
func (r Resolver) TopLevel(ctx context.Context, dir string) (string, bool) {
	top, err := r.git()(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(top) == "" {
		return "", false
	}
	return filepath.Clean(strings.TrimSpace(top)), true
}

// Root is the folder that holds the link file: the git top level, or dir outside git.
func (r Resolver) Root(ctx context.Context, dir string) string {
	if top, ok := r.TopLevel(ctx, dir); ok {
		return top
	}
	return dir
}

// Branch returns the checked-out branch, or "" on a detached HEAD or outside git.
func (r Resolver) Branch(ctx context.Context, dir string) string {
	branch, err := r.git()(ctx, dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(branch)
}

// Repository returns the GitHub owner/repo of the origin remote.
func (r Resolver) Repository(ctx context.Context, dir string) (string, bool) {
	remote, err := r.git()(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", false
	}
	return NormalizeRemote(remote)
}

// Lookup finds the environment deploying branch of the app connected to the origin repository; found is false without a single match.
func (r Resolver) Lookup(ctx context.Context, client *api.Client, dir string, branch string) (Match, bool, error) {
	repository, ok := r.Repository(ctx, dir)
	if !ok || branch == "" {
		return Match{}, false, nil
	}
	apps, err := client.ListAll(ctx, "/v1/apps", url.Values{"gitRepository": {repository}}, 0)
	if err != nil {
		return Match{}, false, fmt.Errorf("list apps: %w", err)
	}
	appOf := map[string]string{}
	var ids []string
	for _, app := range apps.Resources {
		connected, _ := app["gitRepository"].(string)
		if !strings.EqualFold(connected, repository) {
			continue
		}
		appID, _ := app["publicId"].(string)
		environments, _ := app["environments"].([]any)
		for _, value := range environments {
			// IDs from the API end up in file names, so only well-formed ones count.
			if id, ok := value.(string); ok && IsID(id) {
				appOf[id] = appID
				ids = append(ids, id)
			}
		}
	}
	if len(appOf) == 0 {
		return Match{}, false, nil
	}
	var environments []api.Resource
	// The API takes at most 30 publicId[] per request.
	for chunk := range slices.Chunk(ids, 30) {
		page, err := client.ListAll(ctx, "/v1/environments", url.Values{"publicId[]": chunk}, 0)
		if err != nil {
			return Match{}, false, fmt.Errorf("list environments: %w", err)
		}
		environments = append(environments, page.Resources...)
	}
	var matches []Match
	for _, environment := range environments {
		id, _ := environment["publicId"].(string)
		git, _ := environment["git"].(map[string]any)
		deployed, _ := git["branch"].(string)
		if _, ours := appOf[id]; !ours || deployed != branch {
			continue
		}
		name, _ := environment["name"].(string)
		matches = append(matches, Match{Repository: repository, Branch: branch, AppID: appOf[id], EnvironmentID: id, EnvironmentName: name})
	}
	if len(matches) != 1 {
		return Match{}, false, nil
	}
	return matches[0], true, nil
}

// Verify checks that id belongs to the account, so a fork of a template fails with a hint instead of a bare 404.
func Verify(ctx context.Context, client *api.Client, id string) (string, error) {
	if _, err := Environment(ctx, client, id); err != nil {
		return "", err
	}
	return id, nil
}

// Environment gets the environment id, with Verify's hint when it is not in the account.
func Environment(ctx context.Context, client *api.Client, id string) (api.Resource, error) {
	response, err := client.GetResource(ctx, "/v1/environments/"+url.PathEscape(id))
	if err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == http.StatusNotFound || httpErr.Status == http.StatusForbidden) {
			return nil, &NotInAccountError{ID: id}
		}
		return nil, err
	}
	return response.Resource, nil
}

// NotInAccountError means the API does not know the environment for this account, for example in a fork of a template.
type NotInAccountError struct{ ID string }

func (e *NotInAccountError) Error() string {
	return e.ID + " is not in your account; check the ID with frbit environments list"
}

// StaleEntry names the fix when the environment of a file entry is not in the account.
func StaleEntry(err error, label string) error {
	var missing *NotInAccountError
	if !errors.As(err, &missing) {
		return err
	}
	return fmt.Errorf("%s (entry %q in %s) is not in your account; relink with frbit link <env-id> --label %s, or edit %s", missing.ID, label, FilePath, label, FilePath)
}

// NormalizeRemote turns a GitHub remote URL (SSH, scp-like or HTTPS) into owner/repo.
func NormalizeRemote(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	var path string
	if host, rest, ok := strings.Cut(remote, ":"); ok && strings.EqualFold(host, "git@github.com") {
		path = rest
	} else {
		parsed, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
			return "", false
		}
		switch parsed.Scheme {
		case "https", "http", "ssh", "git":
		default:
			return "", false
		}
		path = parsed.Path
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, repo, ok := strings.Cut(path, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", false
	}
	return owner + "/" + repo, true
}

func (r Resolver) git() GitRunner {
	if r.Git != nil {
		return r.Git
	}
	return SystemGit
}

func (r Resolver) getenv(name string) string {
	if r.Getenv != nil {
		return r.Getenv(name)
	}
	return os.Getenv(name)
}
