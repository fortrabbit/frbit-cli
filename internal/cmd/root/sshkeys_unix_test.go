//go:build unix

package root

import (
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSSHKeysAddRefusesAFIFOWithoutBlocking(t *testing.T) {
	home := sshHome(t, nil)
	path := filepath.Join(home, ".ssh", "fifo.pub")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip(err)
	}
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add", path)
	if err == nil || !strings.Contains(err.Error(), "is not a public key file") {
		t.Fatalf("err = %v", err)
	}
}
