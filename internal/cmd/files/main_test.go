package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/sshexec"
)

// sshE and sshTTY are the rsync -e commands for the test environment's pinned host, without and with a terminal.
var sshE, sshTTY string

// TestMain keeps the pinned known_hosts file in a temp folder instead of the user's config dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "frbit-files-test-")
	if err != nil {
		panic(err)
	}
	sshexec.KnownHostsPath = func() (string, error) { return filepath.Join(dir, "known_hosts"), nil }
	if sshE, err = sshexec.RshCommand("en-abc123@ssh.us-e1a.frbit.app", false); err != nil {
		panic(err)
	}
	if sshTTY, err = sshexec.RshCommand("en-abc123@ssh.us-e1a.frbit.app", true); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
