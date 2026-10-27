package cmdutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// WriteJSONLine writes value as one JSON line without escaping &, < or >, so URLs stay readable.
func WriteJSONLine(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	return nil
}

// PlainJSON re-encodes a raw FRAPI value without \/ escapes. Object keys come out sorted.
// It returns raw unchanged when the value cannot be re-encoded.
func PlainJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return raw
	}
	var buffer bytes.Buffer
	if err := WriteJSONLine(&buffer, value); err != nil {
		return raw
	}
	return json.RawMessage(bytes.TrimSpace(buffer.Bytes()))
}

const maxJSONInputSize = 10 << 20

// ReadJSONObject reads a JSON object from a file, or from stdin when path is
// "-". The raw object is retained so the API receives the user's exact shape.
func ReadJSONObject(path string, stdin io.Reader) (json.RawMessage, error) {
	var reader io.Reader
	var closeReader io.Closer
	if path == "-" {
		reader = stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open JSON input: %w", err)
		}
		reader = file
		closeReader = file
	}
	if closeReader != nil {
		defer closeReader.Close()
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxJSONInputSize+1))
	if err != nil {
		return nil, fmt.Errorf("read JSON input: %w", err)
	}
	if len(body) > maxJSONInputSize {
		return nil, fmt.Errorf("JSON input exceeds %d bytes", maxJSONInputSize)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("JSON input is empty")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, fmt.Errorf("JSON input must be an object: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("JSON input must be an object")
	}
	return json.RawMessage(body), nil
}

func ParseAssignments(values []string) (map[string]string, error) {
	assignments := make(map[string]string, len(values))
	for _, value := range values {
		name, assigned, ok := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid assignment %q; expected NAME=VALUE", value)
		}
		assignments[name] = assigned
	}
	return assignments, nil
}

func AnyFlagChanged(command *cobra.Command, names ...string) bool {
	for _, name := range names {
		if command.Flags().Changed(name) {
			return true
		}
	}
	return false
}
