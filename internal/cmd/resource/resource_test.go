package resource

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

type configStore struct{}

func (configStore) Load() (config.Config, error) { return config.Config{}, nil }
func (configStore) Save(config.Config) error     { return nil }

type credentialStore struct{}

func (credentialStore) Get(string) (credentials.Credential, error) {
	return credentials.Credential{}, credentials.ErrNotFound
}
func (credentialStore) Set(string, credentials.Credential) error { return nil }
func (credentialStore) Delete(string) error                      { return nil }

func TestWriteLogsStripsTerminalControlCharacters(t *testing.T) {
	output := &bytes.Buffer{}
	err := writeLogs(output, api.Resource{"logs": []any{map[string]any{
		"time": "2026-01-01T00:00:00Z",
		"log":  "\x1b[2Jbuild\r\ncomplete",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := output.String(); strings.ContainsAny(got, "\x1b\r") || !strings.Contains(got, "[2Jbuildcomplete") {
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
		ConfigStore:     configStore{},
		CredentialStore: credentialStore{},
		HTTPClient:      &http.Client{},
		Version:         "test",
	}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.SetOut(output)
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
