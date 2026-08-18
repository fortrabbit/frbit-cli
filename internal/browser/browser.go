package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open asks the operating system to open rawURL in the default browser.
func Open(rawURL string) error {
	name, args, err := command(runtime.GOOS, rawURL)
	if err != nil {
		return err
	}

	process := exec.Command(name, args...)
	if err := process.Start(); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}
	if err := process.Process.Release(); err != nil {
		return fmt.Errorf("release browser process: %w", err)
	}

	return nil
}

func command(goos string, rawURL string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{rawURL}, nil
	case "linux":
		return "xdg-open", []string{rawURL}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}, nil
	default:
		return "", nil, fmt.Errorf("open browser: unsupported operating system %q", goos)
	}
}
