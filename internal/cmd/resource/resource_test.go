package resource

import (
	"bytes"
	"context"
	"github.com/fortrabbit/frbit-cli/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

func TestWriteLogsStripsTerminalControlCharacters(t *testing.T) {
	output := &bytes.Buffer{}
	err := writeLogs(output, api.Resource{"logs": []any{map[string]any{
		"time": "2026-01-01T00:00:00Z",
		"log":  "\x1b[2Jbuild\r\ncomplete",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := output.String(); strings.ContainsAny(got, "\x1b\r") || !strings.Contains(got, "[2Jbuild\ncomplete") {
		t.Fatalf("log output = %q", got)
	}
}

func TestDeploymentLogsFollowStopsOnFinalState(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "test-token")
	LogsPollInterval = time.Millisecond
	t.Cleanup(func() { LogsPollInterval = 2 * time.Second })
	var mu sync.Mutex
	logCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/logs") {
			logCalls++
			switch logCalls {
			case 1:
				_, _ = w.Write([]byte(`{"ready":false,"logs":[]}`))
			case 2:
				_, _ = w.Write([]byte(`{"ready":false,"logs":[{"time":"t1","log":"build"},{"time":"t2","log":"\u001b[2Jinstall"}]}`))
			default:
				_, _ = w.Write([]byte(`{"ready":true,"logs":[{"time":"t1","log":"build"},{"time":"t2","log":"install"},{"time":"t3","log":"done"}]}`))
			}
			return
		}
		status := "building"
		if logCalls >= 2 {
			status = "succeeded"
		}
		_, _ = w.Write([]byte(`{"publicId":"dp-abc123","status":"` + status + `"}`))
	}))
	defer server.Close()
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: &bytes.Buffer{}},
		ConfigStore:     testutil.ConfigStore{},
		CredentialStore: testutil.CredentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.SetOut(output)
	root.SetErr(&bytes.Buffer{})
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.AddCommand(NewCmdGroup(factory, Spec{Use: "deployments", Singular: "deployment", Path: "/deployments", Short: "x", Logs: true}))
	root.SetArgs([]string{"--host", server.URL, "deployments", "logs", "dp-abc123", "--follow"})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow did not stop on a final state")
	}
	want := "t1\tbuild\nt2\t[2Jinstall\nt3\tdone\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

// followDeployment runs `deployments logs dp-abc123 --follow` against handler and returns stdout and stderr.
func followDeployment(t *testing.T, ctx context.Context, handler http.HandlerFunc) (string, string, error) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	LogsPollInterval = time.Millisecond
	t.Cleanup(func() { LogsPollInterval = 2 * time.Second })
	server := httptest.NewServer(handler)
	defer server.Close()
	output, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader(""), Out: output, ErrOut: errOut},
		ConfigStore:     testutil.ConfigStore{},
		CredentialStore: testutil.CredentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.SetOut(output)
	root.SetErr(errOut)
	root.AddCommand(NewCmdGroup(factory, Spec{Use: "deployments", Singular: "deployment", Path: "/deployments", Short: "x", Logs: true}))
	root.SetArgs([]string{"--host", server.URL, "deployments", "logs", "dp-abc123", "--follow"})
	err := root.ExecuteContext(ctx)
	return output.String(), errOut.String(), err
}

func TestDeploymentLogsFollowKeepsPollingWhenReadyButNotFinal(t *testing.T) {
	var mu sync.Mutex
	deploymentCalls := 0
	out, errOut, err := followDeployment(t, context.Background(), func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/logs") {
			_, _ = w.Write([]byte(`{"ready":true,"logs":[{"time":"t1","log":"build"}]}`))
			return
		}
		deploymentCalls++
		status := "building"
		if deploymentCalls >= 3 {
			status = "failed"
		}
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	})
	if err != nil || deploymentCalls != 3 || out != "t1\tbuild\n" || strings.Contains(errOut, "waiting for logs") {
		t.Fatalf("err=%v calls=%d out=%q err=%q", err, deploymentCalls, out, errOut)
	}
}

func TestDeploymentLogsFollowExitsZeroOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, errOut, err := followDeployment(t, ctx, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/logs") {
			cancel()
			_, _ = w.Write([]byte(`{"ready":false,"logs":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"building"}`))
	})
	if err != nil {
		t.Fatalf("cancel must exit cleanly: %v %q", err, errOut)
	}
}

func TestDeploymentLogsFollowSurvivesTransientErrorAndAnnouncesWaiting(t *testing.T) {
	var mu sync.Mutex
	logCalls := 0
	out, errOut, err := followDeployment(t, context.Background(), func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/logs") {
			logCalls++
			switch logCalls {
			case 1:
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"title":"down"}`))
			case 2:
				_, _ = w.Write([]byte(`{"ready":false,"logs":[]}`))
			default:
				_, _ = w.Write([]byte(`{"ready":true,"logs":[{"time":"t1","log":"ok"}]}`))
			}
			return
		}
		status := "building"
		if logCalls >= 2 {
			status = "succeeded"
		}
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	})
	if err != nil || out != "t1\tok\n" || strings.Count(errOut, "waiting for logs") != 1 {
		t.Fatalf("err=%v out=%q err=%q", err, out, errOut)
	}
}

func TestFlatObjectsReadAsKeyValuePairsInCalendarOrder(t *testing.T) {
	var out bytes.Buffer
	if err := WriteResource(&out, api.Resource{"backups": map[string]any{"retention": map[string]any{"monthly": 1.0, "daily": 7.0, "weekly": 4.0}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "daily 7, weekly 4, monthly 1") {
		t.Fatalf("output:\n%s", out.String())
	}
	if got := resourceValue(map[string]any{"a": map[string]any{"b": 1.0}}); got != `{"a":{"b":1}}` {
		t.Fatalf("nested object = %q", got)
	}
}

func TestAnEmptyObjectStaysVisible(t *testing.T) {
	if got := resourceValue(map[string]any{}); got != "{}" {
		t.Fatalf("empty object = %q", got)
	}
}
