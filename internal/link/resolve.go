// Package link finds the fortrabbit environment of a local checkout.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
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

// Resolver finds the environment of a checkout; zero fields use system git, os.Getenv and no terminal.
type Resolver struct {
	Git    GitRunner
	Getenv func(string) string
	In     io.Reader
	Out    io.Writer
	IsTTY  bool
}

// ForStreams returns a Resolver that asks on the terminal of streams.
func ForStreams(streams iostreams.IOStreams) Resolver {
	return Resolver{In: streams.In, Out: streams.ErrOut, IsTTY: streams.IsTTY}
}

// Resolve finds the environment non-interactively; commands with a terminal use ForStreams(...).Resolve.
func Resolve(ctx context.Context, client *api.Client, dir string, arg string) (string, error) {
	return Resolver{}.Resolve(ctx, client, dir, arg)
}

// Match is an environment found through the repository connected to its app.
type Match struct {
	Repository      string
	Branch          string
	AppID           string
	EnvironmentID   string
	EnvironmentName string
}

// Resolve tries arg (ID or label), FORTRABBIT_ENVIRONMENT, the file entry for the branch, the server lookup, the only file entry, then asks.
func (r Resolver) Resolve(ctx context.Context, client *api.Client, dir string, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if IsID(arg) {
		return arg, nil
	}
	if arg == "" {
		if value := strings.TrimSpace(r.getenv(EnvVar)); value != "" {
			if !IsID(value) {
				return "", fmt.Errorf("%s=%q is not an environment ID", EnvVar, value)
			}
			return value, nil
		}
	}
	branch := r.Branch(ctx, dir)
	entries, err := r.Entries(ctx, dir, branch)
	// Without a terminal a legacy file only fails when nothing else resolves, so CI on a connected repo keeps working.
	legacyErr := err
	if errors.Is(err, ErrLegacyFile) && !r.IsTTY {
		entries, err = newEntries(), nil
	} else {
		legacyErr = nil
	}
	if err != nil {
		return "", err
	}
	if arg != "" {
		id, ok := entries.Get(arg)
		if !ok && legacyErr != nil {
			return "", legacyErr
		}
		if !ok {
			return "", fmt.Errorf("no environment labelled %q in %s; run frbit link", arg, FilePath)
		}
		return Verify(ctx, client, id)
	}
	if id, ok := entries.Get(branch); ok && branch != "" {
		return Verify(ctx, client, id)
	}
	if branch != "" {
		match, found, err := r.Lookup(ctx, client, dir, branch)
		if err != nil {
			return "", err
		}
		if found {
			return match.EnvironmentID, nil
		}
	}
	if entries.Len() == 1 {
		id, _ := entries.Get(entries.Labels()[0])
		return Verify(ctx, client, id)
	}
	if legacyErr != nil {
		return "", legacyErr
	}
	if !r.IsTTY {
		return "", errors.New("no environment for this checkout; run frbit link or pass the environment ID")
	}
	return r.Pick(ctx, client)
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

// Entries reads the environments file; a legacy .fortrabbit file is migrated under branch after asking on a terminal.
func (r Resolver) Entries(ctx context.Context, dir string, branch string) (*Entries, error) {
	entries, err := ReadFile(dir)
	if !errors.Is(err, ErrLegacyFile) {
		return entries, err
	}
	if !r.IsTTY {
		return nil, err
	}
	id, ok, lerr := LegacyID(dir)
	if lerr != nil {
		return nil, lerr
	}
	if !ok {
		return nil, errors.New(".fortrabbit is a file without app-env-id=; move it away and run frbit link")
	}
	label := branch
	if label == "" || ValidLabel(label) != nil {
		label = "default"
	}
	question := fmt.Sprintf(".fortrabbit is a file from an older setup with %s. Move it to %s as %q? [y/N] ", id, FilePath, label)
	yes, err := cmdutil.ConfirmContext(ctx, r.in(), r.out(), question)
	if err != nil {
		return nil, err
	}
	if !yes {
		return nil, ErrLegacyFile
	}
	if _, err := MigrateLegacy(dir, label); err != nil {
		return nil, err
	}
	fmt.Fprintf(r.out(), "Moved %s to %s as %q.\n", id, FilePath, label)
	return ReadFile(dir)
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
	query := url.Values{}
	for _, app := range apps.Resources {
		connected, _ := app["gitRepository"].(string)
		if !strings.EqualFold(connected, repository) {
			continue
		}
		appID, _ := app["publicId"].(string)
		environments, _ := app["environments"].([]any)
		for _, value := range environments {
			if id, ok := value.(string); ok {
				appOf[id] = appID
				query.Add("publicId[]", id)
			}
		}
	}
	if len(appOf) == 0 {
		return Match{}, false, nil
	}
	environments, err := client.ListAll(ctx, "/v1/environments", query, 0)
	if err != nil {
		return Match{}, false, fmt.Errorf("list environments: %w", err)
	}
	var matches []Match
	for _, environment := range environments.Resources {
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
			return nil, fmt.Errorf("%s is not in your account; run frbit link", id)
		}
		return nil, err
	}
	return response.Resource, nil
}

// Pick lists the environments of the account and lets the person choose one by number or ID.
func (r Resolver) Pick(ctx context.Context, client *api.Client) (string, error) {
	environments, err := client.ListAll(ctx, "/v1/environments", nil, 0)
	if err != nil {
		return "", fmt.Errorf("list environments: %w", err)
	}
	if len(environments.Resources) == 0 {
		return "", errors.New("no environments in your account")
	}
	ids := make([]string, 0, len(environments.Resources))
	fmt.Fprintln(r.out(), "Environments in your account:")
	for i, environment := range environments.Resources {
		id, _ := environment["publicId"].(string)
		name, _ := environment["name"].(string)
		ids = append(ids, id)
		fmt.Fprintf(r.out(), "  %d) %s  %s\n", i+1, id, name)
	}
	answer, err := cmdutil.AskContext(ctx, r.in(), r.out(), fmt.Sprintf("Pick an environment [1-%d]: ", len(ids)))
	if err != nil {
		return "", err
	}
	if number, err := strconv.Atoi(answer); err == nil && number >= 1 && number <= len(ids) {
		return ids[number-1], nil
	}
	for _, id := range ids {
		if answer == id {
			return id, nil
		}
	}
	return "", fmt.Errorf("no environment picked (%q)", answer)
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

func (r Resolver) in() io.Reader {
	if r.In != nil {
		return r.In
	}
	return strings.NewReader("")
}

func (r Resolver) out() io.Writer {
	if r.Out != nil {
		return r.Out
	}
	return io.Discard
}
