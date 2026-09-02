package resource

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
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
	if got := output.String(); strings.ContainsAny(got, "\x1b\r") || !strings.Contains(got, "[2Jbuildcomplete") {
		t.Fatalf("log output = %q", got)
	}
}
