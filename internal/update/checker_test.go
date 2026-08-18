package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestLatestReturnsNewerReleaseAndUsesCache(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tag_name":"v1.2.0"}`))
	}))
	defer server.Close()

	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	checker := Checker{
		Client:    server.Client(),
		Endpoint:  server.URL,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		Now:       func() time.Time { return now },
	}

	for range 2 {
		latest, err := checker.Latest(context.Background(), "1.1.0")
		if err != nil {
			t.Fatal(err)
		}
		if latest != "v1.2.0" {
			t.Fatalf("latest = %q, want v1.2.0", latest)
		}
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestLatestIgnoresDevelopmentAndCurrentVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"tag_name":"v1.2.0"}`))
	}))
	defer server.Close()

	checker := Checker{Client: server.Client(), Endpoint: server.URL, Now: time.Now}
	for _, current := range []string{"dev", "1.2.0", "1.3.0", "1.2.0-rc.1"} {
		latest, err := checker.Latest(context.Background(), current)
		if err != nil {
			t.Fatal(err)
		}
		if latest != "" {
			t.Fatalf("current %q: latest = %q, want empty", current, latest)
		}
	}
}
