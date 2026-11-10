// Package jobs builds the jobs command group: workers and cron jobs of an environment.
package jobs

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/spf13/cobra"
)

var spec = resource.Spec{
	Use: "jobs", Singular: "job", Path: "/jobs",
	Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "TYPE", Key: "type"}, {Header: "COMMAND", Key: "command"}, {Header: "INTERVAL", Key: "interval"}, {Header: "ACTIVE", Key: "isActive"}},
}

func NewCmdJobs(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "jobs", Short: "Manage the workers and cron jobs of an environment", Args: cobra.NoArgs}
	command.AddCommand(link.Documented(newCmdList(factory), false), newCmdGet(factory), link.Documented(newCmdCreate(factory), true), newCmdUpdate(factory), newCmdDelete(factory))
	return command
}

func newCmdList(factory *app.Factory) *cobra.Command {
	var printJSON, all bool
	var limit int
	command := &cobra.Command{
		Use:   "list [environment]",
		Short: "List the jobs of an environment and the limit of the booked jobs plan",
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
			response, err := resource.ListPaged(cmd, client, "/v1/environments/"+url.PathEscape(env)+"/jobs", limit, all)
			if err != nil {
				return err
			}
			if printJSON {
				return resource.WriteRaw(cmd, response.Raw)
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
	resource.AddPageFlags(command, "jobs", &limit, &all)
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
			response, err := client.GetResource(cmd.Context(), "/v1/jobs/"+url.PathEscape(args[0]))
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
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
	timeoutUsage, signalUsage := "Seconds the job gets to finish after the exit signal: 1, 5, 10 or 30", "Signal sent when the job is stopped: TERM or INT"
	if !update {
		timeoutUsage += " (default 30)"
		signalUsage += " (default TERM)"
	}
	f.IntVar(&j.exitTimeout, "exit-timeout", 0, timeoutUsage)
	f.StringVar(&j.exitSignal, "exit-signal", "", signalUsage)
	f.BoolVar(&j.inactive, "inactive", false, "Keep the job but do not run it")
	if update {
		f.BoolVar(&j.active, "active", false, "Run the job")
		command.MarkFlagsMutuallyExclusive("active", "inactive")
	}
}

// payload collects the flags that were set; the API validates the values.
func (j *jobFlags) payload(cmd *cobra.Command) map[string]any {
	flags := cmd.Flags()
	body := map[string]any{}
	for flag, field := range map[string]struct {
		key   string
		value any
	}{
		"type": {"type", j.jobType}, "interval": {"interval", j.interval}, "exit-signal": {"exitSignal", j.exitSignal},
		"name": {"name", j.name}, "command": {"command", j.command}, "expression": {"expression", j.expression},
		"exit-timeout": {"exitTimeout", j.exitTimeout}, "max-runtime": {"maxRuntime", j.maxRuntime},
	} {
		if flags.Changed(flag) {
			body[field.key] = field.value
		}
	}
	if flags.Changed("inactive") {
		body["isActive"] = !j.inactive
	}
	if flags.Changed("active") {
		body["isActive"] = j.active
	}
	return body
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var j jobFlags
	var printJSON bool
	command := &cobra.Command{
		Use:   "create <environment>",
		Short: "Add a worker or a cron job to an environment",
		Long: "Add a worker or a cron job to an environment.\n\n" +
			`A worker needs --name, --command and --type worker, e.g. --command "php artisan queue:work".` + "\n" +
			"A cron job also needs --interval and --max-runtime, and --expression with --interval custom.\n" +
			"Jobs count against the booked jobs plan; see frbit jobs list.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := j.payload(cmd)
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.WriteEnvironment(cmd, client, link.FirstArg(args), "")
			if err != nil {
				return err
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(env)+"/jobs", body)
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
		},
	}
	j.bind(command, false)
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdUpdate(factory *app.Factory) *cobra.Command {
	var j jobFlags
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <job>",
		Short: "Change a job; only the flags you pass are sent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := j.payload(cmd)
			if len(body) == 0 {
				return fmt.Errorf("pass at least one flag to change")
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/jobs/"+url.PathEscape(args[0]), body)
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
		},
	}
	j.bind(command, true)
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func writeJob(cmd *cobra.Command, response api.ResourceResponse, printJSON bool) error {
	return resource.Write(cmd, response, printJSON)
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
			response, err := client.GetResource(cmd.Context(), "/v1/jobs/"+url.PathEscape(id))
			if err != nil {
				return err
			}
			name, _ := response.Resource["name"].(string)
			stderr := cmd.ErrOrStderr()
			_, _ = fmt.Fprintf(stderr, "Job: %s (%s)\n\n", iostreams.StripControl(name), id)
			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, stderr, "deletion", id, confirmation, "The job stops running and is removed from its environment. This cannot be undone.")
			if err != nil {
				return err
			}
			if !confirmed {
				return cmdutil.Aborted(stderr)
			}
			if err := client.DeleteResource(cmd.Context(), "/v1/jobs/"+url.PathEscape(id)); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deleted job %s.\n", id)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm by repeating the job ID")
	return command
}
