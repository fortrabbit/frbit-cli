package cmdutil

import "fmt"

// ExitError ends the command with Code and no error line, for a remote command whose own exit code is the answer.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }
