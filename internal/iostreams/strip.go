package iostreams

import (
	"io"
	"strings"
	"unicode"
)

// StripControl removes control characters except newline and tab, so text from the API cannot move the cursor or rewrite the terminal.
func StripControl(value string) string {
	return strings.Map(func(character rune) rune {
		if character != '\n' && character != '\t' && unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}

// Stripper is a writer that applies StripControl to everything written through it; for text output only.
func Stripper(out io.Writer) io.Writer { return stripper{out} }

type stripper struct{ out io.Writer }

func (s stripper) Write(p []byte) (int, error) {
	if _, err := io.WriteString(s.out, StripControl(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}
