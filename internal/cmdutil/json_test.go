package cmdutil

import (
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
