package cmdutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.sql.gz")
	var tempName string
	err := WriteFileAtomic(path, 0o600, func(out io.Writer) error {
		tempName = filepath.Base(out.(*os.File).Name())
		_, err := io.WriteString(out, "data")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tempName, ".frbit-") {
		t.Fatalf("temp name %q", tempName)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat: %v, %v", info, err)
	}
	failed := filepath.Join(dir, "failed")
	if err := WriteFileAtomic(failed, 0o600, func(io.Writer) error { return errors.New("boom") }); err == nil {
		t.Fatal("no error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %v", entries)
	}
}
