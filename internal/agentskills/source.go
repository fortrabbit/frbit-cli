package agentskills

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

const (
	defaultReleaseURL = "https://api.github.com/repos/fortrabbit/agent-skills/releases/latest"
	maxDownloadSize   = 20 << 20
	maxExpandedSize   = 50 << 20
)

type Source struct {
	ReleaseURL string
	ArchiveURL func(string) string
}

type Release struct {
	Tag     string
	Version string
}

type archiveFile struct {
	contents []byte
	mode     uint32
}

type payload struct {
	version        string
	skills         map[string]map[string]archiveFile
	copilot        archiveFile
	updateScript   archiveFile
	removeScript   archiveFile
	hasCopilot     bool
	hasUpdate      bool
	hasUninstaller bool
}

func DefaultSource() Source {
	return Source{
		ReleaseURL: defaultReleaseURL,
		ArchiveURL: func(tag string) string {
			return "https://github.com/fortrabbit/agent-skills/archive/refs/tags/" + tag + ".tar.gz"
		},
	}
}

func (s *Service) LatestRelease(ctx context.Context) (Release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.ReleaseURL, nil)
	if err != nil {
		return Release{}, fmt.Errorf("create skills release request: %w", err)
	}
	s.setRequestHeaders(request)

	response, err := s.client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest skills release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Release{}, fmt.Errorf("fetch latest skills release: %s", response.Status)
	}

	var document struct {
		TagName string `json:"tag_name"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&document); err != nil {
		return Release{}, fmt.Errorf("decode latest skills release: %w", err)
	}
	tag := strings.TrimSpace(document.TagName)
	version := strings.TrimPrefix(tag, "v")
	if tag == "" || version == "" || !validTag(tag) {
		return Release{}, fmt.Errorf("latest skills release returned invalid tag %q", tag)
	}
	return Release{Tag: tag, Version: version}, nil
}

func (s *Service) fetchPayload(ctx context.Context, release Release) (payload, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.ArchiveURL(release.Tag), nil)
	if err != nil {
		return payload{}, fmt.Errorf("create skills archive request: %w", err)
	}
	s.setRequestHeaders(request)

	response, err := s.client.Do(request)
	if err != nil {
		return payload{}, fmt.Errorf("download skills %s: %w", release.Tag, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return payload{}, fmt.Errorf("download skills %s: %s", release.Tag, response.Status)
	}

	limited := &io.LimitedReader{R: response.Body, N: maxDownloadSize + 1}
	gzipReader, err := gzip.NewReader(limited)
	if err != nil {
		return payload{}, fmt.Errorf("open skills archive: %w", err)
	}
	defer gzipReader.Close()

	result := payload{skills: make(map[string]map[string]archiveFile)}
	reader := tar.NewReader(gzipReader)
	var expanded int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return payload{}, fmt.Errorf("read skills archive: %w", err)
		}
		if header.Typeflag == tar.TypeDir || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return payload{}, fmt.Errorf("skills archive contains unsupported entry %q", header.Name)
		}
		name, ok := stripArchiveRoot(header.Name)
		if !ok || !wantedArchivePath(name) {
			continue
		}
		if header.Size < 0 || expanded+header.Size > maxExpandedSize {
			return payload{}, fmt.Errorf("skills archive expands beyond %d bytes", maxExpandedSize)
		}
		contents, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil {
			return payload{}, fmt.Errorf("read %q from skills archive: %w", name, err)
		}
		if int64(len(contents)) != header.Size {
			return payload{}, fmt.Errorf("skills archive entry %q has an invalid size", name)
		}
		expanded += int64(len(contents))
		file := archiveFile{contents: contents, mode: uint32(header.FileInfo().Mode().Perm())}

		switch {
		case name == "VERSION":
			result.version = strings.TrimSpace(string(contents))
		case name == "update.sh":
			result.updateScript, result.hasUpdate = file, true
		case name == "uninstall.sh":
			result.removeScript, result.hasUninstaller = file, true
		case name == ".github/instructions/fortrabbit.instructions.md":
			result.copilot, result.hasCopilot = file, true
		case strings.HasPrefix(name, "skills/"):
			parts := strings.SplitN(strings.TrimPrefix(name, "skills/"), "/", 2)
			if len(parts) != 2 || !validSkillName(parts[0]) || parts[1] == "" {
				continue
			}
			if result.skills[parts[0]] == nil {
				result.skills[parts[0]] = make(map[string]archiveFile)
			}
			result.skills[parts[0]][parts[1]] = file
		}
	}

	if limited.N <= 0 {
		return payload{}, fmt.Errorf("skills archive exceeds %d bytes", maxDownloadSize)
	}
	if result.version != release.Version {
		return payload{}, fmt.Errorf("skills archive version %q does not match release %q", result.version, release.Version)
	}
	if len(result.skills) == 0 {
		return payload{}, fmt.Errorf("skills archive does not contain any skills")
	}
	for name, files := range result.skills {
		if _, ok := files["SKILL.md"]; !ok {
			return payload{}, fmt.Errorf("skill %q does not contain SKILL.md", name)
		}
	}
	return result, nil
}

func (s *Service) setRequestHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", s.userAgent)
}

func validTag(tag string) bool {
	for _, character := range tag {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func stripArchiveRoot(name string) (string, bool) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false
	}
	parts := strings.SplitN(clean, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", false
	}
	clean = path.Clean(parts[1])
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false
	}
	return clean, true
}

func wantedArchivePath(name string) bool {
	return name == "VERSION" || name == "update.sh" || name == "uninstall.sh" ||
		name == ".github/instructions/fortrabbit.instructions.md" || strings.HasPrefix(name, "skills/")
}

// validSkillName deliberately uses a platform-independent allowlist. Archive
// paths always use slashes, but a backslash becomes a path separator on Windows.
func validSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}
