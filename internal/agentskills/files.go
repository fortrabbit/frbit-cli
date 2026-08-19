package agentskills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func replaceDirectory(target string, files map[string]archiveFile) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", parent, err)
	}
	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(target)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary skill directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o755); err != nil {
		return fmt.Errorf("set permissions on temporary skill directory: %w", err)
	}

	paths := make([]string, 0, len(files))
	for relativePath := range files {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)
	for _, relativePath := range paths {
		if err := writeArchiveFile(temporary, relativePath, files[relativePath]); err != nil {
			return err
		}
	}
	return replacePath(temporary, target)
}

func installCopilot(target Target, packageContents payload) error {
	if err := writeFileAtomically(target.InstructionsFile, packageContents.copilot.contents, 0o644); err != nil {
		return err
	}
	return writeFileAtomically(target.VersionFile, []byte(packageContents.version+"\n"), 0o644)
}

func writeArchiveFile(root string, relativePath string, file archiveFile) error {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing unsafe skill path %q", relativePath)
	}
	destination := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destination), err)
	}
	mode := os.FileMode(file.mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.WriteFile(destination, file.contents, mode); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}
	return nil
}

func writeFileAtomically(target string, contents []byte, mode os.FileMode) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", parent, err)
	}
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(target)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", target, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set permissions on %s: %w", temporaryPath, err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %s: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temporaryPath, err)
	}
	return replacePath(temporaryPath, target)
}

func replacePath(source string, target string) error {
	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			if err := os.Rename(source, target); err != nil {
				return fmt.Errorf("install %s: %w", target, err)
			}
			return nil
		}
		return fmt.Errorf("inspect %s: %w", target, err)
	}

	backupFile, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".backup-")
	if err != nil {
		return fmt.Errorf("prepare backup for %s: %w", target, err)
	}
	backup := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		return fmt.Errorf("close backup placeholder: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("prepare backup path: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		return fmt.Errorf("back up %s: %w", target, err)
	}
	if err := os.Rename(source, target); err != nil {
		_ = os.Rename(backup, target)
		return fmt.Errorf("install %s: %w", target, err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove backup %s: %w", backup, err)
	}
	return nil
}
