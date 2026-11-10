package root

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/cmd/backups"
)

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

func TestBackupsDownloadCancelledLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(downloadHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 200; i++ {
			time.Sleep(10 * time.Millisecond)
			if entries, _ := os.ReadDir(dir); len(entries) > 0 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	command := NewCmdRoot(testFactory(&bytes.Buffer{}))
	command.SetArgs([]string{"--host", server.URL, "backups", "download", "ba-abc123", "--output", dir})
	if err := command.ExecuteContext(ctx); err == nil {
		t.Fatal("expected an error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestBackupsDownloadRefusesExistingFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.zip")
	_ = os.WriteFile(target, []byte("old"), 0o644)
	calls := 0
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { calls++ }, "backups", "download", "ba-abc123", "--output", target)
	if err == nil || !strings.Contains(err.Error(), "pass --force") || calls != 0 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestBackupsRestorePromptEndsOnCancel(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"publicId":"ba-abc123","environment":"en-abc123"}`))
	}))
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	reader, writer := io.Pipe()
	defer writer.Close()
	factory := testFactory(&bytes.Buffer{})
	factory.IOStreams.IsTTY = true
	factory.IOStreams.In = reader
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	command := NewCmdRoot(factory)
	command.SetArgs([]string{"--host", server.URL, "backups", "restore", "ba-abc123"})
	start := time.Now()
	err := command.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 2*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "POST") {
			t.Fatalf("destructive request sent: %v", calls)
		}
	}
}

func TestBackupsDownloadErrorHidesSignedLink(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	link := "http://" + closed.Listener.Addr().String() + "/storage/site.zip?X-Amz-Signature=SECRET"
	closed.Close()
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"publicId":"ba-abc123","type":"files","url":%q}`, link)
	}, "backups", "download", "ba-abc123", "--output", t.TempDir())
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/storage/") {
		t.Fatalf("err = %v", err)
	}
}

// downloadOverTLS runs backups download against an https API whose link points at storage, trusting the httptest certificate.
func downloadOverTLS(t *testing.T, api *httptest.Server, dir string) error {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	factory := testFactory(&bytes.Buffer{})
	factory.HTTPClient = api.Client()
	previous := backups.DownloadTransport
	trusted := api.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	backups.DownloadTransport = func() *http.Transport { return &http.Transport{TLSClientConfig: trusted} }
	t.Cleanup(func() { backups.DownloadTransport = previous })
	command := NewCmdRoot(factory)
	command.SetArgs([]string{"--host", api.URL, "backups", "download", "ba-abc123", "--output", dir})
	return command.Execute()
}

func storageServer(t *testing.T, tls bool, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	wrapped := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("API token sent to storage")
		}
		handler(w, r)
	}
	var server *httptest.Server
	if tls {
		server = httptest.NewTLSServer(http.HandlerFunc(wrapped))
	} else {
		server = httptest.NewServer(http.HandlerFunc(wrapped))
	}
	t.Cleanup(server.Close)
	return server
}

func apiHanding(t *testing.T, tls bool, link string) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"publicId":"ba-abc123","type":"files","url":%q}`, link)
	})
	var server *httptest.Server
	if tls {
		server = httptest.NewTLSServer(handler)
	} else {
		server = httptest.NewServer(handler)
	}
	t.Cleanup(server.Close)
	return server
}

func TestBackupsDownloadHTTPSOnly(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("zipbytes")) }

	plain := storageServer(t, false, ok)
	err := downloadOverTLS(t, apiHanding(t, true, plain.URL+"/site.zip?sig=SECRET"), t.TempDir())
	if err == nil || err.Error() != "the backup link is not https; refusing to download" {
		t.Fatalf("http link from https API: %v", err)
	}

	redirectTarget := storageServer(t, false, ok)
	redirecting := storageServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/site.zip?sig=SECRET", http.StatusFound)
	})
	err = downloadOverTLS(t, apiHanding(t, true, redirecting.URL+"/site.zip?sig=SECRET"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "the download redirected to plain http; refusing") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("redirect to http: %v", err)
	}

	secure := storageServer(t, true, ok)
	hop := storageServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secure.URL+"/site.zip?sig=1", http.StatusFound)
	})
	dir := t.TempDir()
	if err := downloadOverTLS(t, apiHanding(t, true, hop.URL+"/site.zip?sig=1"), dir); err != nil {
		t.Fatalf("https redirect: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "ba-abc123-files.zip")); string(data) != "zipbytes" {
		t.Fatalf("data = %q", data)
	}

	// A plain http API (local development) may hand out http links.
	dir = t.TempDir()
	_, err = runDomains(t, downloadHandler(t, ok), "backups", "download", "ba-abc123", "--output", dir)
	if err != nil {
		t.Fatalf("http API: %v", err)
	}
}
