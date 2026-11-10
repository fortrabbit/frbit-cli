package sshexec

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPinnedKeyFingerprint(t *testing.T) {
	fields := strings.Fields(PinnedHostKey)
	if len(fields) != 2 || fields[0] != "ssh-ed25519" {
		t.Fatalf("PinnedHostKey = %q", PinnedHostKey)
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(blob)
	if got := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]); got != "SHA256:UB3p/YgtQSuK3WESB/0ra5dTDWkggrCOz0WkWe4Tvvg" {
		t.Fatalf("fingerprint = %s", got)
	}
}

func TestPinned(t *testing.T) {
	for host, want := range map[string]bool{
		"ssh.eu-w1a.frbit.app": true, "SSH.us-e1a.FRBIT.app": true, "en-x@ssh.eu-w1a.frbit.app": true,
		"ssh.dev.frbit.cloud": false, "ssh.frbit.app": false, "ssh.a.b.frbit.app": false, "evil.frbit.app.example.com": false, "xssh.eu-w1a.frbit.app": false,
	} {
		if got := Pinned(host); got != want {
			t.Errorf("Pinned(%q) = %v", host, got)
		}
	}
}

func useKnownHosts(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conf dir", "frbit", "known_hosts")
	previous := KnownHostsPath
	KnownHostsPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { KnownHostsPath = previous })
	return path
}

func TestHostOptionsPinFrbitHosts(t *testing.T) {
	path := useKnownHosts(t)
	options, err := Options("en-abc123@ssh.eu-w1a.frbit.app", false)
	if err != nil {
		t.Fatal(err)
	}
	quoted := `"` + filepath.ToSlash(path) + `"`
	for _, want := range []string{"UserKnownHostsFile=" + quoted, "GlobalKnownHostsFile=" + quoted, "StrictHostKeyChecking=yes", "CheckHostIP=no", "VerifyHostKeyDNS=no", "UpdateHostKeys=no", "HostKeyAlias=ssh.eu-w1a.frbit.app", "BatchMode=yes"} {
		if !slices.Contains(options, want) {
			t.Errorf("options lack %q: %q", want, options)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "ssh.*.frbit.app "+PinnedHostKey+"\n" {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	// A changed file is rewritten.
	if err := os.WriteFile(path, []byte("ssh.*.frbit.app ssh-ed25519 AAAAother\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Options("en-abc123@ssh.eu-w1a.frbit.app", true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "ssh.*.frbit.app "+PinnedHostKey+"\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("not rewritten: %q %v", data, info.Mode())
	}
}

func TestHostOptionsLeaveOtherHostsAlone(t *testing.T) {
	path := useKnownHosts(t)
	options, err := Options("en-abc123@ssh.dev.frbit.cloud", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		if strings.Contains(option, "KnownHosts") || strings.Contains(option, "StrictHostKeyChecking") {
			t.Fatalf("pinned a foreign host: %q", options)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("known_hosts written for a foreign host")
	}
}

func TestRshCommandSurvivesRsyncSplitting(t *testing.T) {
	rsync, err := exec.LookPath("rsync")
	if err != nil {
		t.Skip("rsync not installed")
	}
	path := useKnownHosts(t)
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >&2; done\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rsh, err := RshCommand("en-abc123@ssh.eu-w1a.frbit.app", false)
	if err != nil {
		t.Fatal(err)
	}
	// Swap the program for the fake, keeping rsh's own quoting of the options.
	rsh = "'" + fake + "'" + strings.TrimPrefix(rsh, "ssh")
	source := filepath.Join(dir, "f")
	_ = os.WriteFile(source, nil, 0o600)
	output, _ := exec.Command(rsync, "-e", rsh, source, "en-abc123@ssh.eu-w1a.frbit.app:/x").CombinedOutput()
	if !strings.Contains(string(output), "\nUserKnownHostsFile=\""+filepath.ToSlash(path)+"\"\n") {
		t.Fatalf("rsync passed:\n%s", output)
	}
}
