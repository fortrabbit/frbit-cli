package cmdutil

import (
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

// WriteNext prints the server's hint for the next setup step, if there is one.
func WriteNext(writer io.Writer, next *api.Step) error {
	if next == nil || next.Instruction == "" {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "Next: %s\n", iostreams.StripControl(next.Instruction)); err != nil {
		return err
	}
	if next.URL != nil && *next.URL != "" {
		_, err := fmt.Fprintf(writer, "  %s\n", iostreams.StripControl(*next.URL))
		return err
	}
	return nil
}

// WriteOpenStep prints the optional step named name while it is open and not already the next step; fallback stands in for a missing instruction, command is the frbit command that completes it.
func WriteOpenStep(writer io.Writer, you api.You, name, fallback, command string) error {
	step := you.OptionalStep(name)
	if step == nil || step.Done || (you.Next != nil && you.Next.Step == name) {
		return nil
	}
	instruction := iostreams.StripControl(step.Instruction)
	if instruction == "" {
		instruction = fallback
	}
	if _, err := fmt.Fprintf(writer, "Optional: %s\n", instruction); err != nil {
		return err
	}
	if command != "" {
		if _, err := fmt.Fprintf(writer, "  %s\n", command); err != nil {
			return err
		}
	}
	if link := step.Link(); link != "" {
		_, err := fmt.Fprintf(writer, "  %s\n", link)
		return err
	}
	return nil
}
