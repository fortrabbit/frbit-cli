package environments

import (
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

// metricsNow is the clock; tests replace it.
var metricsNow = time.Now

var metricRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

func newCmdMetrics(factory *app.Factory) *cobra.Command {
	var printJSON bool
	var rangeName, step string
	command := &cobra.Command{
		Use:   "metrics <environment> [slug]",
		Short: "Show usage against plan limits, or the history of one metric",
		Long:  "Without a slug: current usage per booked component with its limit, the last 24 hours, and the available metric slugs. With a slug: that metric over --range (default 24h), one point per --step (default hour for 1h and 24h, day for 7d and 30d).",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/v1/environments/" + url.PathEscape(args[0]) + "/metrics"
			var query url.Values
			if len(args) == 2 {
				var err error
				if query, err = metricQuery(rangeName, step, metricsNow()); err != nil {
					return err
				}
				path += "/" + url.PathEscape(args[1])
			} else if cmd.Flags().Changed("range") || cmd.Flags().Changed("step") {
				return fmt.Errorf("--range and --step need a metric slug")
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResourceWithQuery(cmd.Context(), path, query)
			if err != nil {
				return err
			}
			if printJSON {
				return writeRaw(cmd, response.Raw)
			}
			if len(args) == 2 {
				return writeMetricHistory(cmd.OutOrStdout(), response.Resource)
			}
			return writeMetricsSummary(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().StringVar(&rangeName, "range", "24h", "Range of a metric history: 1h, 24h, 7d or 30d")
	command.Flags().StringVar(&step, "step", "", "Point size of a metric history: hour or day (default by range)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func metricQuery(rangeName, step string, now time.Time) (url.Values, error) {
	length, ok := metricRanges[rangeName]
	if !ok {
		return nil, fmt.Errorf("--range must be 1h, 24h, 7d or 30d")
	}
	if step == "" {
		step = "hour"
		if length > 24*time.Hour {
			step = "day"
		}
	}
	if step != "hour" && step != "day" {
		return nil, fmt.Errorf("--step must be hour or day")
	}
	now = now.UTC()
	return url.Values{
		"from": {now.Add(-length).Format(time.RFC3339)},
		"to":   {now.Format(time.RFC3339)},
		"step": {step},
	}, nil
}

func writeMetricsSummary(out io.Writer, value api.Resource) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "COMPONENT\tMETRIC\tCURRENT\tLIMIT\tUSED")
	usage, _ := value["usage"].([]any)
	for _, raw := range usage {
		row, _ := raw.(map[string]any)
		percent := "-"
		if p, ok := row["percent"].(float64); ok {
			percent = fmt.Sprintf("%.1f%%", p)
		}
		period := ""
		if row["period"] == "month" {
			period = " (this month)"
		}
		_, _ = fmt.Fprintf(table, "%v\t%v\t%s%s\t%s\t%s\n", row["component"], row["metric"], formatBytes(row["current"]), period, formatBytes(row["limit"]), percent)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if last, ok := value["last24Hours"].(map[string]any); ok {
		_, _ = fmt.Fprintln(out, "\nLast 24 hours")
		table = tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintf(table, "requests\t%s\n", number(last["requests"]))
		errors, _ := last["errors"].(map[string]any)
		for _, status := range []string{"500", "503", "504"} {
			_, _ = fmt.Fprintf(table, "HTTP %s\t%s\n", status, number(errors[status]))
		}
		times, _ := last["responseTimeMs"].(map[string]any)
		_, _ = fmt.Fprintf(table, "response time median\t%s ms\n", number(times["median"]))
		_, _ = fmt.Fprintf(table, "response time p99\t%s ms\n", number(times["p99"]))
		_, _ = fmt.Fprintf(table, "PHP memory peak\t%s\n", percentValue(last["phpMemoryMaxPercent"]))
		kills, _ := last["outOfMemoryKills"].(map[string]any)
		_, _ = fmt.Fprintf(table, "out-of-memory kills (php)\t%s\n", number(kills["php"]))
		_, _ = fmt.Fprintf(table, "out-of-memory kills (jobs)\t%s\n", number(kills["jobs"]))
		if err := table.Flush(); err != nil {
			return err
		}
	}
	if slugs, _ := value["metrics"].([]any); len(slugs) > 0 {
		names := make([]string, 0, len(slugs))
		for _, slug := range slugs {
			names = append(names, fmt.Sprint(slug))
		}
		_, _ = fmt.Fprintf(out, "\nMetrics: %s\nHistory: frbit environments metrics <environment> <slug> --range 7d\n", strings.Join(names, ", "))
	}
	return nil
}

func writeMetricHistory(out io.Writer, value api.Resource) error {
	_, _ = fmt.Fprintf(out, "%v (%v), %v to %v, one point per %v\n", value["slug"], value["unit"], value["from"], value["to"], value["step"])
	if coverage, ok := value["coverage"].(map[string]any); ok && coverage["status"] != "complete" {
		_, _ = fmt.Fprintf(out, "Coverage: %v\n", coverage["status"])
	}
	if totals, _ := value["totals"].([]any); len(totals) > 0 {
		_, _ = fmt.Fprintln(out)
		table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, raw := range totals {
			row, _ := raw.(map[string]any)
			_, _ = fmt.Fprintf(table, "%v\t%s\n", row["label"], number(row["value"]))
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	series, _ := value["series"].(map[string]any)
	names := make([]string, 0, len(series))
	for name := range series {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	// Every series has a point per bucket, so rows are indexed by position.
	columns := make([][]any, len(names))
	rows := 0
	for index, name := range names {
		columns[index], _ = series[name].([]any)
		if len(columns[index]) > rows {
			rows = len(columns[index])
		}
	}
	_, _ = fmt.Fprintln(out)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "TIME\t%s\n", strings.ToUpper(strings.Join(names, "\t")))
	for row := 0; row < rows; row++ {
		cells := make([]string, len(names))
		timestamp := ""
		for index := range names {
			if row >= len(columns[index]) {
				cells[index] = "-"
				continue
			}
			point, _ := columns[index][row].([]any)
			if len(point) == 2 {
				timestamp = fmt.Sprint(point[0])
				cells[index] = number(point[1])
			}
		}
		_, _ = fmt.Fprintf(table, "%s\t%s\n", timestamp, strings.Join(cells, "\t"))
	}
	return table.Flush()
}

func number(value any) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprint(value)
}

func percentValue(value any) string {
	if value == nil {
		return "-"
	}
	return number(value) + "%"
}

func formatBytes(value any) string {
	v, ok := value.(float64)
	if !ok {
		return "-"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	index := 0
	for v >= 1024 && index < len(units)-1 {
		v /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d B", int64(v))
	}
	return fmt.Sprintf("%.1f %s", v, units[index])
}
