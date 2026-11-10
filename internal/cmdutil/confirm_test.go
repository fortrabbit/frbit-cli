package cmdutil

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestConfirmExactRequiresExactValue(t *testing.T) {
	for _, test := range []struct {
		name      string
		input     string
		expected  string
		confirmed bool
	}{
		{name: "match", input: "ap-abc123\n", expected: "ap-abc123", confirmed: true},
		{name: "surrounding whitespace", input: "  ap-abc123  \n", expected: "ap-abc123", confirmed: true},
		{name: "different case", input: "AP-ABC123\n", expected: "ap-abc123", confirmed: false},
		{name: "different identifier", input: "ap-def456\n", expected: "ap-abc123", confirmed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := &bytes.Buffer{}
			confirmed, err := ConfirmExact(strings.NewReader(test.input), output, "Type the ID: ", test.expected)
			if err != nil {
				t.Fatal(err)
			}
			if confirmed != test.confirmed {
				t.Fatalf("confirmed = %t, want %t", confirmed, test.confirmed)
			}
			if output.String() != "Type the ID: " {
				t.Fatalf("prompt = %q", output.String())
			}
		})
	}
}

func TestAbortedPrintsAndReturnsSentinel(t *testing.T) {
	var out bytes.Buffer
	err := Aborted(&out)
	if !errors.Is(err, ErrAborted) || out.String() != "Aborted.\n" {
		t.Fatalf("err %v, out %q", err, out.String())
	}
}
