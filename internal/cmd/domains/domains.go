// Package domains builds the domains command group: the generic list, get and delete plus create and DNS checks.
package domains

import (
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdDomains(factory *app.Factory) *cobra.Command {
	command := resource.NewCmdGroup(factory, resource.Spec{
		Use: "domains", Singular: "domain", Path: "/domains", Short: "Create, inspect, and check domains", SupportsPage: true, SupportsFilter: true,
		Delete: &resource.DeleteSpec{Warning: "Deleting this domain disconnects it from its environment. The domain will no longer serve the environment."},
		Fields: []resource.Field{{Header: "ID", Key: "publicId"}, {Header: "NAME", Key: "name"}, {Header: "TYPE", Key: "type"}, {Header: "ENVIRONMENT", Key: "environment"}, {Header: "MAIN", Key: "isMain"}, {Header: "UPDATED", Key: "updatedAt"}},
	})
	command.AddCommand(newCmdCreate(factory), newCmdDNS(factory))
	return command
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var main, cloud, printJSON bool
	command := &cobra.Command{
		Use:   "create <environment> <name>",
		Short: "Add a domain to an environment",
		Long: "Add a domain to an environment and print the DNS record to set at your DNS provider.\n\n" +
			"Without --cloud, name is a hostname you own, such as www.example.com.\n" +
			"With --cloud, name is a label such as acme-shop, not a full hostname; the API appends the cloud domain root.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			environment, name := args[0], args[1]
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			domainType := "external"
			if cloud {
				domainType = "cloud"
				availability, err := client.GetResourceWithQuery(cmd.Context(), "/v1/domains/cloud/availability", url.Values{"name": {name}})
				if err != nil {
					return err
				}
				if available, _ := availability.Resource["available"].(bool); !available {
					reason, _ := availability.Resource["reason"].(string)
					if reason == "" {
						reason = "unavailable"
					}
					return fmt.Errorf("cloud domain %q is not available: %s", name, reason)
				}
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/domains", map[string]any{
				"environment": environment, "name": name, "type": domainType, "isMain": main,
			})
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			if err := resource.WriteResource(cmd.OutOrStdout(), response.Resource); err != nil {
				return err
			}
			if cloud {
				return nil // cloud domains need no DNS record
			}
			publicID, _ := response.Resource["publicId"].(string)
			if publicID == "" {
				return nil
			}
			dns, err := client.GetResource(cmd.Context(), "/v1/domains/"+url.PathEscape(publicID)+"/dns")
			if err != nil {
				return fmt.Errorf("domain %s was created, but its DNS records could not be read: %w", publicID, err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			return writeDNS(cmd.OutOrStdout(), dns.Resource)
		},
	}
	command.Flags().BoolVar(&main, "main", false, "Make the domain the main domain of the environment")
	command.Flags().BoolVar(&cloud, "cloud", false, "Create a cloud domain; name is a label such as acme-shop")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdDNS(factory *app.Factory) *cobra.Command {
	var refresh, printJSON bool
	command := &cobra.Command{
		Use:   "dns <public-id>",
		Short: "Show the DNS record a domain needs and what DNS returns now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			path := "/v1/domains/" + url.PathEscape(args[0]) + "/dns"
			if refresh {
				if _, err := client.PostAction(cmd.Context(), path+"/refresh"); err != nil {
					return err
				}
			}
			response, err := client.GetResource(cmd.Context(), path)
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return writeDNS(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().BoolVar(&refresh, "refresh", false, "Check DNS again before printing")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func writeDNS(output io.Writer, dns api.Resource) error {
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "RECORD\tTYPE\tNAME\tVALUE")
	for _, row := range []struct{ label, key string }{{"expected", "expected"}, {"actual", "actual"}} {
		record, _ := dns[row.key].(map[string]any)
		if record == nil {
			_, _ = fmt.Fprintf(table, "%s\t-\t-\t-\n", row.label)
			continue
		}
		_, _ = fmt.Fprintf(table, "%s\t%v\t%v\t%v\n", row.label, record["type"], record["host"], record["value"])
	}
	if err := table.Flush(); err != nil {
		return err
	}
	status, _ := dns["status"].(string)
	_, err := fmt.Fprintf(output, "\nVerification: %s\n", status)
	return err
}
