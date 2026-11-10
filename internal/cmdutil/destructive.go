package cmdutil

import (
	"context"
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/app"
)

// CheckDestructive rejects a missing or wrong --confirm before any request is sent.
func CheckDestructive(f *app.Factory, action, publicID, confirmation string) error {
	if confirmation != "" && confirmation != publicID {
		return fmt.Errorf("--confirm must exactly match %s", publicID)
	}
	if confirmation == "" && !f.IOStreams.IsTTY {
		return fmt.Errorf("%s requires interactive confirmation; pass --confirm %s for intentional non-interactive use", action, publicID)
	}
	return nil
}

// ConfirmDestructive returns true when --confirm matches publicID, or when the person types publicID on a TTY after the optional warning.
func ConfirmDestructive(f *app.Factory, out io.Writer, action, publicID, confirmation, warning string) (bool, error) {
	return ConfirmDestructiveContext(context.Background(), f, out, action, publicID, confirmation, warning)
}

// ConfirmDestructiveContext is ConfirmDestructive whose prompt ends with ctx.Err() when ctx is cancelled.
func ConfirmDestructiveContext(ctx context.Context, f *app.Factory, out io.Writer, action, publicID, confirmation, warning string) (bool, error) {
	if err := CheckDestructive(f, action, publicID, confirmation); err != nil {
		return false, err
	}
	if confirmation != "" {
		return true, nil
	}
	if warning != "" {
		if _, err := fmt.Fprintf(out, "WARNING: %s\n\n", warning); err != nil {
			return false, err
		}
	}
	return ConfirmExactContext(ctx, f.IOStreams.In, out, fmt.Sprintf("Type %s to confirm: ", publicID), publicID)
}
