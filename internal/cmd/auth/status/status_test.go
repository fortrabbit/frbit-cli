package status

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/spf13/cobra"
)

type configStore struct{}

func (configStore) Load() (config.Config, error) { return config.Config{}, nil }
func (configStore) Save(config.Config) error     { return nil }

func TestStatusJSONKeepsSlashesAndAmpersandsPlain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"publicId":"pe-abc123","email":"ada@example.com","name":"Ada","next":{"step":"connect_git","instruction":"Connect GitHub.","url":"https:\/\/dash.example\/git?a=1&b=2"},"optional":[]}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("FRBIT_TOKEN", "frbit-at-test")

	output := &bytes.Buffer{}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.AddCommand(NewCmdStatus(&app.Factory{ConfigStore: configStore{}, HTTPClient: server.Client(), Version: "test"}, nil))
	root.SetOut(output)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--host", server.URL, "status", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status error = %v", err)
	}
	line := strings.TrimSpace(output.String())
	if !strings.Contains(line, `"url":"https://dash.example/git?a=1&b=2"`) || strings.Contains(line, `\/`) || strings.Contains(line, `\u0026`) {
		t.Fatalf("status line = %q", line)
	}
}

func statusServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"publicId":"pe-abc123","email":"ada@example.com","name":"Ada","next":{"step":"done"},"optional":[]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func runStatus(t *testing.T, server *httptest.Server, args ...string) (stdout string, stderr string) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.AddCommand(NewCmdStatus(&app.Factory{ConfigStore: configStore{}, HTTPClient: server.Client(), Version: "test"}, nil))
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(append([]string{"--host", server.URL, "status"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("status error = %v", err)
	}
	return out.String(), errOut.String()
}

const frbitTokenWarning = "FRBIT_TOKEN is set and is used instead of a stored sign-in. Unset it, or revoke the token under API tokens."

func TestStatusWarnsOnStderrWhileFRBITTokenIsSet(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "frbit-at-test")

	stdout, stderr := runStatus(t, statusServer(t))
	if !strings.Contains(stderr, frbitTokenWarning) || strings.Contains(stdout, "FRBIT_TOKEN is set") || strings.Contains(stderr+stdout, "frbit-at-test") {
		t.Fatalf("stdout %q, stderr %q", stdout, stderr)
	}
}

func TestStatusJSONCarriesTheWarningField(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "frbit-at-test")

	stdout, stderr := runStatus(t, statusServer(t), "--json")
	var line struct{ Status, Warning string }
	if err := json.Unmarshal([]byte(stdout), &line); err != nil || line.Status != "signed_in" || line.Warning != frbitTokenWarning || stderr != "" {
		t.Fatalf("stdout %q (%v), stderr %q", stdout, err, stderr)
	}
}
