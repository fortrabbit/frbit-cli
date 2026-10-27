package cmdutil

import (
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

// WriteNext prints the server's hint for the next setup step, if there is one.
func WriteNext(writer io.Writer, next *api.Step) error {
	if next == nil || next.Instruction == "" {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "Next: %s\n", next.Instruction); err != nil {
		return err
	}
	if next.URL != nil && *next.URL != "" {
		_, err := fmt.Fprintf(writer, "  %s\n", *next.URL)
		return err
	}
	return nil
}
