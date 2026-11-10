// Package catalog exposes the read-only catalog and git lookups of the public API.
package catalog

import (
	"fmt"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

// NewCommands returns the catalog groups and the git group.
func NewCommands(factory *app.Factory) []*cobra.Command {
	var commands []*cobra.Command
	for _, spec := range specs() {
		commands = append(commands, resource.NewCmdGroup(factory, spec))
	}
	return append(commands, newCmdGit(factory))
}

func specs() []resource.Spec {
	return []resource.Spec{
		{
			Use: "regions", Singular: "region", Path: "/catalog/regions", Short: "List the regions an app can be created in", NoGet: true,
			Fields: []resource.Field{{Header: "IDENTIFIER", Key: "identifier"}, {Header: "NAME", Key: "name"}, {Header: "LOCATION", Key: "location"}, {Header: "RECOMMENDED", Key: "recommended"}},
		},
		{
			Use: "plans", Singular: "plan", Path: "/catalog/component-plans", Short: "List component plans, sizes and prices", NoGet: true,
			QueryFilters:  []resource.QueryFilter{{Name: "region", Usage: "Region identifier, e.g. eu-w1a"}, {Name: "currency", Usage: "Price currency: EUR or USD"}},
			ClientFilters: []resource.ClientFilter{{Name: "component", Usage: "Only the component with this slug, e.g. php", Key: "slug"}},
			Fields:        []resource.Field{{Header: "COMPONENT", Key: "slug"}, {Header: "OPTIONAL", Key: "optional"}, {Header: "AUTOSCALES", Key: "autoscales"}, {Header: "SIZES", Key: "sizes"}},
		},
		{
			Use: "presets", Singular: "preset", Path: "/catalog/software-presets", Short: "List software presets and their versions", NoGet: true,
			Fields: []resource.Field{{Header: "SLUG", Key: "slug"}, {Header: "NAME", Key: "name"}, {Header: "VERSIONS", Key: "versions"}, {Header: "DEFAULT", Key: "defaultVersion"}},
		},
		{
			Use: "software", Singular: "software", Path: "/catalog/software", Short: "List software with its versions", NoGet: true,
			Fields: []resource.Field{{Header: "SLUG", Key: "slug"}, {Header: "NAME", Key: "name"}, {Header: "TYPE", Key: "type"}, {Header: "VERSIONS", Key: "versions"}, {Header: "DEFAULT", Key: "defaultVersion"}},
		},
		{
			Use: "php-versions", Singular: "PHP version", Path: "/catalog/php-versions", Short: "List the available PHP versions", NoGet: true,
			Fields: []resource.Field{{Header: "VERSION", Key: "version"}, {Header: "DEFAULT", Key: "default"}, {Header: "EOL", Key: "eol"}, {Header: "SURCHARGE", Key: "surcharge"}},
		},
		{
			Use: "php-extensions", Singular: "PHP extension", Path: "/catalog/php-extensions", Short: "List the available PHP extensions", NoGet: true,
			Fields: []resource.Field{{Header: "NAME", Key: "name"}, {Header: "DEFAULT ENABLED", Key: "defaultEnabled"}},
		},
		{
			Use: "node-versions", Singular: "Node.js version", Path: "/catalog/node-versions", Short: "List the available Node.js versions", NoGet: true,
			Fields: []resource.Field{{Header: "VERSION", Key: "version"}, {Header: "DEFAULT", Key: "default"}, {Header: "EOL", Key: "eol"}},
		},
	}
}

func newCmdGit(factory *app.Factory) *cobra.Command {
	group := &cobra.Command{Use: "git", Short: "List connected Git repositories and their branches", Args: cobra.NoArgs}

	repositories := resource.NewCmdList(factory, resource.Spec{
		Use: "repositories", Singular: "repository", Path: "/git-repositories",
		Fields: []resource.Field{{Header: "REPOSITORY", Key: "fullName"}, {Header: "DEFAULT BRANCH", Key: "defaultBranch"}, {Header: "PRIVATE", Key: "private"}, {Header: "APP", Key: "connectedAppPublicId"}},
	})
	repositories.Use = "repositories"
	repositories.Short = "List repositories of the connected Git account"

	group.AddCommand(repositories, newCmdBranches(factory))
	return group
}

func newCmdBranches(factory *app.Factory) *cobra.Command {
	spec := resource.Spec{
		Use: "branches", Singular: "branch",
		Fields: []resource.Field{{Header: "BRANCH", Key: "name"}, {Header: "PROTECTED", Key: "protected"}},
	}
	var printJSON bool
	command := &cobra.Command{
		Use:   "branches <owner/repo>",
		Short: "List the branches of a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, repo, ok := strings.Cut(args[0], "/")
			if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") || owner == "." || owner == ".." || repo == "." || repo == ".." {
				return fmt.Errorf("repository must be owner/repo, got %q", args[0])
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.ListResources(cmd.Context(), "/v1/git-repositories/"+owner+"/"+repo+"/branches", 0, nil, nil)
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return resource.WriteResources(cmd.OutOrStdout(), response.Resources, response.TotalItems, spec)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}
