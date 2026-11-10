package link

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// FolderName holds deploy.yml too; this package never touches that file because the API rejects unknown keys in it.
	FolderName = ".fortrabbit"
	FileName   = "environments.yml"
	// FilePath is the label → environment map relative to the project root.
	FilePath = FolderName + "/" + FileName
)

var (
	idPattern    = regexp.MustCompile(`^en-[a-z0-9]+$`)
	labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

	// ErrLegacyFile means .fortrabbit is a file from an older setup, which blocks the .fortrabbit folder.
	ErrLegacyFile = errors.New(".fortrabbit is a file from an older setup; run frbit link on a terminal to migrate .fortrabbit")
)

// IsID reports whether value is an environment public ID rather than a label.
func IsID(value string) bool { return idPattern.MatchString(value) }

// ValidLabel checks that label can be written unquoted into the file and is not mistaken for an ID.
func ValidLabel(label string) error {
	if !labelPattern.MatchString(label) || strings.HasPrefix(label, "en-") {
		return fmt.Errorf("invalid label %q: use letters, digits, '.', '_', '/' or '-', not starting with en-", label)
	}
	return nil
}

// Entries is the parsed environments.yml; it keeps comments and order so a rewrite only changes the touched line.
type Entries struct {
	lines  []string
	labels []string
	ids    map[string]string
	index  map[string]int
}

func (e *Entries) Get(label string) (string, bool) {
	id, ok := e.ids[label]
	return id, ok
}

// Labels returns the labels in file order.
func (e *Entries) Labels() []string { return append([]string(nil), e.labels...) }

func (e *Entries) Len() int { return len(e.labels) }

// Set adds label or replaces its ID in place.
func (e *Entries) Set(label, id string) error {
	if err := ValidLabel(label); err != nil {
		return err
	}
	if !IsID(id) {
		return fmt.Errorf("invalid environment ID %q", id)
	}
	line := label + ": " + id
	if i, ok := e.index[label]; ok {
		e.lines[i] = line
	} else {
		e.index[label] = len(e.lines)
		e.lines = append(e.lines, line)
		e.labels = append(e.labels, label)
	}
	e.ids[label] = id
	return nil
}

func newEntries() *Entries {
	return &Entries{ids: map[string]string{}, index: map[string]int{}}
}

// ReadFile reads dir/.fortrabbit/environments.yml; a missing file gives empty entries, a .fortrabbit file gives ErrLegacyFile.
func ReadFile(dir string) (*Entries, error) {
	if legacy, err := isLegacy(dir); err != nil {
		return nil, err
	} else if legacy {
		return nil, ErrLegacyFile
	}
	raw, err := os.ReadFile(filepath.Join(dir, FolderName, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return newEntries(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", FilePath, err)
	}
	return parse(string(raw))
}

func parse(content string) (*Entries, error) {
	entries := newEntries()
	scanner := bufio.NewScanner(strings.NewReader(content))
	number := 0
	for scanner.Scan() {
		number++
		line := strings.TrimRight(scanner.Text(), " \t\r")
		entries.lines = append(entries.lines, line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || (number == 1 && trimmed == "---") {
			continue
		}
		label, id, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", FilePath, number, err)
		}
		if _, dup := entries.ids[label]; dup {
			return nil, fmt.Errorf("%s line %d: label %q appears twice", FilePath, number, label)
		}
		entries.index[label] = len(entries.lines) - 1
		entries.labels = append(entries.labels, label)
		entries.ids[label] = id
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", FilePath, err)
	}
	return entries, nil
}

var errLineShape = errors.New(`expected a flat map of "label: en-xxxxxx" lines`)

func parseLine(line string) (string, string, error) {
	if line[0] == ' ' || line[0] == '\t' {
		return "", "", errLineShape
	}
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", errLineShape
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = value[:i]
	}
	label, id := unquote(strings.TrimSpace(key)), unquote(strings.TrimSpace(value))
	if !labelPattern.MatchString(label) || !IsID(id) {
		return "", "", errLineShape
	}
	return label, id, nil
}

func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// WriteFile writes entries to dir/.fortrabbit/environments.yml, creating the folder; other files in it stay untouched.
func WriteFile(dir string, entries *Entries) error {
	for _, label := range entries.labels {
		if id := entries.ids[label]; !labelPattern.MatchString(label) || !IsID(id) {
			return fmt.Errorf("write %s: invalid entry %q: %q", FilePath, label, id)
		}
	}
	folder := filepath.Join(dir, FolderName)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", FolderName, err)
	}
	content := strings.Join(entries.lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	temp, err := os.CreateTemp(folder, ".environments-*.yml")
	if err != nil {
		return fmt.Errorf("write %s: %w", FilePath, err)
	}
	defer os.Remove(temp.Name())
	if _, err := temp.WriteString(content); err != nil {
		temp.Close()
		return fmt.Errorf("write %s: %w", FilePath, err)
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return fmt.Errorf("write %s: %w", FilePath, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", FilePath, err)
	}
	if err := os.Rename(temp.Name(), filepath.Join(folder, FileName)); err != nil {
		return fmt.Errorf("write %s: %w", FilePath, err)
	}
	return nil
}

func isLegacy(dir string) (bool, error) {
	info, err := os.Stat(filepath.Join(dir, FolderName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", FolderName, err)
	}
	return !info.IsDir(), nil
}

// LegacyID returns the app-env-id of a legacy .fortrabbit file; ok is false when there is no such file or no ID in it.
func LegacyID(dir string) (string, bool, error) {
	legacy, err := isLegacy(dir)
	if err != nil || !legacy {
		return "", false, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, FolderName))
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", FolderName, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(key) == "app-env-id" {
			id := unquote(strings.TrimSpace(value))
			if IsID(id) {
				return id, true, nil
			}
		}
	}
	return "", false, nil
}

// MigrateLegacy replaces the legacy .fortrabbit file with the folder and stores its ID under label.
func MigrateLegacy(dir, label string) (string, error) {
	if err := ValidLabel(label); err != nil {
		return "", err
	}
	id, ok, err := LegacyID(dir)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New(".fortrabbit is a file without app-env-id=; move it away and run frbit link")
	}
	legacy := filepath.Join(dir, FolderName)
	backup := legacy + ".migrating"
	if err := os.Rename(legacy, backup); err != nil {
		return "", fmt.Errorf("migrate %s: %w", FolderName, err)
	}
	entries := newEntries()
	if err := entries.Set(label, id); err != nil {
		return "", err
	}
	if err := WriteFile(dir, entries); err != nil {
		_ = os.RemoveAll(legacy)
		_ = os.Rename(backup, legacy)
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return "", fmt.Errorf("migrate %s: %w", FolderName, err)
	}
	return id, nil
}
