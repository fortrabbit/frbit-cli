package environments

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func writeRaw(cmd *cobra.Command, raw []byte) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
	return err
}

func newCmdPhpSettings(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "php-settings", Short: "Read and update php.ini settings and extensions", Args: cobra.NoArgs}
	command.AddCommand(newCmdPhpSettingsGet(factory), newCmdPhpSettingsUpdate(factory))
	return command
}

func newCmdPhpSettingsGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <public-id>",
		Short: "Get PHP settings and extensions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/php-settings")
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdPhpSettingsUpdate(factory *app.Factory) *cobra.Command {
	var setValues, extensionValues []string
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <public-id>",
		Short: "Change php.ini settings and enable or disable extensions",
		Long:  "Valid directives and values: 'frbit php-settings list'. Valid extensions: 'frbit php-extensions list'. Directives and extensions you do not name stay unchanged.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := phpSettingsPayload(setValues, extensionValues)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/php-settings", payload)
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
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
	command.AddCommand(newCmdBasicAuthGet(factory), newCmdBasicAuthUpdate(factory))
	return command
}

func newCmdBasicAuthGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <public-id>",
		Short: "Get the basic auth state (the password is never returned)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/basic-auth")
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdBasicAuthUpdate(factory *app.Factory) *cobra.Command {
	var enabled bool
	var username string
	var passwordStdin bool
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <public-id>",
		Short: "Enable, disable, or change basic auth",
		Long:  "The password is read as one line from stdin with --password-stdin and is never accepted as a flag. Pipe it in: printf '%s\\n' \"$PASSWORD\" | frbit environments basic-auth update ... --password-stdin. A password typed on a terminal is echoed. Omit it to keep the current password. --enabled and --username keep their current values when omitted.",
		Args:  cobra.ExactArgs(1),
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
			path := "/v1/environments/" + url.PathEscape(args[0]) + "/basic-auth"
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
			return writeResourceResponse(cmd, response, printJSON)
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
	command.AddCommand(newCmdFirewallList(factory), newCmdFirewallAdd(factory), newCmdFirewallDelete(factory))
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
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "ID\tPORTS\tDESCRIPTION")
	for _, rule := range rules {
		_, _ = fmt.Fprintf(table, "%v\t%s\t%v\n", rule["publicId"], formatPorts(rule), rule["description"])
	}
	return table.Flush()
}

func newCmdFirewallList(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "list <environment-id>",
		Short: "List the firewall rules of an environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.ListResources(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/firewall-rules", 0, nil, nil)
			if err != nil {
				return err
			}
			if printJSON {
				return writeRaw(cmd, response.Raw)
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
	last := start
	if end != nil {
		last = end.(int)
	}
	if start < 1 || last > 65535 {
		return 0, nil, fmt.Errorf("--port must be within 1-65535, got %q", value)
	}
	if last < start {
		return 0, nil, fmt.Errorf("--port range %q ends before it starts", value)
	}
	if last-start+1 > 1000 {
		return 0, nil, fmt.Errorf("--port range %q opens %d ports; at most 1000 are allowed", value, last-start+1)
	}
	return start, end, nil
}

func newCmdFirewallAdd(factory *app.Factory) *cobra.Command {
	var description, port string
	var printJSON bool
	command := &cobra.Command{
		Use:   "add <environment-id> --description D --port N[-M]",
		Short: "Open a port or port range",
		Args:  cobra.ExactArgs(1),
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
			payload := map[string]any{"description": description, "portStart": start, "portEnd": end}
			response, err := client.CreateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/firewall-rules", payload)
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
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
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return err
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
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm deletion by repeating the firewall rule ID")
	return command
}

func newCmdComponents(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "components", Short: "Read and change the booked component sizes", Args: cobra.NoArgs}
	command.AddCommand(newCmdComponentsGet(factory), newCmdComponentsUpdate(factory))
	return command
}

func writeComponents(out io.Writer, value api.Resource) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
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
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <public-id>",
		Short: "Get the booked component plans",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/components")
			if err != nil {
				return err
			}
			if printJSON {
				return writeRaw(cmd, response.Raw)
			}
			return writeComponents(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdComponentsUpdate(factory *app.Factory) *cobra.Command {
	var componentValues []string
	var region string
	var yes, printJSON bool
	command := &cobra.Command{
		Use:   "update <public-id> --component SLUG=SIZE",
		Short: "Change component sizes (billed)",
		Long:  "Slugs: php, database, storage, traffic, key-value-store, backups, jobs. Sizes: 'frbit plans list'; optional components accept off. Shows old -> new price per changed component and the monthly change, priced in the environment's region, and asks first, also for a downsize. Components already at the requested size are skipped; if nothing changes, nothing is sent.",
		Args:  cobra.ExactArgs(1),
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
			current, err := client.GetResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/components")
			if err != nil {
				return err
			}
			components, err = changedComponents(current.Resource, components)
			if err != nil {
				return err
			}
			if len(components) == 0 {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No component plan changes.")
				return err
			}
			if region == "" {
				if region, err = environmentRegion(cmd, client, args[0]); err != nil {
					return err
				}
			}
			plans, err := client.ListResources(cmd.Context(), "/v1/catalog/component-plans", 0, nil, map[string]string{"currency": currentCurrency(current.Resource), "region": region})
			if err != nil {
				return err
			}
			summary, err := componentChangeSummary(current.Resource, plans.Resources, components)
			if err != nil {
				return err
			}
			confirmed, err := cmdutil.AskBilledWith(cmd, factory, summary, "Change component plans? [y/N] ", yes)
			if err != nil {
				return err
			}
			if !confirmed {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return err
			}
			payload := make(map[string]any, len(components))
			for slug, size := range components {
				payload[slug] = size
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/environments/"+url.PathEscape(args[0])+"/components", payload)
			if err != nil {
				return err
			}
			if printJSON {
				return writeRaw(cmd, response.Raw)
			}
			return writeComponents(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().StringArrayVar(&componentValues, "component", nil, "Component as SLUG=SIZE (repeatable)")
	command.Flags().StringVar(&region, "region", "", "Region identifier to price the plans in (default: the region of the environment's app)")
	command.Flags().BoolVar(&yes, "yes", false, "Confirm billing without asking")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

// environmentRegion finds the app that lists the environment and returns its region.
func environmentRegion(cmd *cobra.Command, client *api.Client, environmentID string) (string, error) {
	apps, err := client.ListAll(cmd.Context(), "/v1/apps", nil, 0)
	if err != nil {
		return "", err
	}
	for _, appResource := range apps.Resources {
		environments, _ := appResource["environments"].([]any)
		for _, raw := range environments {
			id, _ := raw.(string)
			if id == environmentID || strings.HasSuffix(id, "/"+environmentID) {
				if region, _ := appResource["region"].(string); region != "" {
					return region, nil
				}
			}
		}
	}
	return "", fmt.Errorf("could not resolve the environment's region; pass --region")
}

// changedComponents keeps only the requested components whose size differs from the booked one.
func changedComponents(current api.Resource, components map[string]string) (map[string]string, error) {
	booked := map[string]string{}
	items, _ := current["components"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		slug, _ := item["slug"].(string)
		size, _ := item["size"].(string)
		booked[slug] = size
	}
	changed := map[string]string{}
	for slug, size := range components {
		old, ok := booked[slug]
		if !ok {
			return nil, fmt.Errorf("environment has no component %s", slug)
		}
		if old != size {
			changed[slug] = size
		}
	}
	return changed, nil
}

func currentCurrency(current api.Resource) string {
	items, _ := current["components"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if currency, _ := item["currency"].(string); currency != "" {
			return currency
		}
	}
	return "EUR"
}

// componentChangeSummary shows old and new price per named component and the signed monthly delta; an unknown slug or size is an error.
func componentChangeSummary(current api.Resource, plans []api.Resource, components map[string]string) (string, error) {
	type booked struct {
		size  string
		cents float64
	}
	bookedPlans := map[string]booked{}
	items, _ := current["components"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		slug, _ := item["slug"].(string)
		size, _ := item["size"].(string)
		cents, _ := item["priceInCents"].(float64)
		bookedPlans[slug] = booked{size, cents}
	}
	slugs := make([]string, 0, len(components))
	for slug := range components {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	currency := currentCurrency(current)
	var lines []string
	var delta float64
	for _, slug := range slugs {
		size := components[slug]
		old, isBooked := bookedPlans[slug]
		if !isBooked {
			return "", fmt.Errorf("environment has no component %s", slug)
		}
		found := false
		for _, plan := range plans {
			if plan["slug"] != slug {
				continue
			}
			sizes, _ := plan["sizes"].([]any)
			for _, raw := range sizes {
				entry, _ := raw.(map[string]any)
				if entry["size"] != size {
					continue
				}
				found = true
				cents, _ := entry["priceInCents"].(float64)
				if entryCurrency, _ := entry["currency"].(string); entryCurrency != "" && entryCurrency != currency {
					return "", fmt.Errorf("catalog prices %s in %s but the environment is billed in %s", slug, entryCurrency, currency)
				}
				delta += cents - old.cents
				lines = append(lines, fmt.Sprintf("%s: %s (%.2f %s) -> %s (%.2f %s)", slug, old.size, old.cents/100, currency, size, cents/100, currency))
			}
		}
		if !found {
			return "", fmt.Errorf("component %s has no size %q in the catalog; see 'frbit plans list'", slug, size)
		}
	}
	return fmt.Sprintf("%s\nMonthly change: %+.2f %s", strings.Join(lines, "\n"), delta/100, currency), nil
}
