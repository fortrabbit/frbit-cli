package environments

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/fortrabbit/frbit-cli/internal/link"
	"github.com/spf13/cobra"
)

func newCmdPhpSettings(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "php-settings", Short: "Read and update php.ini settings and extensions", Args: cobra.NoArgs}
	command.AddCommand(link.Documented(newCmdPhpSettingsGet(factory), false), link.Documented(newCmdPhpSettingsUpdate(factory), true))
	return command
}

func newCmdPhpSettingsGet(factory *app.Factory) *cobra.Command {
	return newEnvGet(factory, "Get PHP settings and extensions", "/php-settings", resource.WriteResource)
}

func newCmdPhpSettingsUpdate(factory *app.Factory) *cobra.Command {
	var setValues, extensionValues []string
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <environment>",
		Short: "Change php.ini settings and enable or disable extensions",
		Long:  "Change php.ini settings and enable or disable extensions. Valid directives and values: 'frbit php-settings list'. Valid extensions: 'frbit php-extensions list'. Directives and extensions you do not name stay unchanged.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := phpSettingsPayload(setValues, extensionValues)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.WriteEnvironment(cmd, client, link.FirstArg(args), "")
			if err != nil {
				return err
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(env)+"/php-settings", payload)
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
		},
	}
	command.Flags().StringArrayVar(&setValues, "set", nil, "php.ini directive as KEY=VALUE (repeatable)")
	command.Flags().StringArrayVar(&extensionValues, "extension", nil, "Extension as NAME=on|off (repeatable)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func phpSettingsPayload(setValues, extensionValues []string) (map[string]any, error) {
	payload := map[string]any{}
	if len(setValues) > 0 {
		assignments, err := cmdutil.ParseAssignments(setValues)
		if err != nil {
			return nil, err
		}
		settings := make(map[string]any, len(assignments))
		for key, value := range assignments {
			// The catalog lists integers (60) and strings (128M); send numbers as numbers.
			if number, err := strconv.Atoi(value); err == nil && strconv.Itoa(number) == value {
				settings[key] = number
			} else {
				settings[key] = value
			}
		}
		payload["settings"] = settings
	}
	if len(extensionValues) > 0 {
		assignments, err := cmdutil.ParseAssignments(extensionValues)
		if err != nil {
			return nil, err
		}
		extensions := make(map[string]bool, len(assignments))
		for name, value := range assignments {
			switch strings.ToLower(value) {
			case "on":
				extensions[name] = true
			case "off":
				extensions[name] = false
			default:
				return nil, fmt.Errorf("--extension %s must be %s=on or %s=off", name, name, name)
			}
		}
		payload["extensions"] = extensions
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("provide --set or --extension")
	}
	return payload, nil
}

func newCmdBasicAuth(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "basic-auth", Short: "Read and update HTTP basic auth", Args: cobra.NoArgs}
	command.AddCommand(link.Documented(newCmdBasicAuthGet(factory), false), link.Documented(newCmdBasicAuthUpdate(factory), true))
	return command
}

func newCmdBasicAuthGet(factory *app.Factory) *cobra.Command {
	return newEnvGet(factory, "Get the basic auth state (the password is never returned)", "/basic-auth", resource.WriteResource)
}

func newCmdBasicAuthUpdate(factory *app.Factory) *cobra.Command {
	var enabled bool
	var username string
	var passwordStdin bool
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <environment>",
		Short: "Enable, disable, or change basic auth",
		Long:  "Enable, disable, or change basic auth. The password is read as one line from stdin with --password-stdin and is never accepted as a flag. Pipe it in: printf '%s\\n' \"$PASSWORD\" | frbit environments basic-auth update ... --password-stdin. A password typed on a terminal is echoed. Omit it to keep the current password. --enabled and --username keep their current values when omitted.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if cmd.Flags().Changed("enabled") {
				payload["enabled"] = enabled
			}
			if cmd.Flags().Changed("username") {
				payload["username"] = username
			}
			if passwordStdin {
				line, err := bufio.NewReader(factory.IOStreams.In).ReadString('\n')
				if err != nil && err != io.EOF {
					return fmt.Errorf("read password from stdin: %w", err)
				}
				password := strings.TrimRight(line, "\r\n")
				if password == "" {
					return fmt.Errorf("--password-stdin read an empty password")
				}
				payload["password"] = password
			}
			if len(payload) == 0 {
				return fmt.Errorf("provide --enabled, --username, or --password-stdin")
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.WriteEnvironment(cmd, client, link.FirstArg(args), "")
			if err != nil {
				return err
			}
			path := "/v1/environments/" + url.PathEscape(env) + "/basic-auth"
			// The PUT replaces the settings and needs enabled and username, so missing ones keep their current value.
			if _, ok := payload["enabled"]; !ok || payload["username"] == nil {
				current, err := client.GetResource(cmd.Context(), path)
				if err != nil {
					return err
				}
				if _, ok := payload["enabled"]; !ok {
					payload["enabled"], _ = current.Resource["enabled"].(bool)
				}
				if payload["username"] == nil {
					name, _ := current.Resource["username"].(string)
					if name == "" {
						return fmt.Errorf("basic auth has no username yet; pass --username")
					}
					payload["username"] = name
				}
			}
			response, err := client.PutResource(cmd.Context(), path, payload)
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
		},
	}
	command.Flags().BoolVar(&enabled, "enabled", false, "Ask for credentials (--enabled=false turns it off)")
	command.Flags().StringVar(&username, "username", "", "Login name")
	command.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read the password from the first line of stdin")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdFirewallRules(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "firewall-rules", Short: "Open extra ports of an environment", Args: cobra.NoArgs}
	command.AddCommand(link.Documented(newCmdFirewallList(factory), false), link.Documented(newCmdFirewallCreate(factory), true), newCmdFirewallDelete(factory))
	return command
}

func formatPorts(rule api.Resource) string {
	start, _ := rule["portStart"].(float64)
	end, ok := rule["portEnd"].(float64)
	if !ok || end == start {
		return strconv.Itoa(int(start))
	}
	return fmt.Sprintf("%d-%d", int(start), int(end))
}

func writeFirewallRules(out io.Writer, rules []api.Resource) error {
	if len(rules) == 0 {
		_, err := fmt.Fprintln(out, "No firewall rules found.")
		return err
	}
	table := tabwriter.NewWriter(iostreams.Stripper(out), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "ID\tPORTS\tDESCRIPTION")
	for _, rule := range rules {
		_, _ = fmt.Fprintf(table, "%v\t%s\t%v\n", rule["publicId"], formatPorts(rule), rule["description"])
	}
	return table.Flush()
}

func newCmdFirewallList(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "list [environment]",
		Short: "List the firewall rules of an environment",
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
			response, err := client.ListResources(cmd.Context(), "/v1/environments/"+url.PathEscape(env)+"/firewall-rules", 0, nil, nil)
			if err != nil {
				return err
			}
			if printJSON {
				return resource.WriteRaw(cmd, response.Raw)
			}
			return writeFirewallRules(cmd.OutOrStdout(), response.Resources)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func parsePortRange(value string) (int, any, error) {
	startText, endText, isRange := strings.Cut(value, "-")
	start, err := strconv.Atoi(strings.TrimSpace(startText))
	if err != nil {
		return 0, nil, fmt.Errorf("--port must be N or N-M, got %q", value)
	}
	var end any
	if isRange {
		number, err := strconv.Atoi(strings.TrimSpace(endText))
		if err != nil {
			return 0, nil, fmt.Errorf("--port must be N or N-M, got %q", value)
		}
		end = number
	}
	return start, end, nil
}

func newCmdFirewallCreate(factory *app.Factory) *cobra.Command {
	var description, port string
	var printJSON bool
	command := &cobra.Command{
		Use:   "create <environment> --description D --port N[-M]",
		Short: "Open a port or port range",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if description == "" || port == "" {
				return fmt.Errorf("--description and --port are required")
			}
			start, end, err := parsePortRange(port)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.WriteEnvironment(cmd, client, link.FirstArg(args), "")
			if err != nil {
				return err
			}
			payload := map[string]any{"description": description, "portStart": start, "portEnd": end}
			response, err := client.CreateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(env)+"/firewall-rules", payload)
			if err != nil {
				return err
			}
			return resource.Write(cmd, response, printJSON)
		},
	}
	command.Flags().StringVar(&description, "description", "", "What the port is for")
	command.Flags().StringVar(&port, "port", "", "Port or range, e.g. 5000 or 8080-8090 (at most 1000 ports)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdFirewallDelete(factory *app.Factory) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use:   "delete <firewall-rule-id>",
		Short: "Delete a firewall rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := cmdutil.CheckDestructive(factory, "deletion", id, confirmation); err != nil {
				return err
			}
			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, cmd.ErrOrStderr(), "deletion", id, confirmation, "Deleting this rule closes its ports again.")
			if err != nil {
				return err
			}
			if !confirmed {
				return cmdutil.Aborted(cmd.ErrOrStderr())
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if err := client.DeleteResource(cmd.Context(), "/v1/firewall-rules/"+url.PathEscape(id)); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deleted firewall rule %s.\n", id)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm by repeating the firewall rule ID")
	return command
}

func newCmdComponents(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "components", Short: "Read and change the booked component sizes", Args: cobra.NoArgs}
	command.AddCommand(link.Documented(newCmdComponentsGet(factory), false), link.Documented(newCmdComponentsUpdate(factory), true))
	return command
}

func writeComponents(out io.Writer, value api.Resource) error {
	table := tabwriter.NewWriter(iostreams.Stripper(out), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "COMPONENT\tSIZE\tPRICE/MONTH")
	components, _ := value["components"].([]any)
	for _, raw := range components {
		component, _ := raw.(map[string]any)
		cents, _ := component["priceInCents"].(float64)
		_, _ = fmt.Fprintf(table, "%v\t%v\t%.2f %v\n", component["slug"], component["size"], cents/100, component["currency"])
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if pricing, ok := value["pricing"].(map[string]any); ok {
		cents, _ := pricing["monthlyPriceInCents"].(float64)
		_, err := fmt.Fprintf(out, "\nTotal: %.2f %v per month\n", cents/100, pricing["currency"])
		return err
	}
	return nil
}

func newCmdComponentsGet(factory *app.Factory) *cobra.Command {
	return newEnvGet(factory, "Get the booked component plans", "/components", writeComponents)
}

func newCmdComponentsUpdate(factory *app.Factory) *cobra.Command {
	var componentValues []string
	var yes, dryRun, printJSON bool
	command := &cobra.Command{
		Use:   "update <environment> --component SLUG=SIZE",
		Short: "Change component sizes (billed)",
		Long:  "Change component sizes (billed). Slugs: php, database, storage, traffic, key-value-store, backups, jobs. Sizes: 'frbit plans list'; optional components accept off. The API checks the change and prices it first: the monthly price after the change and the payment method are shown, and a billed change asks before it applies, also for a downsize. A trial app or a change to a free environment does not ask.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			components, err := cmdutil.ParseAssignments(componentValues)
			if err != nil {
				return err
			}
			if len(components) == 0 {
				return fmt.Errorf("provide at least one --component SLUG=SIZE")
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			env, err := link.WriteEnvironment(cmd, client, link.FirstArg(args), "")
			if err != nil {
				return err
			}
			path := "/v1/environments/" + url.PathEscape(env) + "/components"
			payload := make(map[string]any, len(components))
			for slug, size := range components {
				payload[slug] = size
			}
			if dryRun {
				return cmdutil.DryRunBilledChange(cmd, client, path, payload, printJSON)
			}
			confirmed, err := cmdutil.ConfirmBilledChange(cmd, factory, client, path, payload, yes)
			if err != nil {
				return err
			}
			if !confirmed {
				return cmdutil.Aborted(cmd.ErrOrStderr())
			}
			response, err := client.UpdateResource(cmd.Context(), path, payload)
			if err != nil {
				return err
			}
			if printJSON {
				return resource.WriteRaw(cmd, response.Raw)
			}
			return writeComponents(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().StringArrayVar(&componentValues, "component", nil, "Component as SLUG=SIZE (repeatable)")
	command.Flags().BoolVar(&yes, "yes", false, "Confirm billing without asking")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Show the monthly price after the change and change nothing (with --json, the API response)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}
