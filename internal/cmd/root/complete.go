package root

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	envlink "github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// completionHost is replaced in tests, which run against a local API.
var completionHost = cmdutil.IsDefaultHost

const (
	// completionTTL keeps Tab fast on repeated presses without hiding a new environment for long.
	completionTTL = time.Minute
	// completionTimeout bounds git and the API calls, so a slow network does not hang the prompt.
	completionTimeout = 5 * time.Second
)

// addEnvironmentCompletion completes every [environment] or <environment> argument and --environment flag below root.
func addEnvironmentCompletion(root *cobra.Command, factory *app.Factory) {
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if index := environmentArg(command.Use); index >= 0 && command.ValidArgsFunction == nil {
			command.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
				if len(args) != index {
					return nil, cobra.ShellCompDirectiveDefault
				}
				return completeEnvironments(cmd, factory, toComplete)
			}
		}
		command.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Name == "environment" {
				_ = command.RegisterFlagCompletionFunc(flag.Name, func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
					return completeEnvironments(cmd, factory, toComplete)
				})
			}
		})
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func environmentArg(use string) int {
	fields := strings.Fields(use)
	if len(fields) < 2 {
		return -1
	}
	index := 0
	for _, word := range fields[1:] {
		if strings.HasPrefix(word, "[--") || !(strings.HasPrefix(word, "<") || strings.HasPrefix(word, "[")) {
			continue
		}
		if word == "[environment]" || word == "<environment>" {
			return index
		}
		index++
	}
	return -1
}

func completeEnvironments(cmd *cobra.Command, factory *app.Factory, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()
	var completions []cobra.Completion
	add := func(value, description string) {
		value, description = iostreams.SingleLine(value), iostreams.SingleLine(description)
		if strings.HasPrefix(strings.ToLower(value), strings.ToLower(toComplete)) {
			completions = append(completions, cobra.CompletionWithDesc(value, description))
		}
	}
	if dir, err := os.Getwd(); err == nil {
		if entries, err := envlink.ReadFile(envlink.Resolver{}.Root(ctx, dir)); err == nil {
			for _, label := range entries.Labels() {
				id, _ := entries.Get(label)
				add(label, id+", from "+envlink.FilePath)
			}
		}
	}
	for _, candidate := range completionCandidates(ctx, cmd, factory) {
		if candidate.AppName != "" {
			add(candidate.Name(), candidate.EnvironmentID)
		}
		if strings.HasPrefix(toComplete, "en-") {
			add(candidate.EnvironmentID, candidate.Name())
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// completionCandidates lists the environments of the account from a cache per API host and credential, or the API; nil on any error.
func completionCandidates(ctx context.Context, cmd *cobra.Command, factory *app.Factory) []envlink.Candidate {
	host, err := cmdutil.Host(cmd, factory)
	// Tab must not send the token to a host the person has not confirmed, as a command run would warn first.
	if err != nil || !completionHost(host) {
		return nil
	}
	profile, err := cmdutil.Profile(cmd)
	if err != nil {
		return nil
	}
	credential, _, err := factory.Credential(profile)
	if err != nil || credential.AccessToken == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(host + "\x00" + profile + "\x00" + credential.AccessToken))
	path := ""
	if dir := cmdutil.CompletionCacheDir(); dir != "" {
		path = filepath.Join(dir, hex.EncodeToString(sum[:16])+".json")
	}
	if candidates, ok := readCompletionCache(path); ok {
		return candidates
	}
	client, err := cmdutil.CredentialClient(factory, host, profile, credential)
	if err != nil {
		return nil
	}
	candidates, err := envlink.Candidates(ctx, client)
	if err != nil {
		return nil
	}
	writeCompletionCache(path, candidates)
	return candidates
}

func readCompletionCache(path string) ([]envlink.Candidate, bool) {
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if age := time.Since(info.ModTime()); age < 0 || age > completionTTL {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var candidates []envlink.Candidate
	if json.Unmarshal(raw, &candidates) != nil {
		return nil, false
	}
	return candidates, true
}

// writeCompletionCache stores candidates readable by the owner only, as they name the apps of the account, and drops stale files left by earlier tokens.
func writeCompletionCache(path string, candidates []envlink.Candidate) {
	if path == "" || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	if stale, err := os.ReadDir(filepath.Dir(path)); err == nil {
		for _, entry := range stale {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > completionTTL {
				_ = os.Remove(filepath.Join(filepath.Dir(path), entry.Name()))
			}
		}
	}
	_ = cmdutil.WriteFileAtomic(path, 0o600, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(candidates)
	})
}
