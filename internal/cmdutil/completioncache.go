package cmdutil

import (
	"os"
	"path/filepath"
)

// CompletionCacheDir holds the environment names cached for shell completion; "" without a user cache folder.
func CompletionCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "frbit", "completion")
}

// ClearCompletionCache removes the cached names, so they do not outlive a sign-in.
func ClearCompletionCache() {
	if dir := CompletionCacheDir(); dir != "" {
		_ = os.RemoveAll(dir)
	}
}
