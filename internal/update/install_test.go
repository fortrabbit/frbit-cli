package update

import "testing"

func TestInstallMethod(t *testing.T) {
	for path, want := range map[string]Method{
		"/opt/homebrew/Cellar/frbit/1.21.0/bin/frbit":                                                     MethodHomebrew,
		"/usr/local/lib/node_modules/@fortrabbit/cli-darwin-arm64/bin/frbit":                              MethodNPM,
		"/usr/local/lib/node_modules/@fortrabbit/cli/node_modules/@fortrabbit/cli-darwin-arm64/bin/frbit": MethodNPM,
		"/work/app/lib/node_modules/@fortrabbit/cli-linux-x64/bin/frbit":                                  MethodUnknown,
		"/home/me/.volta/tools/image/packages/@fortrabbit/cli/lib/node_modules/@fortrabbit/cli/bin/frbit": MethodUnknown,
		"/home/me/.local/bin/frbit": MethodScript,
		"/usr/local/bin/frbit":      MethodScript,
		"/home/me/.npm/_npx/abc123/lib/node_modules/@fortrabbit/cli-linux-x64/bin/frbit":           MethodUnknown,
		"/home/me/project/node_modules/@fortrabbit/cli-linux-x64/bin/frbit":                        MethodUnknown,
		"/home/me/.local/share/pnpm/global/5/.pnpm/x/node_modules/@fortrabbit/cli-linux/bin/frbit": MethodUnknown,
		"/home/me/.local/share/mise/installs/frbit/1.21.0/frbit":                                   MethodUnknown,
		"/nix/store/abc-frbit-1.21.0/bin/frbit":                                                    MethodUnknown,
	} {
		if got := installMethod(path, "/home/me", func(p string) bool { return p == "/usr/local/bin/frbit" }); got != want {
			t.Errorf("installMethod(%s) = %v, want %v", path, got, want)
		}
	}
}

func TestUpgrade(t *testing.T) {
	for _, tc := range []struct {
		method       Method
		goos, dir    string
		canWrite     bool
		command, url string
	}{
		{MethodHomebrew, "darwin", "/opt/homebrew/Cellar/frbit/1.21.0/bin", true, "brew upgrade frbit", ""},
		{MethodNPM, "linux", "/usr/local/lib/node_modules/@fortrabbit/cli-linux-x64/bin", true, "npm install --global @fortrabbit/cli@latest", ""},
		{MethodNPM, "linux", "/usr/lib/node_modules/@fortrabbit/cli-linux-x64/bin", false, "", InstallDocsURL},
		{MethodScript, "linux", "/home/me/.local/bin", true, "curl -fsSL https://www.fortrabbit.com/install.sh | FRBIT_INSTALL_DIR=/home/me/.local/bin sh", ""},
		{MethodScript, "darwin", "/usr/local/bin", false, "", InstallDocsURL},
		{MethodScript, "darwin", "/Users/Jane Doe/.local/bin", true, "", InstallDocsURL},
		{MethodScript, "linux", "/home/a&b/.local/bin", true, "", InstallDocsURL},
		{MethodUnknown, "linux", "/nix/store/abc/bin", false, "", InstallDocsURL},
		{MethodNPM, "windows", "", true, "", InstallDocsURL},
	} {
		command, url := upgrade(tc.method, tc.goos, tc.dir, tc.canWrite)
		if command != tc.command || url != tc.url {
			t.Errorf("upgrade(%v, %s, %s) = %q, %q; want %q, %q", tc.method, tc.goos, tc.dir, command, url, tc.command, tc.url)
		}
	}
}
