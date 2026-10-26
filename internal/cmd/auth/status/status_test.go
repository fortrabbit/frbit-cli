package status

import (
	"bytes"
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
