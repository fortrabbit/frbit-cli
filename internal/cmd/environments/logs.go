package environments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/spf13/cobra"
)

const maxLogLimit = 1000

type logEntry struct {
	Time    string         `json:"time"`
	Level   *string        `json:"level"`
	Message string         `json:"message"`
	Meta    map[string]any `json:"meta"`
}

func newCmdLogs(factory *app.Factory) *cobra.Command {
	var source, since, status, method string
	var limit int
	var printJSON bool
	command := &cobra.Command{
		Use:   "logs [environment]",
		Short: "Show the logs of an environment",
		Long:  "Prints the newest --limit entries of one log source, oldest first. Logs are kept for 14 days. --status and --method filter the access log only.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch source {
			case "php", "apache", "access", "jobs":
			default:
				return fmt.Errorf("--source must be php, apache, access or jobs")
			}
			if limit < 1 || limit > maxLogLimit {
				return fmt.Errorf("--limit must be between 1 and %d", maxLogLimit)
			}
			if (status != "" || method != "") && source != "access" {
				return fmt.Errorf("--status and --method need --source access")
			}
			query := url.Values{"source": {source}}
			if since != "" {
				query.Set("since", since)
			}
			if status != "" {
				query.Set("status", status)
			}
			if method != "" {
				query.Set("method", strings.ToUpper(method))
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.ReadEnvironment(cmd, client, link.FirstArg(args))
			if err != nil {
				return err
			}
			newest, raw, pages, err := fetchLogs(cmd.Context(), client, "/v1/environments/"+url.PathEscape(env)+"/logs", query, limit)
			if err != nil {
				return err
			}
			if printJSON {
				if pages == 1 {
					return resource.WriteRaw(cmd, raw)
				}
				merged, err := json.Marshal(newest)
				if err != nil {
					return err
				}
				return resource.WriteRaw(cmd, merged)
			}
			out := cmd.OutOrStdout()
			if len(newest) == 0 {
				_, err := fmt.Fprintln(out, "No log entries found.")
				return err
			}
			return writeLogEntries(out, reversed(newest))
		},
	}
	command.Flags().StringVar(&source, "source", "php", "Log source: php, apache, access or jobs")
	command.Flags().IntVar(&limit, "limit", 100, "Number of entries to show (1-1000)")
	command.Flags().StringVar(&since, "since", "", "Oldest entry: a duration back from now (15m, 1h, 7d) or an ISO 8601 time with a time zone")
	command.Flags().StringVar(&status, "status", "", "Access log only: HTTP status, class or list (500, 5xx, 500,503)")
	command.Flags().StringVar(&method, "method", "", "Access log only: HTTP method, e.g. GET")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the entries as JSON")
	return command
}

// fetchLogs reads up to limit entries, newest first, paging back through rel="next".
// It returns them with the body of the first page and the page count.
func fetchLogs(ctx context.Context, client *api.Client, path string, query url.Values, limit int) ([]logEntry, []byte, int, error) {
	var newest []logEntry
	var firstBody []byte
	pages := 0
	query = cloneQuery(query)
	query.Set("limit", fmt.Sprint(limit))
	for len(newest) < limit {
		body, link, err := client.GetWithLinks(ctx, path, query)
		if err != nil {
			return nil, nil, 0, err
		}
		var batch []logEntry
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, nil, 0, fmt.Errorf("decode logs: %w", err)
		}
		if pages == 0 {
			firstBody = body
		}
		pages++
		newest = append(newest, batch...)
		next := api.LinkRel(link, "next")
		if next == "" || len(batch) == 0 {
			break
		}
		if path, query, err = client.ResolveLink(next); err != nil {
			return nil, nil, 0, err
		}
	}
	if len(newest) > limit {
		newest = newest[:limit]
		pages = 2
	}
	return newest, firstBody, pages, nil
}

func writeLogEntries(out io.Writer, entries []logEntry) error {
	for _, entry := range entries {
		level := ""
		if entry.Level != nil {
			level = iostreams.StripControl(*entry.Level)
		}
		lines := strings.Split(strings.ReplaceAll(entry.Message, "\r\n", "\n"), "\n")
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\n", iostreams.StripControl(entry.Time), level, iostreams.StripControl(lines[0])); err != nil {
			return err
		}
		for _, line := range lines[1:] {
			if _, err := fmt.Fprintf(out, "  %s\n", iostreams.StripControl(line)); err != nil {
				return err
			}
		}
	}
	return nil
}

func reversed(entries []logEntry) []logEntry {
	out := make([]logEntry, len(entries))
	for index, entry := range entries {
		out[len(entries)-1-index] = entry
	}
	return out
}

func cloneQuery(query url.Values) url.Values {
	out := url.Values{}
	for key, values := range query {
		out[key] = append([]string(nil), values...)
	}
	return out
}
