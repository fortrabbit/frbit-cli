package cmdutil

import (
	"fmt"
	"io"

	"github.com/fortrabbit/frbit-cli/internal/agent"
)

func PrintOAuthHints(writer io.Writer, agents []agent.Agent) error {
	if _, err := fmt.Fprintln(writer, "Complete MCP authorization if prompted by your agent:"); err != nil {
		return err
	}
	for _, target := range agents {
		var hint string
		switch target {
		case agent.ClaudeCode:
			hint = "run /mcp in Claude Code"
		case agent.Codex:
			hint = "run `codex mcp login fortrabbit`"
		}
		if _, err := fmt.Fprintf(writer, "  %-14s %s\n", target.Label()+":", hint); err != nil {
			return err
		}
	}
	return nil
}
