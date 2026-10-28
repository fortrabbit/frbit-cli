package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/agent"
	"github.com/fortrabbit/frbit-cli/internal/agentmcp"
	"github.com/fortrabbit/frbit-cli/internal/agentskills"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/spf13/cobra"
)

type uninstallSystem struct {
	executable func() (string, error)
	cacheDir   func() (string, error)
	runner     agentmcp.Runner
	goos       string
}

// binaryRemoval removes the binary with the package manager that installed it, or deletes the file.
type binaryRemoval struct {
	path    string
	command []string
}

func NewCmdUninstall(factory *app.Factory) *cobra.Command {
	return newCmdUninstall(factory, nil, nil, uninstallSystem{
		executable: os.Executable,
		cacheDir:   os.UserCacheDir,
		runner:     agentmcp.ExecRunner{},
		goos:       runtime.GOOS,
	})
}

func newCmdUninstall(factory *app.Factory, mcp mcpService, skills skillsService, system uninstallSystem) *cobra.Command {
	if mcp == nil {
		mcp = agentmcp.NewService(agentmcp.Options{})
	}
	if skills == nil {
		skills = agentskills.NewService(agentskills.Options{
			HTTPClient: factory.HTTPClient,
			UserAgent:  fmt.Sprintf("frbit/%s", factory.Version),
		})
	}
	var yes bool
	command := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the agent setup and the frbit CLI",
		Long: `Remove everything fortrabbit put on this machine for coding agents: the MCP server and the
skill in Claude Code and Codex, the Claude Code plugin, the CLI sign-in, the config and cache
folders, and the frbit binary. A Homebrew or npm install is removed with brew or npm.

Lists everything first and asks once. Apps, environments and the account stay.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(cmd, factory, mcp, skills, system, yes)
		},
	}
	command.Flags().BoolVarP(&yes, "yes", "y", false, "Remove without prompting for confirmation")
	return command
}

func runUninstall(cmd *cobra.Command, factory *app.Factory, mcp mcpService, skills skillsService, system uninstallSystem, yes bool) error {
	writer := cmd.OutOrStdout()
	// Without Claude Code or Codex there is no agent setup to undo, only the CLI itself.
	agents, err := mcp.ResolveAgents(nil)
	if err != nil {
		agents = nil
	}
	pluginID := ""
	if slices.Contains(agents, agent.ClaudeCode) {
		pluginID = mcp.ClaudePluginID(cmd.Context())
	}
	plan, err := planRemoval(cmd, factory, mcp, skills, agents)
	if err != nil {
		return err
	}
	folders := existingFolders(factory, system)
	binary, err := locateBinary(system.executable)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(writer, "This removes:"); err != nil {
		return err
	}
	if err := plan.print(writer); err != nil {
		return err
	}
	if pluginID != "" {
		if _, err := fmt.Fprintf(writer, "  %-14s plugin %s\n", agent.ClaudeCode.Label()+":", pluginID); err != nil {
			return err
		}
	}
	for _, folder := range folders {
		if _, err := fmt.Fprintf(writer, "  %-14s %s\n", "frbit CLI:", folder); err != nil {
			return err
		}
	}
	if err := printBinary(writer, binary); err != nil {
		return err
	}
	if confirmed, err := confirmRemoval(factory, writer, yes); err != nil || !confirmed {
		return err
	}

	if err := plan.execute(cmd, factory, mcp, skills); err != nil {
		return err
	}
	if pluginID != "" {
		if err := mcp.RemoveClaudePlugin(cmd.Context(), pluginID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "Removed the Claude Code plugin %s\n", pluginID); err != nil {
			return err
		}
	}
	for _, folder := range folders {
		if err := os.RemoveAll(folder); err != nil {
			return fmt.Errorf("remove %s: %w", folder, err)
		}
		if _, err := fmt.Fprintf(writer, "Removed %s\n", folder); err != nil {
			return err
		}
	}
	finalStep, err := removeBinary(cmd.Context(), system, binary)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "frbit uninstalled.\n\n%s\n", disconnectHint); err != nil {
		return err
	}
	if finalStep != "" {
		_, err = fmt.Fprintln(writer, finalStep)
	}
	return err
}

func existingFolders(factory *app.Factory, system uninstallSystem) []string {
	var candidates []string
	if factory.CredentialLockPath != "" {
		candidates = append(candidates, filepath.Dir(factory.CredentialLockPath))
	}
	if cacheDir, err := system.cacheDir(); err == nil {
		candidates = append(candidates, filepath.Join(cacheDir, "frbit"))
	}
	var folders []string
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			folders = append(folders, candidate)
		}
	}
	return folders
}

func locateBinary(executable func() (string, error)) (binaryRemoval, error) {
	path, err := executable()
	if err != nil {
		return binaryRemoval{}, fmt.Errorf("find the frbit binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	slashed := filepath.ToSlash(path)
	switch {
	case strings.Contains(slashed, "/Cellar/frbit/"):
		return binaryRemoval{path: path, command: []string{"brew", "uninstall", "frbit"}}, nil
	case strings.Contains(slashed, "/node_modules/@fortrabbit/"):
		return binaryRemoval{path: path, command: []string{"npm", "uninstall", "--global", "@fortrabbit/cli"}}, nil
	}
	return binaryRemoval{path: path}, nil
}

func printBinary(writer io.Writer, binary binaryRemoval) error {
	via := ""
	if binary.command != nil {
		via = fmt.Sprintf(" (%s)", strings.Join(binary.command, " "))
	}
	_, err := fmt.Fprintf(writer, "  %-14s binary at %s%s\n", "frbit CLI:", binary.path, via)
	return err
}

// removeBinary returns the step left to the person when the binary cannot remove itself.
func removeBinary(ctx context.Context, system uninstallSystem, binary binaryRemoval) (string, error) {
	if binary.command != nil {
		if output, err := system.runner.Run(ctx, binary.command[0], binary.command[1:]...); err != nil {
			return "", fmt.Errorf("%s: %w: %s", strings.Join(binary.command, " "), err, strings.TrimSpace(output))
		}
		return "", nil
	}
	// Windows keeps a running executable locked.
	if system.goos == "windows" {
		return fmt.Sprintf("Delete %s to finish.", binary.path), nil
	}
	if err := os.Remove(binary.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove %s: %w", binary.path, err)
	}
	return "", nil
}
