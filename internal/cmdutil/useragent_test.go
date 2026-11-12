package cmdutil

import "testing"

func withEnv(t *testing.T, values map[string]string) {
	t.Helper()
	previous := agentGetenv
	agentGetenv = func(name string) string { return values[name] }
	t.Cleanup(func() { agentGetenv = previous })
}

func TestUserAgentNamesTheDetectedAgent(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{}, "frbit/1.16.0"},
		{map[string]string{"AI_AGENT": "claude-code_2-1-322_agent"}, "frbit/1.16.0 agent/claude-code"},
		{map[string]string{"CLAUDECODE": "1"}, "frbit/1.16.0 agent/claude-code"},
		{map[string]string{"CODEX_SANDBOX": "seatbelt"}, "frbit/1.16.0 agent/codex"},
		{map[string]string{"OPENCODE": "1"}, "frbit/1.16.0 agent/opencode"},
		{map[string]string{"AI_AGENT": "Bad Value; DROP", "CURSOR_AGENT": "1"}, "frbit/1.16.0 agent/cursor"},
	}
	for _, c := range cases {
		withEnv(t, c.env)
		if got := UserAgent("1.16.0"); got != c.want {
			t.Errorf("env %v: UserAgent = %q, want %q", c.env, got, c.want)
		}
	}
}
