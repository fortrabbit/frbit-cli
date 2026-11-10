package cmdutil

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

func billedCommand(errOut *bytes.Buffer) *cobra.Command {
	command := &cobra.Command{}
	command.SetErr(errOut)
	return command
}

func TestAskBilledYesSkipsQuestion(t *testing.T) {
	errOut := &bytes.Buffer{}
	factory := &app.Factory{IOStreams: iostreams.IOStreams{In: strings.NewReader(""), IsTTY: false}}
	ok, err := AskBilled(billedCommand(errOut), factory, "Monthly price: 24.00 EUR", "Create and start billing? [y/N] ", true)
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	if !strings.Contains(errOut.String(), "24.00 EUR") {
		t.Fatalf("summary = %q", errOut.String())
	}
}

func TestAskBilledWithoutTTYRequiresYes(t *testing.T) {
	factory := &app.Factory{IOStreams: iostreams.IOStreams{In: strings.NewReader("y\n"), IsTTY: false}}
	ok, err := AskBilled(billedCommand(&bytes.Buffer{}), factory, "Monthly price: 24.00 EUR", "Create and start billing? [y/N] ", false)
	if ok || err == nil || err.Error() != "billed change needs --yes" {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
}

func TestAskBilledOnTTYAnswers(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "yes\n": true, "n\n": false, "\n": false} {
		errOut := &bytes.Buffer{}
		factory := &app.Factory{IOStreams: iostreams.IOStreams{In: strings.NewReader(answer), IsTTY: true}}
		ok, err := AskBilled(billedCommand(errOut), factory, "Monthly price: 24.00 EUR", "Create and start billing? [y/N] ", false)
		if err != nil || ok != want {
			t.Fatalf("answer %q: ok = %v, err = %v", answer, ok, err)
		}
		if !strings.Contains(errOut.String(), "Create and start billing? [y/N]") {
			t.Fatalf("prompt = %q", errOut.String())
		}
	}
}
