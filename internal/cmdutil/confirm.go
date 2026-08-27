package cmdutil

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func Confirm(reader io.Reader, writer io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprint(writer, prompt); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func ConfirmExact(reader io.Reader, writer io.Writer, prompt string, expected string) (bool, error) {
	if _, err := fmt.Fprint(writer, prompt); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	return strings.TrimSpace(answer) == expected, nil
}
