package link

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

// Pick lets a person on a terminal choose one of the environments of the account; elsewhere it fails with ErrNoEnvironment and the list.
func (t Target) Pick(ctx context.Context, client *api.Client) (Candidate, error) {
	candidates, err := Candidates(ctx, client)
	if err != nil {
		return Candidate{}, fmt.Errorf("%w (listing the environments failed: %v)", ErrNoEnvironment, err)
	}
	if len(candidates) == 0 {
		return Candidate{}, ErrNoEnvironment
	}
	if !t.Interactive || t.In == nil || t.Stderr == nil {
		return Candidate{}, fmt.Errorf("%w:\n%s", ErrNoEnvironment, ListCandidates(candidates))
	}
	return ask(ctx, t.In, t.Stderr, candidates)
}

// discover picks the environment when nothing in the checkout names one, and offers to save the pick.
func (t Target) discover(ctx context.Context, client *api.Client, dir string) (string, error) {
	picked, err := t.Pick(ctx, client)
	if err != nil {
		return "", err
	}
	if !t.NoSave {
		if err := t.offerSave(ctx, dir, picked); err != nil {
			return "", err
		}
	}
	_, _ = fmt.Fprintf(t.Stderr, "→ %s (%s)\n", picked.EnvironmentID, iostreams.SingleLine(picked.Name()))
	return picked.EnvironmentID, nil
}

func ask(ctx context.Context, in *bufio.Reader, out io.Writer, candidates []Candidate) (Candidate, error) {
	if _, err := fmt.Fprintln(out, "No environment for this checkout. Pick one:"); err != nil {
		return Candidate{}, err
	}
	width := len(strconv.Itoa(len(candidates)))
	for i, candidate := range candidates {
		if _, err := fmt.Fprintf(out, "  %*d  %s  %s\n", width, i+1, iostreams.SingleLine(candidate.Name()), candidate.EnvironmentID); err != nil {
			return Candidate{}, err
		}
	}
	for {
		answer, err := cmdutil.AskContext(ctx, in, out, fmt.Sprintf("Environment [1-%d]: ", len(candidates)))
		if err != nil {
			return Candidate{}, err
		}
		if answer == "" {
			return Candidate{}, cmdutil.Aborted(out)
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(candidates) {
			return candidates[n-1], nil
		}
		for _, candidate := range candidates {
			if candidate.EnvironmentID == answer {
				return candidate, nil
			}
		}
		hint := "%q is not in the list; type a number, or press Enter to abort.\n"
		switch matches := matchName(candidates, answer); {
		case len(matches) == 1:
			return matches[0], nil
		case len(matches) > 1:
			hint = "%q matches more than one environment; type its number.\n"
		}
		if _, err := fmt.Fprintf(out, hint, answer); err != nil {
			return Candidate{}, err
		}
	}
}

func (t Target) offerSave(ctx context.Context, dir string, picked Candidate) error {
	root := t.Resolver.Root(ctx, dir)
	branch := t.Resolver.Branch(ctx, root)
	entries, err := ReadFile(root)
	if err != nil {
		return err
	}
	label := SaveLabel(branch, picked.EnvironmentName)
	// Without a branch, Find only uses the file when it holds a single entry.
	if ValidLabel(label) != nil || (branch == "" && entries.Len() > 0) {
		return nil
	}
	save, err := cmdutil.ConfirmContext(ctx, t.In, t.Stderr, fmt.Sprintf("Save as %q in %s, so frbit finds it next time? [y/N] ", label, FilePath))
	if err != nil || !save {
		return err
	}
	return Save(root, label, picked.EnvironmentID, t.Stderr)
}

// SaveLabel is the label a link is saved under: the branch, which Find looks up first, or outside git the environment name.
func SaveLabel(branch, environmentName string) string {
	if branch != "" {
		return branch
	}
	return environmentName
}

// Save stores id under label in the link file of root and says so on out.
func Save(root, label, id string, out io.Writer) error {
	entries, err := ReadFile(root)
	if err != nil {
		return err
	}
	if err := entries.Set(label, id); err != nil {
		return err
	}
	if err := WriteFile(root, entries); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Linked %s to %s in %s.\n", label, id, FilePath)
	return err
}
