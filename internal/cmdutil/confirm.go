package cmdutil

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ConfirmContext asks a y/N question; it gives up with ctx.Err() when ctx ends while waiting for input.
func ConfirmContext(ctx context.Context, reader io.Reader, writer io.Writer, prompt string) (bool, error) {
	answer, err := readAnswer(ctx, reader, writer, prompt)
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}

// ConfirmExactContext reports whether the answer equals expected; it gives up with ctx.Err() when ctx ends while waiting for input.
func ConfirmExactContext(ctx context.Context, reader io.Reader, writer io.Writer, prompt string, expected string) (bool, error) {
	answer, err := readAnswer(ctx, reader, writer, prompt)
	if err != nil {
		return false, err
	}
	return answer == expected, nil
}

func readAnswer(ctx context.Context, reader io.Reader, writer io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(writer, prompt); err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type result struct {
		answer string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		answer, err := bufio.NewReader(reader).ReadString('\n')
		done <- result{answer, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		if r.err != nil && r.err != io.EOF {
			return "", fmt.Errorf("read confirmation: %w", r.err)
		}
		return strings.TrimSpace(r.answer), nil
	}
}

// ErrAborted ends a command whose confirmation was declined; it exits 1 without an error line.
var ErrAborted = errors.New("aborted")

// Aborted prints "Aborted." to out and returns ErrAborted.
func Aborted(out io.Writer) error {
	if _, err := fmt.Fprintln(out, "Aborted."); err != nil {
		return err
	}
	return ErrAborted
}
