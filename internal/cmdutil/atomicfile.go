package cmdutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes path through a temp file next to it, so a failed write never leaves a partial file under the real name.
// The .frbit- prefix is what files push excludes, and the temp file starts out 0600.
func WriteFileAtomic(path string, mode os.FileMode, write func(io.Writer) error) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".frbit-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(file.Name())
		}
	}()
	err = write(file)
	if err == nil {
		err = file.Chmod(mode)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// OutputPath is output, or defaultName inside output when output is empty or a folder; without force an existing file is an error.
func OutputPath(output, defaultName string, force bool) (string, error) {
	path := output
	if info, err := os.Stat(output); output == "" || (err == nil && info.IsDir()) {
		path = filepath.Join(output, defaultName)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return "", fmt.Errorf("folder %s does not exist", filepath.Dir(path))
	}
	if !force {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return "", fmt.Errorf("%s exists; pass --force to overwrite", path)
		}
	}
	return path, nil
}
