package environments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

// logsPollInterval is the wait between polls of --follow; tests replace it.
var logsPollInterval = 2 * time.Second

const followPageLimit = 1000

type logEntry struct {
	Time    string         `json:"time"`
	Level   *string        `json:"level"`
	Message string         `json:"message"`
	Meta    map[string]any `json:"meta"`
}

func newCmdLogs(factory *app.Factory) *cobra.Command {
	var source, since, status, method string
	var limit int
	var follow, printJSON bool
	command := &cobra.Command{
		Use:   "logs <environment>",
		Short: "Show the logs of an environment, optionally following new entries",
		Long:  "Prints the newest --limit entries of one log source, oldest first. --follow keeps polling and prints new entries until Ctrl-C. Logs are kept for 14 days. --status and --method filter the access log only.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch source {
			case "php", "apache", "access", "jobs":
			default:
				return fmt.Errorf("--source must be php, apache, access or jobs")
			}
			if limit < 1 || limit > followPageLimit {
				return fmt.Errorf("--limit must be between 1 and %d", followPageLimit)
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
			path := "/v1/environments/" + args[0] + "/logs"
			out := cmd.OutOrStdout()
			newest, raw, pages, prev, err := fetchLogs(cmd.Context(), client, path, query, limit)
			if err != nil {
				if follow && cmd.Context().Err() != nil {
					return nil
				}
				return err
			}
			if !follow {
				if printJSON {
					if pages == 1 {
						return writeRaw(cmd, raw)
					}
					merged, err := json.Marshal(newest)
					if err != nil {
						return err
					}
					return writeRaw(cmd, merged)
				}
				if len(newest) == 0 {
					_, err := fmt.Fprintln(out, "No log entries found.")
					return err
				}
				return writeLogEntries(out, reversed(newest), false)
			}
			if err := writeLogEntries(out, reversed(newest), printJSON); err != nil {
				return err
			}
			return followLogs(cmd.Context(), client, out, path, query, prev, printJSON)
		},
	}
	command.Flags().StringVar(&source, "source", "php", "Log source: php, apache, access or jobs")
	command.Flags().IntVar(&limit, "limit", 100, "Number of entries to show (1-1000); with --follow, the entries shown before following")
	command.Flags().StringVar(&since, "since", "", "Oldest entry: a duration back from now (15m, 1h, 7d) or an ISO 8601 time with a time zone")
	command.Flags().StringVar(&status, "status", "", "Access log only: HTTP status, class or list (500, 5xx, 500,503)")
	command.Flags().StringVar(&method, "method", "", "Access log only: HTTP method, e.g. GET")
	command.Flags().BoolVar(&follow, "follow", false, "Keep polling and print new entries until interrupted")
	command.Flags().BoolVar(&printJSON, "json", false, "Print entries as JSON (one object per line with --follow)")
	return command
}

// fetchLogs reads up to limit entries, newest first, paging back through rel="next".
// It returns them with the body of the first page, the page count and the rel="prev" link of the first page.
func fetchLogs(ctx context.Context, client *api.Client, path string, query url.Values, limit int) ([]logEntry, []byte, int, string, error) {
	var newest []logEntry
	var firstBody []byte
	prev := ""
	pages := 0
	query = cloneQuery(query)
	query.Set("limit", fmt.Sprint(limit))
	for len(newest) < limit {
		body, link, err := client.GetWithLinks(ctx, path, query)
		if err != nil {
			return nil, nil, 0, "", err
		}
		var batch []logEntry
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, nil, 0, "", fmt.Errorf("decode logs: %w", err)
		}
		if pages == 0 {
			prev = api.LinkRel(link, "prev")
			firstBody = body
		}
		pages++
		newest = append(newest, batch...)
		next := api.LinkRel(link, "next")
		if next == "" || len(batch) == 0 {
			break
		}
		if path, query, err = client.ResolveLink(next); err != nil {
			return nil, nil, 0, "", err
		}
	}
	if len(newest) > limit {
		newest = newest[:limit]
		pages = 2
	}
	return newest, firstBody, pages, prev, nil
}

// followLogs polls the rel="prev" cursor until the context ends; Ctrl-C is a clean stop.
// Transient errors (429, 503, network) wait and retry.
func followLogs(ctx context.Context, client *api.Client, out io.Writer, path string, query url.Values, prev string, asJSON bool) error {
	skip := 0
	for {
		err := pollLogs(ctx, client, out, path, query, &prev, &skip, asJSON)
		wait := logsPollInterval
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			delay, transient := api.TransientDelay(err, logsPollInterval)
			if !transient {
				return err
			}
			wait = delay
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// pollLogs drains the pages after the cursor. skip counts the lines already printed for the
// current cursor, so a page without a newer cursor is never printed twice.
func pollLogs(ctx context.Context, client *api.Client, out io.Writer, path string, query url.Values, prev *string, skip *int, asJSON bool) error {
	if *prev == "" {
		// An empty log has no cursor yet; ask again until the first entries arrive.
		entries, _, _, cursor, err := fetchLogs(ctx, client, path, query, followPageLimit)
		if err != nil {
			return err
		}
		if err := writeLogEntries(out, reversed(entries), asJSON); err != nil {
			return err
		}
		*prev, *skip = cursor, 0
		return nil
	}
	for {
		next, nextQuery, err := client.ResolveLink(*prev)
		if err != nil {
			return err
		}
		nextQuery.Set("limit", fmt.Sprint(followPageLimit))
		body, link, err := client.GetWithLinks(ctx, next, nextQuery)
		if err != nil {
			return err
		}
		var batch []logEntry
		if err := json.Unmarshal(body, &batch); err != nil {
			return fmt.Errorf("decode logs: %w", err)
		}
		fresh := batch
		if len(fresh) > *skip {
			fresh = fresh[:len(fresh)-*skip]
		} else {
			fresh = nil
		}
		if err := writeLogEntries(out, reversed(fresh), asJSON); err != nil {
			return err
		}
		cursor := api.LinkRel(link, "prev")
		if cursor == "" {
			*skip += len(fresh)
			return nil
		}
		*prev, *skip = cursor, 0
		if len(batch) == 0 {
			return nil
		}
	}
}

func writeLogEntries(out io.Writer, entries []logEntry, asJSON bool) error {
	for _, entry := range entries {
		if asJSON {
			raw, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "%s\n", raw); err != nil {
				return err
			}
			continue
		}
		level := ""
		if entry.Level != nil {
			level = stripLogControl(*entry.Level)
		}
		lines := strings.Split(strings.ReplaceAll(entry.Message, "\r\n", "\n"), "\n")
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\n", stripLogControl(entry.Time), level, stripLogControl(lines[0])); err != nil {
			return err
		}
		for _, line := range lines[1:] {
			if _, err := fmt.Fprintf(out, "  %s\n", stripLogControl(line)); err != nil {
				return err
			}
		}
	}
	return nil
}

func stripLogControl(value string) string {
	return strings.Map(func(character rune) rune {
		if character != '\t' && unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
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
