package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://api.github.com/repos/fortrabbit/frbit-cli/releases/latest"
	cacheLifetime   = 24 * time.Hour
)

type Checker struct {
	Client    *http.Client
	Endpoint  string
	CachePath string
	Now       func() time.Time
}

type cacheEntry struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest"`
}

func NewChecker(client *http.Client) Checker {
	cachePath := ""
	if cacheDir, err := os.UserCacheDir(); err == nil {
		cachePath = filepath.Join(cacheDir, "frbit", "update-check.json")
	}

	return Checker{
		Client:    client,
		Endpoint:  defaultEndpoint,
		CachePath: cachePath,
		Now:       time.Now,
	}
}

// Latest returns a newer stable release, or an empty string when current is up to date.
func (c Checker) Latest(ctx context.Context, current string) (string, error) {
	currentVersion, ok := parseVersion(current)
	if !ok {
		return "", nil
	}

	now := c.Now()
	if cached, ok := c.readCache(); ok && now.Sub(cached.CheckedAt) >= 0 && now.Sub(cached.CheckedAt) < cacheLifetime {
		return newerVersion(currentVersion, cached.Latest), nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "frbit/"+strings.TrimPrefix(current, "v"))

	response, err := c.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check latest release: %s", response.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	if _, ok := parseVersion(release.TagName); !ok {
		return "", errors.New("latest release has an invalid version")
	}

	c.writeCache(cacheEntry{CheckedAt: now, Latest: release.TagName})
	return newerVersion(currentVersion, release.TagName), nil
}

func (c Checker) readCache() (cacheEntry, bool) {
	var entry cacheEntry
	if c.CachePath == "" {
		return entry, false
	}
	data, err := os.ReadFile(c.CachePath)
	if err != nil || json.Unmarshal(data, &entry) != nil || entry.CheckedAt.IsZero() {
		return cacheEntry{}, false
	}
	return entry, true
}

func (c Checker) writeCache(entry cacheEntry) {
	if c.CachePath == "" {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil || os.MkdirAll(filepath.Dir(c.CachePath), 0o700) != nil {
		return
	}
	_ = os.WriteFile(c.CachePath, data, 0o600)
}

func newerVersion(current [3]int, latest string) string {
	latestVersion, ok := parseVersion(latest)
	if !ok {
		return ""
	}
	for index := range current {
		if latestVersion[index] > current[index] {
			return "v" + strings.TrimPrefix(latest, "v")
		}
		if latestVersion[index] < current[index] {
			return ""
		}
	}
	return ""
}

func parseVersion(value string) ([3]int, bool) {
	var version [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" || strings.Contains(value, "-") || strings.Contains(value, "+") {
		return version, false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return version, false
	}
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return [3]int{}, false
		}
		version[index] = parsed
	}
	return version, true
}
