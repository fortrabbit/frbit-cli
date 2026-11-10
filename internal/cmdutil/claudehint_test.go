package cmdutil

import (
	"bytes"
	"testing"
)

func setClaudeHint(t *testing.T, listed bool, env map[string]string) {
	t.Helper()
	oldListed, oldGetenv, oldWritten := claudePluginListed, claudeGetenv, claudeHintWritten
	t.Cleanup(func() { claudePluginListed, claudeGetenv, claudeHintWritten = oldListed, oldGetenv, oldWritten })
	claudePluginListed = listed
	claudeGetenv = func(key string) string { return env[key] }
	claudeHintWritten = false
}

func TestClaudeHintOffByDefault(t *testing.T) {
	setClaudeHint(t, claudePluginListed, map[string]string{"CLAUDECODE": "1"})
	var out bytes.Buffer
	SuggestClaudePlugin(&out)
	if out.Len() != 0 {
		t.Fatalf("hint written while plugin is not listed: %q", out.String())
	}
}

func TestClaudeHintWrittenWhenListedAndInClaudeCode(t *testing.T) {
	setClaudeHint(t, true, map[string]string{"CLAUDECODE": "1"})
	var out bytes.Buffer
	SuggestClaudePlugin(&out)
	SuggestClaudePlugin(&out)
	want := "<claude-code-hint v=\"1\" type=\"plugin\" value=\"fortrabbit@claude-plugins-official\" />\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q once", out.String(), want)
	}
}

func TestClaudeHintSilentOutsideClaudeCode(t *testing.T) {
	setClaudeHint(t, true, map[string]string{})
	var out bytes.Buffer
	SuggestClaudePlugin(&out)
	if out.Len() != 0 {
		t.Fatalf("hint written outside Claude Code: %q", out.String())
	}
}
