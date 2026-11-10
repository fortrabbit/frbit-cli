package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/sshexec"
)

// TestMain keeps the pinned known_hosts file in a temp folder instead of the user's config dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "frbit-db-test-")
	if err != nil {
		panic(err)
	}
	sshexec.KnownHostsPath = func() (string, error) { return filepath.Join(dir, "known_hosts"), nil }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
