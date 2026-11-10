package root

import (
	"os"
	"testing"
)

// TestMain runs outside any git checkout, so commands that fall back to the checkout's environment find none.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "frbit-root-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	_ = os.Unsetenv("FORTRABBIT_ENVIRONMENT")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
