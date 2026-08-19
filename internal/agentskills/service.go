package agentskills

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const sourceMarker = ".frbit-skill-source"

var legacySkillNames = map[string]bool{
	"fortrabbit":            true,
	"fortrabbit-api-access": true,
}

type Service struct {
	client     *http.Client
	source     Source
	homeDir    func() (string, error)
	workingDir func() (string, error)
	now        func() time.Time
	userAgent  string
}

type Options struct {
	HTTPClient *http.Client
	Source     Source
	HomeDir    func() (string, error)
	WorkingDir func() (string, error)
	Now        func() time.Time
	UserAgent  string
}

func NewService(options Options) *Service {
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	source := options.Source
	if source.ReleaseURL == "" || source.ArchiveURL == nil {
		source = DefaultSource()
	}
	homeDir := options.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	workingDir := options.WorkingDir
	if workingDir == nil {
		workingDir = os.Getwd
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	userAgent := options.UserAgent
	if userAgent == "" {
		userAgent = "frbit/dev"
	}
	return &Service{
		client:     client,
		source:     source,
		homeDir:    homeDir,
		workingDir: workingDir,
		now:        now,
		userAgent:  userAgent,
	}
}

func (s *Service) InstallTargets(project bool, requested []Agent) ([]Target, error) {
	return s.targets(project, requested, true)
}

func (s *Service) InspectionTargets(project bool, requested []Agent) ([]Target, error) {
	return s.targets(project, requested, false)
}

func (s *Service) targets(project bool, requested []Agent, detect bool) ([]Target, error) {
	home, err := s.homeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	workDir, err := s.workingDir()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	return resolveTargets(home, workDir, project, requested, detect)
}

func (s *Service) InstallRelease(ctx context.Context, release Release, targets []Target) ([]Installation, error) {
	packageContents, err := s.fetchPayload(ctx, release)
	if err != nil {
		return nil, err
	}

	for _, target := range targets {
		switch target.Agent {
		case AgentClaudeCode, AgentCodex:
			if err := s.installSkills(target, packageContents); err != nil {
				return nil, fmt.Errorf("install skills for %s: %w", target.Agent.Label(), err)
			}
		case AgentCopilot:
			if !packageContents.hasCopilot {
				return nil, fmt.Errorf("skills release does not contain GitHub Copilot instructions")
			}
			if err := installCopilot(target, packageContents); err != nil {
				return nil, fmt.Errorf("install skills for %s: %w", target.Agent.Label(), err)
			}
		default:
			return nil, fmt.Errorf("unsupported target agent %q", target.Agent)
		}
	}
	return s.InspectTargets(targets)
}

func (s *Service) Inspect(project bool, requested []Agent) ([]Installation, error) {
	targets, err := s.InspectionTargets(project, requested)
	if err != nil {
		return nil, err
	}
	return s.InspectTargets(targets)
}

func (s *Service) InspectTargets(targets []Target) ([]Installation, error) {
	var installations []Installation
	for _, target := range targets {
		items, err := inspectTarget(target)
		if err != nil {
			return nil, err
		}
		installations = append(installations, items...)
	}
	sortInstallations(installations)
	return installations, nil
}

func (s *Service) Removals(project bool, requested []Agent) ([]Removal, error) {
	targets, err := s.InspectionTargets(project, requested)
	if err != nil {
		return nil, err
	}
	var removals []Removal
	for _, target := range targets {
		installations, err := inspectTarget(target)
		if err != nil {
			return nil, err
		}
		for _, installation := range installations {
			removal := Removal{Agent: installation.Agent, Path: installation.Path}
			if installation.Agent == AgentCopilot {
				removal.Additional = []string{target.VersionFile}
			} else {
				removal.Directory = true
			}
			removals = append(removals, removal)
		}
	}
	sort.Slice(removals, func(i, j int) bool {
		if removals[i].Agent != removals[j].Agent {
			return removals[i].Agent < removals[j].Agent
		}
		return removals[i].Path < removals[j].Path
	})
	return removals, nil
}

func (s *Service) Remove(removals []Removal) error {
	for _, removal := range removals {
		var err error
		if removal.Directory {
			err = os.RemoveAll(removal.Path)
		} else {
			err = os.Remove(removal.Path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return fmt.Errorf("remove %s: %w", removal.Path, err)
		}
		for _, extra := range removal.Additional {
			if err := os.Remove(extra); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", extra, err)
			}
		}
	}
	return nil
}

func (s *Service) installSkills(target Target, packageContents payload) error {
	names := make([]string, 0, len(packageContents.skills))
	for name := range packageContents.skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		files := make(map[string]archiveFile, len(packageContents.skills[name])+5)
		for relativePath, file := range packageContents.skills[name] {
			files[relativePath] = file
		}
		files[".version"] = archiveFile{contents: []byte(packageContents.version + "\n"), mode: 0o644}
		files[sourceMarker] = archiveFile{contents: []byte("https://github.com/fortrabbit/agent-skills\n"), mode: 0o644}
		files[".last-update-check"] = archiveFile{
			contents: []byte(fmt.Sprintf("%d\n", s.now().Unix())),
			mode:     0o644,
		}
		if packageContents.hasUpdate {
			file := packageContents.updateScript
			file.mode = 0o755
			files["update.sh"] = file
		}
		if packageContents.hasUninstaller {
			file := packageContents.removeScript
			file.mode = 0o755
			files["uninstall.sh"] = file
		}
		if err := replaceDirectory(filepath.Join(target.SkillsBase, name), files); err != nil {
			return err
		}
	}
	return nil
}

func inspectTarget(target Target) ([]Installation, error) {
	if target.Agent == AgentCopilot {
		if _, err := os.Stat(target.InstructionsFile); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, fmt.Errorf("inspect %s: %w", target.InstructionsFile, err)
		}
		return []Installation{{
			Agent:   target.Agent,
			Skill:   "fortrabbit",
			Scope:   target.Scope,
			Version: readVersion(target.VersionFile),
			Path:    target.InstructionsFile,
		}}, nil
	}

	entries, err := os.ReadDir(target.SkillsBase)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect %s: %w", target.SkillsBase, err)
	}
	var installations []Installation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(target.SkillsBase, entry.Name())
		if !isFortrabbitSkill(directory, entry.Name()) {
			continue
		}
		installations = append(installations, Installation{
			Agent:   target.Agent,
			Skill:   entry.Name(),
			Scope:   target.Scope,
			Version: readVersion(filepath.Join(directory, ".version")),
			Path:    directory,
		})
	}
	return installations, nil
}

func isFortrabbitSkill(directory string, name string) bool {
	if _, err := os.Stat(filepath.Join(directory, sourceMarker)); err == nil {
		return true
	}
	if !legacySkillNames[name] {
		return false
	}
	_, err := os.Stat(filepath.Join(directory, "SKILL.md"))
	return err == nil
}

func readVersion(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(contents)) == "" {
		return "unknown"
	}
	return strings.TrimSpace(string(contents))
}
