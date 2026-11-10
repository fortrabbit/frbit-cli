package iostreams

import (
	"bytes"
	"fmt"
	"testing"
)

func TestStripControl(t *testing.T) {
	const input = "a\tb\nc\x1b]0;x\x07\x1b[1A\r\u009bd é"
	const want = "a\tb\nc]0;x[1Ad é"
	if got := StripControl(input); got != want {
		t.Fatalf("StripControl = %q, want %q", got, want)
	}
	var out bytes.Buffer
	if _, err := fmt.Fprint(Stripper(&out), input); err != nil || out.String() != want {
		t.Fatalf("Stripper = %q, %v", out.String(), err)
	}
}
