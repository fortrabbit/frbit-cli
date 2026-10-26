package cmdutil

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadJSONObjectFromStdinPreservesJSON(t *testing.T) {
	input := ` {"name":"store","nested":{"enabled":true}} `
	body, err := ReadJSONObject("-", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != strings.TrimSpace(input) {
		t.Fatalf("body = %q", body)
	}
}

func TestReadJSONObjectRejectsNonObject(t *testing.T) {
	for _, input := range []string{`[1,2,3]`, `null`} {
		if _, err := ReadJSONObject("-", strings.NewReader(input)); err == nil {
			t.Fatalf("expected %s to fail", input)
		}
	}
}

func TestParseAssignmentsKeepsEqualsAndCommasInValue(t *testing.T) {
	values, err := ParseAssignments([]string{"TOKEN=abc=def", "HOSTS=one,two"})
	if err != nil {
		t.Fatal(err)
	}
	if values["TOKEN"] != "abc=def" || values["HOSTS"] != "one,two" {
		t.Fatalf("values = %#v", values)
	}
}

func TestWriteJSONLineKeepsHTMLAndSlashesPlain(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSONLine(&out, map[string]string{"url": "https://a.example/x?a=1&b=<2>"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "{\"url\":\"https://a.example/x?a=1&b=<2>\"}\n" {
		t.Fatalf("line = %q", got)
	}
}

func TestPlainJSONDropsSlashEscapes(t *testing.T) {
	got, err := PlainJSON([]byte(`{"step":"connect_git","url":"https:\/\/dash.example\/git?a=1&b=2","n":1.50}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"n":1.50,"step":"connect_git","url":"https://dash.example/git?a=1&b=2"}` {
		t.Fatalf("got %s", got)
	}
}
