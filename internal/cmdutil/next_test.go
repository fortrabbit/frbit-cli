package cmdutil

import (
	"bytes"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

func TestWriteOpenStepFallsBackAndStripsControl(t *testing.T) {
	url := "https://dash.example/ssh"
	cases := map[string]string{
		"":                 "Optional: Add an SSH key\n  frbit ssh-keys add\n  https://dash.example/ssh\n",
		"Add a key\x1b[2J": "Optional: Add a key[2J\n  frbit ssh-keys add\n  https://dash.example/ssh\n",
	}
	for instruction, want := range cases {
		var out bytes.Buffer
		you := api.You{Optional: []api.OptionalStep{{Step: "add_ssh_key", Instruction: instruction, URL: &url}}}
		if err := WriteOpenStep(&out, you, "add_ssh_key", "Add an SSH key", "frbit ssh-keys add"); err != nil || out.String() != want {
			t.Errorf("WriteOpenStep(%q) = %q, %v; want %q", instruction, out.String(), err, want)
		}
	}
}
