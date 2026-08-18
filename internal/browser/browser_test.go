package browser

import (
	"reflect"
	"testing"
)

func TestCommand(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		wantName string
		wantArgs []string
	}{
		{name: "macOS", goos: "darwin", wantName: "open", wantArgs: []string{"https://example.com"}},
		{name: "Linux", goos: "linux", wantName: "xdg-open", wantArgs: []string{"https://example.com"}},
		{name: "Windows", goos: "windows", wantName: "rundll32", wantArgs: []string{"url.dll,FileProtocolHandler", "https://example.com"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, args, err := command(test.goos, "https://example.com")
			if err != nil {
				t.Fatalf("command() error = %v", err)
			}
			if name != test.wantName {
				t.Fatalf("command() name = %q, want %q", name, test.wantName)
			}
			if !reflect.DeepEqual(args, test.wantArgs) {
				t.Fatalf("command() args = %#v, want %#v", args, test.wantArgs)
			}
		})
	}
}

func TestCommandRejectsUnsupportedOperatingSystem(t *testing.T) {
	if _, _, err := command("plan9", "https://example.com"); err == nil {
		t.Fatal("command() error = nil, want an unsupported operating system error")
	}
}
