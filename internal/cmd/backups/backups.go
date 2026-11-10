// Package backups builds the backups command group: list, create, restore and download environment backups.
package backups

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/spf13/cobra"
)

var downloadTypes = []string{"database", "files"}

var spec = resource.Spec{
	Use: "backups", Singular: "backup", Path: "/backups",
	Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "STATUS", Key: "status"}, {Header: "TRIGGER", Key: "trigger"}, {Header: "CREATED", Key: "createdAt"}, {Header: "FILES BYTES", Key: "filesSizeBytes"}, {Header: "DB BYTES", Key: "databaseSizeBytes"}, {Header: "AVAILABLE UNTIL", Key: "availableUntil"}},
}

func NewCmdBackups(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "backups", Short: "List, create, restore and download backups of an environment", Args: cobra.NoArgs}
	command.AddCommand(link.Documented(newCmdList(factory), false), newCmdGet(factory), link.Documented(newCmdCreate(factory), true), newCmdRestore(factory), newCmdDownload(factory))
	return command
}

func newCmdList(factory *app.Factory) *cobra.Command {
	var printJSON, all bool
	var limit int
	command := &cobra.Command{
		Use:   "list [environment]",
		Short: "List the backups of an environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.ReadEnvironment(cmd, client, link.FirstArg(args))
			if err != nil {
				return err
			}
			response, err := resource.ListPaged(cmd, client, "/v1/environments/"+url.PathEscape(env)+"/backups", limit, all)
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return resource.WriteResources(cmd.OutOrStdout(), response.Resources, response.TotalItems, spec)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	resource.AddPageFlags(command, "backups", &limit, &all)
	return command
}

func newCmdGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <backup>",
		Short: "Get a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/backups/"+url.PathEscape(args[0]))
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return resource.WriteResource(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "create <environment>",
		Short: "Request a backup of an environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.WriteEnvironment(cmd, client, link.FirstArg(args), "")
			if err != nil {
				return err
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(env)+"/backups", map[string]any{})
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			status, _ := response.Resource["status"].(string)
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Backup of %s requested (%s). It shows up in `frbit backups list %s` once it is completed.\n", env, status, env)
			return err
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdRestore(factory *app.Factory) *cobra.Command {
	var confirmation string
	var printJSON bool
	command := &cobra.Command{
		Use:   "restore <backup>",
		Short: "Restore a backup, overwriting the live data of its environment",
		Long:  "Restore a backup, overwriting the live data of its environment. It shows the environment and the date of the backup and asks you to type the environment ID; without a terminal, pass --confirm <environment-id>.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			// The environment ID to confirm comes from the backup, so it is read before the check.
			response, err := client.GetResource(cmd.Context(), "/v1/backups/"+url.PathEscape(id))
			if err != nil {
				return err
			}
			environment, _ := response.Resource["environment"].(string)
			if environment == "" {
				return fmt.Errorf("the API names no environment for backup %s; nothing was restored", id)
			}
			if confirmation != "" && confirmation != environment && strings.HasPrefix(confirmation, "ba-") {
				return fmt.Errorf("--confirm takes the environment ID; pass --confirm %s", environment)
			}
			if err := cmdutil.CheckDestructive(factory, "restore", environment, confirmation); err != nil {
				return err
			}
			env, err := client.GetResource(cmd.Context(), "/v1/environments/"+url.PathEscape(environment))
			if err != nil {
				return err
			}
			name, _ := env.Resource["name"].(string)
			created, _ := response.Resource["createdAt"].(string)
			stderr := cmd.ErrOrStderr()
			_, _ = fmt.Fprintf(stderr, "Backup:      %s, created %s\nEnvironment: %s (%s)\n\n", id, iostreams.StripControl(created), iostreams.StripControl(name), environment)
			warning := "Restoring overwrites the live data of " + environment + ": its database and its files are replaced by the state of this backup. Changes made since are lost. This cannot be undone."
			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, stderr, "restore", environment, confirmation, warning)
			if err != nil {
				return err
			}
			if !confirmed {
				return cmdutil.Aborted(stderr)
			}
			restored, err := client.CreateResource(cmd.Context(), "/v1/backups/"+url.PathEscape(id)+"/restore", map[string]any{})
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", restored.Raw)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Restore of %s to %s requested.\n", id, environment)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm the restore by repeating the environment ID (en-…) of the backup")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response of the restore request as JSON")
	return command
}

// DownloadTransport returns the transport for storage downloads; it never carries the API token. Tests replace it.
var DownloadTransport = func() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment}
}

func newCmdDownload(factory *app.Factory) *cobra.Command {
	var downloadType, output string
	var force bool
	command := &cobra.Command{
		Use:   "download <backup>",
		Short: "Download the files or the database of a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !slices.Contains(downloadTypes, downloadType) {
				return fmt.Errorf("--type must be one of %s", strings.Join(downloadTypes, ", "))
			}
			// Fail before the API request; the name from the storage answer is checked again after it.
			if output != "" && output != "-" {
				if _, err := cmdutil.OutputPath(output, id, force); err != nil {
					return err
				}
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/backups/"+url.PathEscape(id)+"/download/"+downloadType)
			if err != nil {
				return err
			}
			link, _ := response.Resource["url"].(string)
			if link == "" {
				return errors.New("the API returned no download link")
			}
			name, err := download(cmd.Context(), cmd.OutOrStdout(), link, client.Secure(), id+"-"+downloadType, output, force)
			if err != nil {
				return err
			}
			if name != "" {
				_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Saved %s\n", name)
			}
			return err
		},
	}
	command.Flags().StringVar(&downloadType, "type", "files", "What to download: database or files")
	command.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
	command.Flags().StringVar(&output, "output", "", `File or folder to write to; "-" writes to stdout`)
	return command
}

// download streams the signed link to a temp file next to the target and renames it on success. The link carries its own authorisation, so no API token is sent.
// With secure set, as for an https API, the link and every redirect must be https, so the signed link never travels in clear text.
func download(ctx context.Context, stdout io.Writer, link string, secure bool, baseName, output string, force bool) (string, error) {
	parsed, err := url.Parse(link)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("the API returned an invalid download link")
	}
	if secure && parsed.Scheme != "https" {
		return "", errors.New("the backup link is not https; refusing to download")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	// No overall timeout: backups are large, the context ends the transfer.
	transport := DownloadTransport()
	transport.ResponseHeaderTimeout = 30 * time.Second
	client := &http.Client{Transport: transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if secure && next.URL.Scheme != "https" {
			return errors.New("the download redirected to plain http; refusing")
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download backup: %w", withoutURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download backup: the storage answered %s (the link may have expired)", response.Status)
	}
	if output == "-" {
		_, err = io.Copy(stdout, response.Body)
		return "", err
	}

	target, err := cmdutil.OutputPath(output, baseName+extension(response.Header.Get("Content-Disposition"), parsed.Path), force)
	if err != nil {
		return "", err
	}
	if err := cmdutil.WriteFileAtomic(target, 0o600, func(out io.Writer) error {
		_, err := io.Copy(out, response.Body)
		return err
	}); err != nil {
		return "", fmt.Errorf("download backup: %w", err)
	}
	return target, nil
}

// withoutURL drops the request URL from a transport error: the signed link is the only authorisation for the backup.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// extension takes the file extension from Content-Disposition, else from the link path.
func extension(disposition, urlPath string) string {
	name := ""
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		name = params["filename"]
	}
	if name == "" {
		name = path.Base(urlPath)
	}
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	ext := path.Ext(name)
	if ext == ".gz" {
		if inner := path.Ext(strings.TrimSuffix(name, ext)); inner != "" {
			return inner + ext
		}
	}
	return ext
}
