package update

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Method is the way frbit was installed, read from where its binary lives.
type Method int

const (
	MethodUnknown Method = iota
	MethodHomebrew
	MethodNPM
	MethodScript
)

// installMethod names a method only where it is certain; anything else, such as npx, pnpm, mise or Nix, is unknown.
func installMethod(path, home string, exists func(string) bool) Method {
	slashed := filepath.ToSlash(path)
	dir := filepath.ToSlash(filepath.Dir(path))
	npmPrefix, _, npmLayout := strings.Cut(slashed, "/lib/node_modules/@fortrabbit/")
	switch {
	case strings.Contains(slashed, "/Cellar/frbit/"):
		return MethodHomebrew
	case strings.Contains(slashed, "/_npx/"), strings.Contains(slashed, "/.pnpm/"), strings.Contains(slashed, "/.volta/"):
		return MethodUnknown
	// A global npm install keeps the package in <prefix>/lib/node_modules and links it as <prefix>/bin/frbit; a project one does not.
	case npmLayout:
		if exists(npmPrefix + "/bin/frbit") {
			return MethodNPM
		}
		return MethodUnknown
	case dir == "/usr/local/bin", home != "" && dir == filepath.ToSlash(filepath.Join(home, ".local", "bin")):
		return MethodScript
	}
	return MethodUnknown
}

// InstallDocsURL documents every install method and how to upgrade it.
const InstallDocsURL = "https://docs.fortrabbit.com/platform/automation/cli/install#updates"

const installScript = "curl -fsSL https://www.fortrabbit.com/install.sh | "

// Upgrade returns the shell command that upgrades the running frbit, or the docs link when no single command does.
func Upgrade() (command, docsURL string) {
	path, err := os.Executable()
	if err != nil {
		return "", InstallDocsURL
	}
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	home, _ := os.UserHomeDir()
	if target, err := filepath.EvalSymlinks(home); err == nil {
		home = target
	}
	dir := filepath.Dir(path)
	return upgrade(installMethod(path, home, lexists), runtime.GOOS, dir, writable(dir))
}

func upgrade(method Method, goos, dir string, canWrite bool) (command, docsURL string) {
	if goos == "windows" {
		return "", InstallDocsURL
	}
	switch method {
	case MethodHomebrew:
		return "brew upgrade frbit", ""
	case MethodNPM:
		if canWrite {
			return "npm install --global @fortrabbit/cli@latest", ""
		}
	case MethodScript:
		// Name the folder: install.sh's defaults can pick another one or take the binary for a brew or npm one.
		if canWrite && shellSafe.MatchString(dir) {
			return installScript + "FRBIT_INSTALL_DIR=" + dir + " sh", ""
		}
	}
	return "", InstallDocsURL
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func lexists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
