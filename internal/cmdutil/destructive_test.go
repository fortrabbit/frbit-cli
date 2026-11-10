package cmdutil

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
)

func destructiveFactory(input string, tty bool) *app.Factory {
	return &app.Factory{IOStreams: iostreams.IOStreams{In: strings.NewReader(input), IsTTY: tty}}
}

func TestConfirmDestructiveAcceptsMatchingConfirm(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := ConfirmDestructiveContext(context.Background(), destructiveFactory("", false), out, "deletion", "ap-abc123", "ap-abc123", "gone")
	if err != nil || !ok || out.String() != "WARNING: gone\n\n" {
		t.Fatalf("ok = %t, err = %v, out = %q", ok, err, out.String())
	}
}

func TestConfirmDestructiveRejectsWrongConfirm(t *testing.T) {
	_, err := ConfirmDestructiveContext(context.Background(), destructiveFactory("", true), &bytes.Buffer{}, "deletion", "ap-abc123", "ap-other1", "")
	if err == nil || err.Error() != "--confirm must exactly match ap-abc123" {
		t.Fatalf("err = %v", err)
	}
}

func TestConfirmDestructiveRequiresConfirmWithoutTTY(t *testing.T) {
	_, err := ConfirmDestructiveContext(context.Background(), destructiveFactory("", false), &bytes.Buffer{}, "disconnecting", "ap-abc123", "", "")
	want := "disconnecting requires interactive confirmation; pass --confirm ap-abc123 for intentional non-interactive use"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestConfirmDestructivePromptsOnTTY(t *testing.T) {
	for _, test := range []struct {
		input string
		want  bool
	}{{"ap-abc123\n", true}, {"nope\n", false}} {
		out := &bytes.Buffer{}
		ok, err := ConfirmDestructiveContext(context.Background(), destructiveFactory(test.input, true), out, "deletion", "ap-abc123", "", "It is gone.")
		if err != nil || ok != test.want {
			t.Fatalf("input %q: ok = %t, err = %v", test.input, ok, err)
		}
		if out.String() != "WARNING: It is gone.\n\nType ap-abc123 to confirm: " {
			t.Fatalf("out = %q", out.String())
		}
	}
}
