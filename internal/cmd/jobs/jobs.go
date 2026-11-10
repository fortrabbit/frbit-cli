// Package jobs builds the jobs command group: workers and cron jobs of an environment.
package jobs

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

var (
	jobTypes     = []string{"worker", "cron"}
	intervals    = []string{"minute", "hour", "day", "custom"}
	signals      = []string{"TERM", "INT"}
	exitTimeouts = []int{1, 5, 10, 30}
	maxRuntimes  = []int{300, 1800, 3600, 10800, 21600}
)

var spec = resource.Spec{
	Use: "jobs", Singular: "job", Path: "/jobs",
	Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "TYPE", Key: "type"}, {Header: "COMMAND", Key: "command"}, {Header: "INTERVAL", Key: "interval"}, {Header: "ACTIVE", Key: "isActive"}},
}

func NewCmdJobs(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "jobs", Short: "Manage the workers and cron jobs of an environment", Args: cobra.NoArgs}
	command.AddCommand(newCmdList(factory), newCmdGet(factory), newCmdCreate(factory), newCmdUpdate(factory), newCmdDelete(factory))
	return command
}

func newCmdList(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "list <environment>",
		Short: "List the jobs of an environment and the limit of the booked jobs plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.ListResources(cmd.Context(), "/v1/environments/"+args[0]+"/jobs", 0, nil, nil)
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			out := cmd.OutOrStdout()
			if err := resource.WriteResources(out, response.Resources, response.TotalItems, spec); err != nil {
				return err
			}
			count, limit := response.Header.Get("X-Jobs-Count"), response.Header.Get("X-Jobs-Limit")
			if count == "" {
				count = strconv.Itoa(len(response.Resources))
			}
			if limit == "" {
				limit = "?"
			}
			_, err = fmt.Fprintf(out, "Jobs: %s of %s\n", count, limit)
			return err
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <job>",
		Short: "Get a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/jobs/"+args[0])
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

type jobFlags struct {
	name, command, jobType, interval, expression, exitSignal string
	exitTimeout, maxRuntime                                  int
	inactive, active                                         bool
}

func (j *jobFlags) bind(command *cobra.Command, update bool) {
	f := command.Flags()
	f.StringVar(&j.name, "name", "", "Name to recognise the job by")
	f.StringVar(&j.command, "command", "", `Command to run; must start with "php " or "bash "`)
	f.StringVar(&j.jobType, "type", "", "worker (runs all the time) or cron (runs on a schedule)")
	f.StringVar(&j.interval, "interval", "", "Cron only: minute, hour, day or custom")
	f.StringVar(&j.expression, "expression", "", "Cron only: crontab expression, required with --interval custom")
	f.IntVar(&j.maxRuntime, "max-runtime", 0, "Cron only: seconds a run may take: 300, 1800, 3600, 10800 or 21600")
	f.IntVar(&j.exitTimeout, "exit-timeout", 0, "Seconds the job gets to finish after the exit signal: 1, 5, 10 or 30 (default 30)")
	f.StringVar(&j.exitSignal, "exit-signal", "", "Signal sent when the job is stopped: TERM or INT (default TERM)")
	f.BoolVar(&j.inactive, "inactive", false, "Keep the job but do not run it")
	if update {
		f.BoolVar(&j.active, "active", false, "Run the job")
		command.MarkFlagsMutuallyExclusive("active", "inactive")
	}
}

// payload collects the flags that were set, validated against the enums of the API.
func (j *jobFlags) payload(cmd *cobra.Command) (map[string]any, error) {
	flags := cmd.Flags()
	body := map[string]any{}
	for _, field := range []struct {
		flag, key string
		value     string
		allowed   []string
	}{
		{"type", "type", j.jobType, jobTypes},
		{"interval", "interval", j.interval, intervals},
		{"exit-signal", "exitSignal", j.exitSignal, signals},
		{"name", "name", j.name, nil},
		{"command", "command", j.command, nil},
		{"expression", "expression", j.expression, nil},
	} {
		if !flags.Changed(field.flag) {
			continue
		}
		if field.value == "" {
			return nil, fmt.Errorf("--%s must not be empty", field.flag)
		}
		if field.allowed != nil && !slices.Contains(field.allowed, field.value) {
			return nil, fmt.Errorf("--%s must be one of %s", field.flag, strings.Join(field.allowed, ", "))
		}
		body[field.key] = field.value
	}
	for _, field := range []struct {
		flag, key string
		value     int
		allowed   []int
	}{{"exit-timeout", "exitTimeout", j.exitTimeout, exitTimeouts}, {"max-runtime", "maxRuntime", j.maxRuntime, maxRuntimes}} {
		if !flags.Changed(field.flag) {
			continue
		}
		if !slices.Contains(field.allowed, field.value) {
			return nil, fmt.Errorf("--%s must be one of %s", field.flag, joinInts(field.allowed))
		}
		body[field.key] = field.value
	}
	if command, ok := body["command"].(string); ok && !strings.HasPrefix(command, "php ") && !strings.HasPrefix(command, "bash ") {
		return nil, fmt.Errorf(`--command must start with "php " or "bash "`)
	}
	if flags.Changed("inactive") {
		body["isActive"] = !j.inactive
	}
	if flags.Changed("active") {
		body["isActive"] = j.active
	}
	return body, nil
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ", ")
}

// checkExpression rejects a crontab expression the API cannot schedule.
func checkExpression(cmd *cobra.Command, client *api.Client, expression string) error {
	response, err := client.GetResourceWithQuery(cmd.Context(), "/v1/jobs/check-cron", url.Values{"expression": {expression}})
	if err != nil {
		return err
	}
	if valid, _ := response.Resource["valid"].(bool); !valid {
		return fmt.Errorf("cron expression %q is not valid", expression)
	}
	return nil
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var j jobFlags
	var printJSON bool
	command := &cobra.Command{
		Use:   "create <environment>",
		Short: "Add a worker or a cron job to an environment",
		Long: "Add a worker or a cron job to an environment.\n\n" +
			`A worker needs --name, --command and --type worker, e.g. --command "php artisan queue:work".` + "\n" +
			"A cron job also needs --interval and --max-runtime, and --expression with --interval custom (checked before the job is created).\n" +
			"Jobs count against the booked jobs plan; see frbit jobs list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := j.payload(cmd)
			if err != nil {
				return err
			}
			for _, required := range []string{"name", "command", "type"} {
				if _, ok := body[required]; !ok {
					return fmt.Errorf("--%s is required", required)
				}
			}
			if body["type"] == "worker" {
				for _, flag := range []string{"interval", "expression", "max-runtime"} {
					if cmd.Flags().Changed(flag) {
						return fmt.Errorf("--%s only applies to --type cron", flag)
					}
				}
			} else {
				for _, required := range []string{"interval", "maxRuntime"} {
					if _, ok := body[required]; !ok {
						return fmt.Errorf("--%s is required for --type cron", map[string]string{"interval": "interval", "maxRuntime": "max-runtime"}[required])
					}
				}
				if err := validateExpression(body, true); err != nil {
					return err
				}
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if expression, ok := body["expression"].(string); ok {
				if err := checkExpression(cmd, client, expression); err != nil {
					return err
				}
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/environments/"+args[0]+"/jobs", body)
			if err != nil {
				return err
			}
			return writeJob(cmd, response, printJSON)
		},
	}
	j.bind(command, false)
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

// validateExpression ties --expression to --interval custom; an update may rely on the stored expression.
func validateExpression(body map[string]any, requireCustom bool) error {
	interval, _ := body["interval"].(string)
	_, hasExpression := body["expression"]
	if hasExpression && interval != "" && interval != "custom" {
		return fmt.Errorf("--expression only applies with --interval custom")
	}
	if requireCustom && interval == "custom" && !hasExpression {
		return fmt.Errorf("--expression is required with --interval custom")
	}
	return nil
}

func newCmdUpdate(factory *app.Factory) *cobra.Command {
	var j jobFlags
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <job>",
		Short: "Change a job; only the flags you pass are sent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := j.payload(cmd)
			if err != nil {
				return err
			}
			if len(body) == 0 {
				return fmt.Errorf("pass at least one flag to change")
			}
			if err := validateExpression(body, false); err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if expression, ok := body["expression"].(string); ok {
				if err := checkExpression(cmd, client, expression); err != nil {
					return err
				}
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/jobs/"+args[0], body)
			if err != nil {
				return err
			}
			return writeJob(cmd, response, printJSON)
		},
	}
	j.bind(command, true)
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func writeJob(cmd *cobra.Command, response api.ResourceResponse, printJSON bool) error {
	if printJSON {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
		return err
	}
	return resource.WriteResource(cmd.OutOrStdout(), response.Resource)
}

func newCmdDelete(factory *app.Factory) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use:   "delete <job>",
		Short: "Delete a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := cmdutil.CheckDestructive(factory, "deletion", id, confirmation); err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/jobs/"+id)
			if err != nil {
				return err
			}
			name, _ := response.Resource["name"].(string)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Job: %s (%s)\n\n", name, id)
			confirmed, err := cmdutil.ConfirmDestructive(factory, out, "deletion", id, confirmation, "The job stops running and is removed from its environment. This cannot be undone.")
			if err != nil {
				return err
			}
			if !confirmed {
				_, err = fmt.Fprintln(out, "Aborted.")
				return err
			}
			if err := client.DeleteResource(cmd.Context(), "/v1/jobs/"+id); err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Deleted job %s.\n", id)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm deletion by repeating the job ID")
	return command
}
