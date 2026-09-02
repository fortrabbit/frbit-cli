package environments

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

func TestWriteEnvironmentVariablesMasksValuesUnlessRevealed(t *testing.T) {
	variables := api.Resource{"custom": []any{map[string]any{"name": "SECRET", "value": "not-for-output"}}}
	masked := &bytes.Buffer{}
	if err := writeEnvironmentVariables(masked, variables, false); err != nil {
		t.Fatal(err)
	}
	if got := masked.String(); strings.Contains(got, "not-for-output") || !strings.Contains(got, "***") {
		t.Fatalf("masked output = %q", got)
	}

	revealed := &bytes.Buffer{}
	if err := writeEnvironmentVariables(revealed, variables, true); err != nil {
		t.Fatal(err)
	}
	if got := revealed.String(); !strings.Contains(got, "not-for-output") {
		t.Fatalf("revealed output = %q", got)
	}
}

func TestWriteEnvironmentVariablesJSONMasksValues(t *testing.T) {
	output := &bytes.Buffer{}
	if err := writeEnvironmentVariablesJSON(output, api.Resource{"custom": []any{map[string]any{"name": "SECRET", "value": "not-for-output"}}}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); strings.Contains(got, "not-for-output") || !strings.Contains(got, `"value":"***"`) {
		t.Fatalf("masked JSON = %q", got)
	}
}
