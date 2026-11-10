// Package resource supplies the common read-only command shape used by the
// public API's resource collections.
package resource

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type Field struct {
	Header string
	Key    string
}

// QueryFilter is a list flag sent as a query parameter of the same name.
type QueryFilter struct {
	Name  string
	Usage string
}

// ClientFilter is a list flag applied after the fetch: it keeps resources whose Key field equals the flag value.
type ClientFilter struct {
	Name  string
	Usage string
	Key   string
}

type DeleteSpec struct {
	Warning string
}

type Spec struct {
	Use            string
	Singular       string
	Path           string
	Short          string
	Fields         []Field
	SupportsPage   bool
	SupportsFilter bool
	QueryFilters   []QueryFilter
	ClientFilters  []ClientFilter
	NoGet          bool
	Logs           bool
	Delete         *DeleteSpec
}

func NewCmdGroup(factory *app.Factory, spec Spec) *cobra.Command {
	command := &cobra.Command{
		Use:   spec.Use,
		Short: spec.Short,
		Args:  cobra.NoArgs,
	}
	command.AddCommand(NewCmdList(factory, spec))
	if !spec.NoGet {
		command.AddCommand(newCmdGet(factory, spec))
	}
	if spec.Logs {
		command.AddCommand(newCmdLogs(factory, spec))
	}
	if spec.Delete != nil {
		command.AddCommand(newCmdDelete(factory, spec))
	}
	return command
}

// NewCmdList builds the list command of spec on its own, for groups that name it differently.
func NewCmdList(factory *app.Factory, spec Spec) *cobra.Command {
	var page int
	var limit int
	var all bool
	var publicIDs []string
	var printJSON bool
	filterValues := make(map[string]*string, len(spec.QueryFilters))
	clientValues := make(map[string]*string, len(spec.ClientFilters))
	command := &cobra.Command{
		Use:   "list",
		Short: "List " + spec.Use + " available to the authenticated person",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if spec.SupportsPage && page < 1 {
				return fmt.Errorf("--page must be at least 1")
			}
			if all && cmd.Flags().Changed("page") {
				return fmt.Errorf("--all cannot be combined with --page")
			}
			if all && cmd.Flags().Changed("limit") {
				return fmt.Errorf("--all cannot be combined with --limit")
			}
			if cmd.Flags().Changed("limit") && limit < 1 {
				return fmt.Errorf("--limit must be at least 1")
			}
			filters := make(map[string]string, len(filterValues))
			for name, value := range filterValues {
				if cmd.Flags().Changed(name) && *value == "" {
					return fmt.Errorf("--%s must not be empty", name)
				}
				filters[name] = *value
			}
			clientFiltered := false
			for _, filter := range spec.ClientFilters {
				if cmd.Flags().Changed(filter.Name) && *clientValues[filter.Name] == "" {
					return fmt.Errorf("--%s must not be empty", filter.Name)
				}
				if *clientValues[filter.Name] != "" {
					clientFiltered = true
				}
			}
			client, err := clientFor(cmd, factory)
			if err != nil {
				return err
			}
			var response api.ResourcesResponse
			if all || limit > 0 {
				fetchLimit := limit
				if clientFiltered {
					fetchLimit = 0 // the limit applies to the filtered result
				}
				startPage := 0
				if cmd.Flags().Changed("page") {
					startPage = page
				}
				response, err = client.ListAll(cmd.Context(), "/v1"+spec.Path, api.ListQuery(startPage, publicIDs, filters), fetchLimit)
			} else {
				listResources := client.ListResourcesWithTotal
				if printJSON {
					listResources = client.ListResources
				}
				response, err = listResources(cmd.Context(), "/v1"+spec.Path, page, publicIDs, filters)
			}
			if err != nil {
				return err
			}
			filtered := clientFiltered
			for _, filter := range spec.ClientFilters {
				if value := *clientValues[filter.Name]; value != "" {
					response = keepMatching(response, filter.Key, value)
				}
			}
			if clientFiltered && limit > 0 && len(response.Resources) > limit {
				response.Resources = response.Resources[:limit]
				response.TotalItems = limit
			}
			if printJSON {
				raw := response.Raw
				if filtered {
					if raw, err = json.Marshal(response.Resources); err != nil {
						return err
					}
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
				return err
			}
			return writeResources(cmd.OutOrStdout(), response.Resources, response.TotalItems, spec)
		},
	}
	if spec.SupportsPage {
		command.Flags().IntVar(&page, "page", 1, "Page number")
	}
	command.Flags().IntVar(&limit, "limit", 0, "Return at most N "+resourceLabel(spec.Use)+", following further pages as needed")
	command.Flags().BoolVar(&all, "all", false, "Return every page")
	if spec.SupportsFilter {
		command.Flags().StringSliceVar(&publicIDs, "id", nil, "Filter by public ID (repeatable)")
	}
	for _, filter := range spec.QueryFilters {
		filterValues[filter.Name] = command.Flags().String(filter.Name, "", filter.Usage)
	}
	for _, filter := range spec.ClientFilters {
		clientValues[filter.Name] = command.Flags().String(filter.Name, "", filter.Usage)
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func keepMatching(response api.ResourcesResponse, key string, value string) api.ResourcesResponse {
	kept := make([]api.Resource, 0, len(response.Resources))
	for _, item := range response.Resources {
		if resourceValue(item[key]) == value {
			kept = append(kept, item)
		}
	}
	response.Resources = kept
	response.TotalItems = len(kept)
	return response
}

func newCmdGet(factory *app.Factory, spec Spec) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "get <public-id>",
		Short: "Get " + articleFor(spec.Singular) + " " + spec.Singular,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientFor(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1"+spec.Path+"/"+args[0])
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return writeResource(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdLogs(factory *app.Factory, spec Spec) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "logs <public-id>",
		Short: "Get deployment logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientFor(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1"+spec.Path+"/"+args[0]+"/logs")
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return writeLogs(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdDelete(factory *app.Factory, spec Spec) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use:   "delete <public-id>",
		Short: "Delete " + articleFor(spec.Singular) + " " + spec.Singular,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			publicID := args[0]
			if err := cmdutil.CheckDestructive(factory, "deletion", publicID, confirmation); err != nil {
				return err
			}

			client, err := clientFor(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1"+spec.Path+"/"+publicID)
			if err != nil {
				return err
			}
			if err := writeDeleteWarning(cmd.OutOrStdout(), spec, response.Resource, publicID); err != nil {
				return err
			}

			confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, cmd.OutOrStdout(), "deletion", publicID, confirmation, "")
			if err != nil {
				return err
			}
			if !confirmed {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return err
			}

			if err := client.DeleteResource(cmd.Context(), "/v1"+spec.Path+"/"+publicID); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s %s.\n", spec.Singular, publicID)
			return err
		},
	}
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm deletion by repeating the public ID")
	return command
}

func writeDeleteWarning(output io.Writer, spec Spec, resource api.Resource, publicID string) error {
	label := spec.Singular
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	name, _ := resource["name"].(string)
	if name == "" {
		if _, err := fmt.Fprintf(output, "%s: %s\n\n", label, publicID); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(output, "%s: %s (%s)\n\n", label, name, publicID); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "WARNING: %s This cannot be undone.\n\n", spec.Delete.Warning)
	return err
}

func articleFor(singular string) string {
	if singular != "" && strings.ContainsAny(strings.ToLower(singular[:1]), "aeiou") {
		return "an"
	}
	return "a"
}

func clientFor(command *cobra.Command, factory *app.Factory) (*api.Client, error) {
	return cmdutil.APIClient(command, factory)
}

// WriteResources prints resources as the table of spec followed by the total line.
func WriteResources(output io.Writer, resources []api.Resource, totalItems int, spec Spec) error {
	return writeResources(output, resources, totalItems, spec)
}

func WriteResource(output io.Writer, value api.Resource) error {
	return writeResource(output, value)
}

func writeResources(output io.Writer, resources []api.Resource, totalItems int, spec Spec) error {
	if len(resources) == 0 {
		if _, err := fmt.Fprintln(output, "No "+resourceLabel(spec.Use)+" found."); err != nil {
			return err
		}
	} else {
		table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		for index, field := range spec.Fields {
			if index > 0 {
				_, _ = fmt.Fprint(table, "\t")
			}
			if _, err := fmt.Fprint(table, field.Header); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(table); err != nil {
			return err
		}
		for _, resource := range resources {
			for index, field := range spec.Fields {
				if index > 0 {
					_, _ = fmt.Fprint(table, "\t")
				}
				if _, err := fmt.Fprint(table, resourceValue(resource[field.Key])); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(table); err != nil {
				return err
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}

	label := resourceLabel(spec.Use)
	if totalItems == 1 {
		label = spec.Singular
	}
	_, err := fmt.Fprintf(output, "\nTotal: %d %s\n", totalItems, label)
	return err
}

func resourceLabel(value string) string {
	return strings.ReplaceAll(value, "-", " ")
}

func writeResource(output io.Writer, resource api.Resource) error {
	keys := make([]string, 0, len(resource))
	for key := range resource {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "FIELD\tVALUE"); err != nil {
		return err
	}
	for _, key := range keys {
		if nested, ok := resource[key].(map[string]any); ok && len(nested) > 0 {
			if err := writeNested(table, key, nested); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\n", key, resourceValue(resource[key])); err != nil {
			return err
		}
	}
	return table.Flush()
}

// writeNested prints an object as one dotted row per field, e.g. deployment.trigger.
func writeNested(table io.Writer, prefix string, nested map[string]any) error {
	keys := make([]string, 0, len(nested))
	for key := range nested {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(table, "%s.%s\t%s\n", prefix, key, resourceValue(nested[key])); err != nil {
			return err
		}
	}
	return nil
}

func writeLogs(output io.Writer, resource api.Resource) error {
	logs, ok := resource["logs"].([]any)
	if !ok || len(logs) == 0 {
		_, err := fmt.Fprintln(output, "No deployment logs found.")
		return err
	}
	for _, value := range logs {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(output, "%s\t%s\n", resourceValue(entry["time"]), stripControlCharacters(resourceValue(entry["log"]))); err != nil {
			return err
		}
	}
	return nil
}

func stripControlCharacters(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}

func resourceValue(value any) string {
	if value == nil {
		return ""
	}
	switch value := value.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, resourceValue(item))
		}
		return strings.Join(parts, ",")
	default:
		encoded, err := json.Marshal(value)
		if err == nil {
			return string(encoded)
		}
		return fmt.Sprint(value)
	}
}
