package environments

import (
	"bytes"
	"context"
	"net/http"
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

func TestListShowsAppNames(t *testing.T) {
	handler := func(r request, w http.ResponseWriter) {
		switch r.path {
		case "/v1/environments":
			echo(w, 200, `[{"publicId":"en-shop11","name":"main","softwareVersion":"12"},{"publicId":"en-blog11","name":"main","softwareVersion":"6"}]`)
		case "/v1/apps":
			echo(w, 200, `[{"publicId":"ap-shop11","name":"shop","environments":["en-shop11"]},{"publicId":"ap-blog11","name":"blog","environments":["en-blog11"]}]`)
		default:
			echo(w, 404, `{}`)
		}
	}
	output, _, err := run(t, "", handler, "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID         APP   NAME", "en-shop11  shop  main", "en-blog11  blog  main"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q:\n%s", want, output)
		}
	}
	output, _, err = run(t, "", handler, "list", "--json")
	if err != nil || strings.Contains(output, "frbit:app") {
		t.Fatalf("JSON changed: %v\n%s", err, output)
	}
}

func TestListWithoutAppsStillPrintsTheTable(t *testing.T) {
	handler := func(r request, w http.ResponseWriter) {
		if r.path == "/v1/environments" {
			echo(w, 200, `[{"publicId":"en-shop11","name":"main"}]`)
			return
		}
		echo(w, 403, `{"title":"Forbidden"}`)
	}
	output, stderr, _, err := runStreams(t, context.Background(), "", handler, "list")
	if err != nil || !strings.Contains(output, "en-shop11") || !strings.Contains(stderr, "warning: no app names") {
		t.Fatalf("err %v\nstdout:\n%s\nstderr:\n%s", err, output, stderr)
	}
}
