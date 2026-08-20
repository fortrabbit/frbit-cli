package login

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

func TestPrepareTokenEntryOpensDevelopmentURL(t *testing.T) {
	t.Setenv("FRBIT_DASHBOARD_URL", "http://localhost:3001")
	output := &bytes.Buffer{}
	opened := ""
	factory := &app.Factory{
		IOStreams: iostreams.IOStreams{Out: output, IsTTY: true},
		OpenBrowser: func(rawURL string) error {
			opened = rawURL
			return nil
		},
	}
	command := &cobra.Command{}
	command.SetOut(output)

	err := prepareTokenEntry(&Options{Factory: factory, Command: command})
	if err != nil {
		t.Fatalf("prepareTokenEntry() error = %v", err)
	}
	if opened != "http://localhost:3001/new/api-token" {
		t.Fatalf("opened URL = %q", opened)
	}
	if got := output.String(); !strings.Contains(got, opened) {
		t.Fatalf("output = %q", got)
	}
}

func TestPrepareTokenEntryNoBrowser(t *testing.T) {
	output := &bytes.Buffer{}
	opened := false
	factory := &app.Factory{
		IOStreams: iostreams.IOStreams{Out: output, IsTTY: true},
		OpenBrowser: func(string) error {
			opened = true
			return nil
		},
	}
	command := &cobra.Command{}
	command.SetOut(output)

	err := prepareTokenEntry(&Options{Factory: factory, Command: command, NoBrowser: true})
	if err != nil {
		t.Fatalf("prepareTokenEntry() error = %v", err)
	}
	if opened {
		t.Fatal("browser was opened with --no-browser")
	}
	if !strings.Contains(output.String(), "https://dash.fortrabbit.com/new/api-token") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPrepareTokenEntryIgnoresBrowserError(t *testing.T) {
	output := &bytes.Buffer{}
	factory := &app.Factory{
		IOStreams: iostreams.IOStreams{Out: output, IsTTY: true},
		OpenBrowser: func(string) error {
			return errors.New("no browser")
		},
	}
	command := &cobra.Command{}
	command.SetOut(output)

	if err := prepareTokenEntry(&Options{Factory: factory, Command: command}); err != nil {
		t.Fatalf("prepareTokenEntry() error = %v", err)
	}
}

func TestPrepareTokenEntrySkipsNonInteractiveInput(t *testing.T) {
	output := &bytes.Buffer{}
	opened := false
	factory := &app.Factory{
		IOStreams: iostreams.IOStreams{Out: output, IsTTY: false},
		OpenBrowser: func(string) error {
			opened = true
			return nil
		},
	}
	command := &cobra.Command{}
	command.SetOut(output)

	if err := prepareTokenEntry(&Options{Factory: factory, Command: command}); err != nil {
		t.Fatalf("prepareTokenEntry() error = %v", err)
	}
	if opened || output.Len() != 0 {
		t.Fatalf("non-interactive preparation opened=%t output=%q", opened, output.String())
	}
}
