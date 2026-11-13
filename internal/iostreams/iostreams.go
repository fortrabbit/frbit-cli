package iostreams

import (
	"io"
	"os"

	"golang.org/x/term"
)

// IOStreams keeps commands independent from process-global streams.
type IOStreams struct {
	In       io.Reader
	Out      io.Writer
	ErrOut   io.Writer
	IsTTY    bool
	IsErrTTY bool
}

// IsTerminal reports whether stream is a file attached to a terminal.
func IsTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func System() IOStreams {
	return IOStreams{
		In:       os.Stdin,
		Out:      os.Stdout,
		ErrOut:   os.Stderr,
		IsTTY:    term.IsTerminal(int(os.Stdin.Fd())),
		IsErrTTY: term.IsTerminal(int(os.Stderr.Fd())),
	}
}
