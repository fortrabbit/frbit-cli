package root

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const zshCompletionMarker = "# frbit shell completion"

type completionGenerator func(*cobra.Command, io.Writer) error

func newCmdCompletion() *cobra.Command {
	command := &cobra.Command{
		Use:   "completion",
		Short: "Install shell completion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Install shell completion with: frbit completion install <shell>")
			return err
		},
	}

	command.AddCommand(
		newCmdGenerateCompletion("bash", false, func(root *cobra.Command, output io.Writer) error { return root.GenBashCompletion(output) }),
		newCmdGenerateCompletion("fish", false, func(root *cobra.Command, output io.Writer) error { return root.GenFishCompletion(output, true) }),
		newCmdGenerateCompletion("powershell", false, func(root *cobra.Command, output io.Writer) error { return root.GenPowerShellCompletion(output) }),
		newCmdGenerateCompletion("zsh", false, func(root *cobra.Command, output io.Writer) error { return root.GenZshCompletion(output) }),
		newCmdInstallCompletion(),
	)

	return command
}

func newCmdGenerateCompletion(shell string, hidden bool, generate completionGenerator) *cobra.Command {
	return &cobra.Command{
		Use:    shell,
		Short:  fmt.Sprintf("Generate the autocompletion script for %s", shell),
		Hidden: hidden,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(output, "# To install this completion, run: frbit completion install %s\n\n", shell); err != nil {
				return err
			}
			if err := generate(cmd.Root(), output); err != nil {
				return err
			}
			_, err := fmt.Fprintf(output, "\n# To install this completion, run: frbit completion install %s\n", shell)
			return err
		},
	}
}

func newCmdInstallCompletion() *cobra.Command {
	command := &cobra.Command{
		Use:   "install",
		Short: "Install shell completion",
		Args:  cobra.NoArgs,
	}
	for _, shell := range []string{"bash", "fish", "powershell", "zsh"} {
		shell := shell
		command.AddCommand(&cobra.Command{
			Use:   shell,
			Short: fmt.Sprintf("Install %s completion", shell),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return installCompletion(cmd.Root(), shell, cmd.OutOrStdout())
			},
		})
	}
	return command
}

func installCompletion(root *cobra.Command, shell string, output io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}

	switch shell {
	case "bash":
		return installGeneratedCompletion(root, filepath.Join(userDataDir(home), "bash-completion", "completions", "frbit"), func(root *cobra.Command, output io.Writer) error {
			return root.GenBashCompletion(output)
		}, output)
	case "fish":
		return installGeneratedCompletion(root, filepath.Join(userConfigDir(home), "fish", "completions", "frbit.fish"), func(root *cobra.Command, output io.Writer) error {
			return root.GenFishCompletion(output, true)
		}, output)
	case "powershell":
		completionPath := filepath.Join(userConfigDir(home), "frbit", "completion.ps1")
		if err := installGeneratedCompletion(root, completionPath, func(root *cobra.Command, output io.Writer) error {
			return root.GenPowerShellCompletion(output)
		}, nil); err != nil {
			return err
		}
		profilePath := powerShellProfilePath(home)
		if err := configurePowerShell(profilePath, completionPath); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "Installed PowerShell completion to %s.\n", completionPath)
		return err
	case "zsh":
		completionPath := filepath.Join(home, ".zfunc", "_frbit")
		if err := installGeneratedCompletion(root, completionPath, func(root *cobra.Command, output io.Writer) error {
			return root.GenZshCompletion(output)
		}, nil); err != nil {
			return err
		}
		zshDir := os.Getenv("ZDOTDIR")
		if zshDir == "" {
			zshDir = home
		}
		if err := configureZsh(filepath.Join(zshDir, ".zshrc")); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "Installed zsh completion to %s. Restart zsh or run exec zsh.\n", completionPath)
		return err
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}

func userDataDir(home string) string {
	if directory := os.Getenv("XDG_DATA_HOME"); directory != "" {
		return directory
	}
	return filepath.Join(home, ".local", "share")
}

func userConfigDir(home string) string {
	if directory := os.Getenv("XDG_CONFIG_HOME"); directory != "" {
		return directory
	}
	return filepath.Join(home, ".config")
}

func powerShellProfilePath(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	}
	return filepath.Join(userConfigDir(home), "powershell", "Microsoft.PowerShell_profile.ps1")
}

func installGeneratedCompletion(root *cobra.Command, path string, generate completionGenerator, output io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create completion directory: %w", err)
	}
	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".frbit-")
	if err != nil {
		return fmt.Errorf("create completion file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)
	if err := generate(root, temporaryFile); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("generate completion: %w", err)
	}
	if err := temporaryFile.Chmod(0o644); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("set completion permissions: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close completion file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install completion: %w", err)
	}
	if output != nil {
		_, err := fmt.Fprintf(output, "Installed completion to %s.\n", path)
		return err
	}
	return nil
}

func configureZsh(zshrcPath string) error {
	contents, err := os.ReadFile(zshrcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", zshrcPath, err)
	}
	if strings.Contains(string(contents), zshCompletionMarker) {
		return nil
	}

	file, err := os.OpenFile(zshrcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", zshrcPath, err)
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "\n%s\nfpath=(~/.zfunc $fpath)\nautoload -Uz compinit\ncompinit\n", zshCompletionMarker)
	if err != nil {
		return fmt.Errorf("configure zsh completion: %w", err)
	}
	return nil
}

func configurePowerShell(profilePath string, completionPath string) error {
	contents, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", profilePath, err)
	}
	if strings.Contains(string(contents), zshCompletionMarker) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return fmt.Errorf("create PowerShell profile directory: %w", err)
	}
	file, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", profilePath, err)
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "\n%s\n. '%s'\n", zshCompletionMarker, strings.ReplaceAll(completionPath, "'", "''"))
	if err != nil {
		return fmt.Errorf("configure PowerShell completion: %w", err)
	}
	return nil
}
