package cmdutil

import (
	"fmt"
	"io"
	"os"
)

const claudePlugin = "fortrabbit@claude-plugins-official"

// Off until the plugin is listed in the official Claude Code marketplace.
var claudePluginListed = false

var (
	claudeGetenv      = os.Getenv
	claudeHintWritten bool
)

// SuggestClaudePlugin writes Claude Code's plugin hint tag once per process, only inside Claude Code.
func SuggestClaudePlugin(w io.Writer) {
	if !claudePluginListed || claudeHintWritten || claudeGetenv("CLAUDECODE") == "" {
		return
	}
	claudeHintWritten = true
	_, _ = fmt.Fprintf(w, "<claude-code-hint v=\"1\" type=\"plugin\" value=\"%s\" />\n", claudePlugin)
}
