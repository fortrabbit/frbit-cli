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

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

type Field struct {
	Header string
	Key    string
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
	Logs           bool
	Delete         *DeleteSpec
}

func NewCmdGroup(factory *app.Factory, spec Spec) *cobra.Command {
	command := &cobra.Command{
		Use:   spec.Use,
		Short: spec.Short,
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newCmdList(factory, spec), newCmdGet(factory, spec))
	if spec.Logs {
		command.AddCommand(newCmdLogs(factory, spec))
	}
	if spec.Delete != nil {
		command.AddCommand(newCmdDelete(factory, spec))
	}
	return command
}

func newCmdList(factory *app.Factory, spec Spec) *cobra.Command {
	var page int
	var publicIDs []string
	var printJSON bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List " + spec.Use + " available to the authenticated person",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if spec.SupportsPage && page < 1 {
				return fmt.Errorf("--page must be at least 1")
			}
			client, err := clientFor(cmd, factory)
			if err != nil {
				return err
			}
			listResources := client.ListResourcesWithTotal
			if printJSON {
				listResources = client.ListResources
			}
			response, err := listResources(cmd.Context(), "/v1"+spec.Path, page, publicIDs)
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return writeResources(cmd.OutOrStdout(), response.Resources, response.TotalItems, spec)
		},
	}
	if spec.SupportsPage {
		command.Flags().IntVar(&page, "page", 1, "Page number")
	}
	if spec.SupportsFilter {
		command.Flags().StringSliceVar(&publicIDs, "id", nil, "Filter by public ID (repeatable)")
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
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
			if confirmation != "" && confirmation != publicID {
				return fmt.Errorf("--confirm must exactly match %s", publicID)
			}
			if confirmation == "" && !factory.IOStreams.IsTTY {
				return fmt.Errorf("deletion requires interactive confirmation; pass --confirm %s for intentional non-interactive use", publicID)
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

			if confirmation == "" {
				confirmed, err := cmdutil.ConfirmExact(
					factory.IOStreams.In,
					cmd.OutOrStdout(),
					fmt.Sprintf("Type %s to confirm: ", publicID),
					publicID,
				)
				if err != nil {
					return err
				}
				if !confirmed {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return err
				}
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
		if _, err := fmt.Fprintf(table, "%s\t%s\n", key, resourceValue(resource[key])); err != nil {
			return err
		}
	}
	return table.Flush()
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
		if _, err := fmt.Fprintf(output, "%s\t%s\n", resourceValue(entry["time"]), resourceValue(entry["log"])); err != nil {
			return err
		}
	}
	return nil
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
