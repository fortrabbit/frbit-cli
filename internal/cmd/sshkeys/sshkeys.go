// Package sshkeys builds the ssh-keys command group: the personal SSH keys of the signed-in person.
package sshkeys

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

var spec = resource.Spec{
	Use: "ssh-keys", Singular: "SSH key", Path: "/ssh-keys",
	Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "TYPE", Key: "type"}, {Header: "COMMENT", Key: "comment"}, {Header: "FINGERPRINT", Key: "fingerprint"}, {Header: "CREATED", Key: "createdAt"}},
}

// defaultKeyFiles are tried in order when add gets no file, relative to the home directory.
var defaultKeyFiles = []string{".ssh/id_ed25519.pub", ".ssh/id_rsa.pub"}

// acceptedKeyTypes are the key types the API accepts.
var acceptedKeyTypes = map[string]bool{"ssh-ed25519": true, "ssh-rsa": true}

// maxKeyFileSize is above any public key line the API accepts, so a larger file is not one.
const maxKeyFileSize = 16 * 1024

func NewCmdSSHKeys(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{
		Use:   "ssh-keys",
		Short: "Manage the personal SSH keys of your account",
		Long: "Manage the personal SSH keys of your account.\n\n" +
			"A personal key gives SSH and SFTP access to every environment you can access, for frbit ssh, db and files.",
		Args: cobra.NoArgs,
	}
	list := resource.NewCmdList(factory, spec)
	list.Short = "List the personal SSH keys of your account"
	command.AddCommand(list, newCmdGet(factory), newCmdAdd(factory), newCmdDelete(factory))
	return command
}

func newCmdGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <ssh-key>",
		Short: "Get an SSH key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/ssh-keys/"+url.PathEscape(args[0]))
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdAdd(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "add [public-key-file]",
		Short: "Add the public key of this machine to your account",
		Long: "Add a public key to your account.\n\n" +
			"Without a file it takes ~/.ssh/id_ed25519.pub, then ~/.ssh/id_rsa.pub. Create a key with `ssh-keygen -t ed25519` when there is none.\n" +
			"Only the public key (.pub) is sent; a private key file is refused. A key that is already on your account counts as done.\n" +
			"Needs a browser sign-in (frbit auth login) that is not limited to apps: an API token cannot add keys. You get an email about the new key.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := keyFile(args)
			if err != nil {
				return err
			}
			keyLine, err := readPublicKey(path)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			existing, err := client.ListAll(cmd.Context(), "/v1/ssh-keys", nil, 0)
			if err != nil {
				return err
			}
			publicKey := strings.Fields(keyLine)[1]
			for _, key := range existing.Resources {
				if value, _ := key["publicKey"].(string); value == publicKey {
					if printJSON {
						raw, err := json.Marshal(key)
						if err != nil {
							return err
						}
						return resource.WriteRaw(cmd, raw)
					}
					id, _ := key["publicId"].(string)
					_, err := fmt.Fprintf(cmd.OutOrStdout(), "The key in %s is already on your account (%s).\n", iostreams.StripControl(path), iostreams.StripControl(id))
					return err
				}
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/ssh-keys", map[string]any{"sshKey": keyLine})
			if err != nil {
				return err
			}
			if printJSON {
				return resource.Write(cmd, response, true)
			}
			id, _ := response.Resource["publicId"].(string)
			fingerprint, _ := response.Resource["fingerprint"].(string)
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Added SSH key %s from %s (fingerprint %s).\n", iostreams.StripControl(id), iostreams.StripControl(path), iostreams.StripControl(fingerprint))
			return err
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

// keyFile is the file given, or the first default key file that is a regular file.
func keyFile(args []string) (string, error) {
	if len(args) == 1 {
		return expandHome(args[0])
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w; pass the public key file", err)
	}
	for _, name := range defaultKeyFiles {
		path := filepath.Join(home, name)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no public key in ~/.ssh/id_ed25519.pub or ~/.ssh/id_rsa.pub; create one with `ssh-keygen -t ed25519`, or pass the public key file")
}

// expandHome expands a leading ~ that the shell left alone, as in a quoted argument.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w; pass the full path of the public key file", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// readPublicKey returns the one key line of a public key file and refuses anything else, above all a private key.
func readPublicKey(path string) (string, error) {
	// Opening a FIFO blocks until a writer appears, so the type is checked before the open.
	if info, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s does not exist", path)
		}
		return "", err
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a public key file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a public key file", path)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxKeyFileSize+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxKeyFileSize {
		return "", fmt.Errorf("%s is not a public key file", path)
	}
	if bytes.Contains(content, []byte("PRIVATE KEY")) {
		hint := ""
		if _, err := os.Stat(path + ".pub"); err == nil {
			hint = fmt.Sprintf(": pass %s.pub", path)
		}
		return "", fmt.Errorf("%s is a private key and is never sent%s", path, hint)
	}
	content = bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 && strings.HasPrefix(lines[0], "---- BEGIN SSH2 PUBLIC KEY") {
		return "", fmt.Errorf("%s is in the SSH2 format: convert it with `ssh-keygen -i -f %s`, then pass the result", path, path)
	}
	if len(lines) != 1 {
		return "", fmt.Errorf("%s must hold exactly one public key line, such as \"ssh-ed25519 AAAA... you@laptop\"", path)
	}
	line := lines[0]
	if strings.IndexFunc(line, func(r rune) bool { return r != '\t' && unicode.IsControl(r) }) >= 0 {
		return "", fmt.Errorf("%s is not a public key: the line holds control characters", path)
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", fmt.Errorf("%s is not a public key: expected \"ssh-ed25519 AAAA... you@laptop\"", path)
	}
	if !acceptedKeyTypes[fields[0]] {
		for _, field := range fields[1:] {
			if acceptedKeyTypes[field] {
				return "", fmt.Errorf("%s starts with options: remove everything before %s", path, field)
			}
		}
		return "", fmt.Errorf("%s holds a %s key: fortrabbit accepts ed25519 and rsa keys only; create one with `ssh-keygen -t ed25519`", path, iostreams.StripControl(fields[0]))
	}
	if blobType(fields[1]) != fields[0] {
		return "", fmt.Errorf("%s is not a public key: the key data does not match its type %s", path, fields[0])
	}
	return line, nil
}

// blobType is the key type encoded at the start of a base64 public key blob, or "" when the blob is not one.
func blobType(blob string) string {
	data, err := base64.StdEncoding.DecodeString(blob)
	if err != nil || len(data) < 4 {
		return ""
	}
	size := binary.BigEndian.Uint32(data[:4])
	if size == 0 || uint64(size) > uint64(len(data)-4) {
		return ""
	}
	return string(data[4 : 4+size])
}

func newCmdDelete(factory *app.Factory) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use:   "delete <ssh-key>",
		Short: "Delete an SSH key from your account",
		Long: "Delete an SSH key from your account. The key no longer reaches any of your environments.\n\n" +
			"Needs a browser sign-in (frbit auth login) approved with Allow deletes that is not limited to apps: an API token cannot delete keys.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := cmdutil.CheckDestructive(factory, "deletion", id, confirmation); err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			path := "/v1/ssh-keys/" + url.PathEscape(id)
			response, err := client.GetResource(cmd.Context(), path)
			if err != nil {
				return err
			}
			comment, _ := response.Resource["comment"].(string)
			fingerprint, _ := response.Resource["fingerprint"].(string)
			stderr := cmd.ErrOrStderr()
			label := strings.TrimSpace(iostreams.StripControl(comment) + " " + iostreams.StripControl(fingerprint))
			_, _ = fmt.Fprintf(stderr, "SSH key: %s (%s)\n\n", label, iostreams.StripControl(id))
			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, stderr, "deletion", id, confirmation, "The key no longer reaches any of your environments. This cannot be undone.")
			if err != nil {
				return err
			}
			if !confirmed {
				return cmdutil.Aborted(stderr)
			}
			if err := client.DeleteResource(cmd.Context(), path); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deleted SSH key %s.\n", iostreams.StripControl(id))
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm by repeating the SSH key ID")
	return command
}
