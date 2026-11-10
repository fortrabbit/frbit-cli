package sshexec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
)

// PinnedHostKey is the host key of fortrabbit's production SSH endpoints (SHA256:UB3p/YgtQSuK3WESB/0ra5dTDWkggrCOz0WkWe4Tvvg).
const PinnedHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOpr+rjgkjbqn8YpDvV1rfK7NN5y62LaBh1Qy2QgWjr0"

// pinnedPattern is the known_hosts pattern written for PinnedHostKey.
const pinnedPattern = "ssh.*.frbit.app"

// pinnedHost matches one region label only, so it never pins more than the known_hosts pattern covers.
var pinnedHost = regexp.MustCompile(`(?i)^ssh\.[a-z0-9-]+\.frbit\.app$`)

// KnownHostsPath returns frbit's own known_hosts file; tests replace it.
var KnownHostsPath = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "frbit", "known_hosts"), nil
}

// host returns the host part of a user@host target.
func host(target string) string {
	if _, h, ok := strings.Cut(target, "@"); ok {
		return h
	}
	return target
}

// Pinned reports whether the host of target is a production endpoint checked against PinnedHostKey.
func Pinned(target string) bool {
	return pinnedHost.MatchString(host(target))
}

// pinOptions writes the pinned known_hosts file and returns the ssh options that check target against it alone.
// ssh accepts a host key found in any listed known_hosts file, so the user's files are left out: a stale or planted entry there must not be accepted for these hosts.
func pinOptions(target string) ([]string, error) {
	path, err := KnownHostsPath()
	if err != nil {
		return nil, err
	}
	if err := writeKnownHosts(path); err != nil {
		return nil, err
	}
	file := filepath.ToSlash(path)
	if strings.ContainsAny(file, `"'`) {
		return nil, fmt.Errorf("cannot pin the fortrabbit host key: %s contains a quote", file)
	}
	file = `"` + file + `"`
	return []string{
		"-o", "UserKnownHostsFile=" + file,
		"-o", "GlobalKnownHostsFile=" + file,
		"-o", "StrictHostKeyChecking=yes",
		"-o", "CheckHostIP=no",
		"-o", "VerifyHostKeyDNS=no",
		"-o", "UpdateHostKeys=no",
		"-o", "HostKeyAlias=" + strings.ToLower(host(target)),
	}, nil
}

func writeKnownHosts(path string) error {
	content := []byte(pinnedPattern + " " + PinnedHostKey + "\n")
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) {
		return os.Chmod(path, 0o600)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := cmdutil.WriteFileAtomic(path, 0o600, func(out io.Writer) error {
		_, err := out.Write(content)
		return err
	}); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// RshCommand is the ssh command line for rsync -e; options with spaces or quotes are single-quoted, which GNU rsync and openrsync both split on.
func RshCommand(target string, interactive bool) (string, error) {
	options, err := Options(target, interactive)
	if err != nil {
		return "", err
	}
	parts := []string{"ssh"}
	for _, option := range options {
		if strings.ContainsAny(option, " \t\"") {
			option = "'" + option + "'"
		}
		parts = append(parts, option)
	}
	return strings.Join(parts, " "), nil
}
