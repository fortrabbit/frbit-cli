package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/link"
)

func healthy() *Probes {
	return &Probes{
		Latest:      func(context.Context, string) (string, error) { return "", nil },
		SignIn:      func(context.Context) (string, error) { return "dev@example.com", nil },
		LookPath:    func(name string) (string, error) { return "/usr/bin/" + name, nil },
		ToolVersion: func(_ context.Context, name string) string { return name + " 1.0" },
		Linked:      func(context.Context) ([]string, bool, error) { return []string{"main"}, true, nil },
		Environment: func(context.Context) (Environment, error) {
			return Environment{ID: "en-a1b2c3", From: "label main", Target: "en-a1b2c3@ssh.eu-n1a.frbit.app"}, nil
		},
		SSH: func(context.Context, string) error { return nil },
		MCP: func(context.Context) ([]agentmcp.Registration, error) {
			return []agentmcp.Registration{{Agent: "claude-code", Installed: true, Current: true}}, nil
		},
		Skills: func() ([]agentskills.Installation, error) {
			return []agentskills.Installation{{Agent: "claude-code", Skill: "fortrabbit", Version: "2.9.4"}}, nil
		},
	}
}

func byName(checks []Check, name string) Check {
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	return Check{}
}

func TestAHealthySetupPassesEveryCheck(t *testing.T) {
	checks := Run(context.Background(), "1.15.0", healthy())
	if failed(checks) {
		t.Fatalf("checks failed: %+v", checks)
	}
	for _, check := range checks {
		if check.Status != StatusOK {
			t.Fatalf("%s = %s (%s), want ok", check.Name, check.Status, check.Detail)
		}
	}
}

func TestAnOutdatedVersionWarnsWithTheFix(t *testing.T) {
	probes := healthy()
	probes.Latest = func(context.Context, string) (string, error) { return "v1.16.0", nil }
	check := byName(Run(context.Background(), "1.15.0", probes), "Version")
	if check.Status != StatusWarn || !strings.Contains(check.Detail, "v1.16.0 is out") || check.Fix == "" {
		t.Fatalf("version check = %+v", check)
	}
}

func TestSignedOutWarnsAndSkipsSSH(t *testing.T) {
	probes := healthy()
	probes.SignIn = func(context.Context) (string, error) { return "", credentials.ErrNotFound }
	sshRan := false
	probes.SSH = func(context.Context, string) error { sshRan = true; return nil }
	checks := Run(context.Background(), "1.15.0", probes)
	if check := byName(checks, "Sign-in"); check.Status != StatusWarn || check.Fix != "frbit auth login" {
		t.Fatalf("sign-in check = %+v", check)
	}
	if check := byName(checks, "SSH"); check.Status != StatusSkip || sshRan {
		t.Fatalf("ssh check = %+v, ran = %v", check, sshRan)
	}
	if failed(checks) {
		t.Fatal("a signed-out machine should not fail")
	}
}

func TestMissingSSHFailsAndMissingRsyncWarns(t *testing.T) {
	probes := healthy()
	probes.LookPath = func(name string) (string, error) {
		if name == "git" {
			return "/usr/bin/git", nil
		}
		return "", errors.New("not found")
	}
	checks := Run(context.Background(), "1.15.0", probes)
	if check := byName(checks, "ssh"); check.Status != StatusFail || !strings.Contains(check.Fix, "OpenSSH") {
		t.Fatalf("ssh tool check = %+v", check)
	}
	if check := byName(checks, "rsync"); check.Status != StatusWarn {
		t.Fatalf("rsync tool check = %+v", check)
	}
	if !failed(checks) {
		t.Fatal("a missing ssh should fail")
	}
}

func TestAFailedSSHConnectionFails(t *testing.T) {
	probes := healthy()
	probes.SSH = func(context.Context, string) error { return errors.New("permission denied (publickey)") }
	check := byName(Run(context.Background(), "1.15.0", probes), "SSH")
	if check.Status != StatusFail || !strings.Contains(check.Detail, "en-a1b2c3 (label main)") {
		t.Fatalf("ssh check = %+v", check)
	}
}

func TestOutsideACheckoutSkipsProjectAndSSH(t *testing.T) {
	probes := healthy()
	probes.Linked = func(context.Context) ([]string, bool, error) { return nil, false, nil }
	checks := Run(context.Background(), "1.15.0", probes)
	if byName(checks, "Project").Status != StatusSkip || byName(checks, "SSH").Status != StatusSkip {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestNoEnvironmentForTheCheckoutSkipsSSHWithTheFix(t *testing.T) {
	probes := healthy()
	probes.Environment = func(context.Context) (Environment, error) { return Environment{}, link.ErrNoEnvironment }
	check := byName(Run(context.Background(), "1.15.0", probes), "SSH")
	if check.Status != StatusSkip || check.Fix != "frbit link <env-id>" {
		t.Fatalf("ssh check = %+v", check)
	}
}

func TestMissingAgentSetupWarns(t *testing.T) {
	probes := healthy()
	probes.MCP = func(context.Context) ([]agentmcp.Registration, error) {
		return []agentmcp.Registration{{Agent: "codex"}}, nil
	}
	probes.Skills = func() ([]agentskills.Installation, error) { return nil, nil }
	checks := Run(context.Background(), "1.15.0", probes)
	if check := byName(checks, "MCP, codex"); check.Status != StatusWarn || check.Fix != "frbit setup agent --agent codex" {
		t.Fatalf("mcp check = %+v", check)
	}
	if check := byName(checks, "Skills"); check.Status != StatusWarn || check.Fix != "frbit skills install" {
		t.Fatalf("skills check = %+v", check)
	}
}

func TestCommandPrintsJSONAndExitsNonZeroOnFailure(t *testing.T) {
	probes := healthy()
	probes.SSH = func(context.Context, string) error { return errors.New("refused") }
	command := NewCmdDoctor(&app.Factory{Version: "1.15.0"}, probes)
	command.SilenceErrors, command.SilenceUsage = true, true
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"--json"})
	if err := command.Execute(); !errors.Is(err, cmdutil.ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	var report struct {
		OK     bool    `json:"ok"`
		Checks []Check `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.OK || byName(report.Checks, "SSH").Status != StatusFail {
		t.Fatalf("report = %+v", report)
	}
}

func TestCommandPrintsTheFixUnderAProblem(t *testing.T) {
	probes := healthy()
	probes.SignIn = func(context.Context) (string, error) { return "", credentials.ErrNotFound }
	command := NewCmdDoctor(&app.Factory{Version: "1.15.0"}, probes)
	command.SilenceErrors, command.SilenceUsage = true, true
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warn  Sign-in") || !strings.Contains(out.String(), "→ frbit auth login") {
		t.Fatalf("output:\n%s", out.String())
	}
}
