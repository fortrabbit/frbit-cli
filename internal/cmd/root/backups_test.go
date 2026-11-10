package root

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupsRestoreWithoutTTYRequiresConfirm(t *testing.T) {
	var calls []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/backups/ba-abc123":
			_, _ = w.Write([]byte(`{"publicId":"ba-abc123","environment":"en-abc123","status":"completed"}`))
		case "POST /v1/backups/ba-abc123/restore":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"publicId":"ba-abc123","status":"Pending"}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}
	_, err := runDomains(t, handler, "backups", "restore", "ba-abc123")
	if err == nil || !strings.Contains(err.Error(), "--confirm ba-abc123") || len(calls) != 0 {
		t.Fatalf("err = %v, calls = %v", err, calls)
	}
	_, err = runDomains(t, handler, "backups", "restore", "ba-abc123", "--confirm", "ba-other")
	if err == nil || len(calls) != 0 {
		t.Fatalf("err = %v, calls = %v", err, calls)
	}
	output, err := runDomains(t, handler, "backups", "restore", "ba-abc123", "--confirm", "ba-abc123")
	if err != nil || strings.Join(calls, ",") != "GET /v1/backups/ba-abc123,POST /v1/backups/ba-abc123/restore" {
		t.Fatalf("err = %v, calls = %v, output:\n%s", err, calls, output)
	}
}

func downloadHandler(t *testing.T, storage func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/backups/ba-abc123/download/files":
			_, _ = fmt.Fprintf(w, `{"publicId":"ba-abc123","type":"files","url":"http://%s/storage/site.zip?sig=1"}`, r.Host)
		case "/storage/site.zip":
			if r.Header.Get("Authorization") != "" {
				t.Errorf("API token sent to storage")
			}
			storage(w, r)
		default:
			t.Fatalf("unexpected %s", r.URL)
		}
	}
}

func TestBackupsDownloadWritesFileAtomically(t *testing.T) {
	dir := t.TempDir()
	_, err := runDomains(t, downloadHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
	}), "backups", "download", "ba-abc123", "--output", dir)
	if err == nil {
		t.Fatal("expected an error for a truncated body")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}

	_, err = runDomains(t, downloadHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("zipbytes"))
	}), "backups", "download", "ba-abc123", "--output", dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ba-abc123-files.zip"))
	if err != nil || string(data) != "zipbytes" {
		t.Fatalf("data = %q, err = %v", data, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("entries: %v", entries)
	}
}

func TestBackupsDownloadToStdout(t *testing.T) {
	output, err := runDomains(t, downloadHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("zipbytes"))
	}), "backups", "download", "ba-abc123", "--output", "-")
	if err != nil || output != "zipbytes" {
		t.Fatalf("output = %q, err = %v", output, err)
	}
}
