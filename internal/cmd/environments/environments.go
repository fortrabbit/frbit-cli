package environments

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdEnvironments(factory *app.Factory) *cobra.Command {
	command := resource.NewCmdGroup(factory, resource.Spec{
		Use:      "environments",
		Singular: "environment",
		Path:     "/environments",
		Short:    "Create, configure, and inspect environments",
		Delete: &resource.DeleteSpec{
			Warning: "Deleting this environment permanently removes its infrastructure. All files and database contents will be erased, and its domains will no longer serve the app.",
		},
		SupportsPage:   true,
		SupportsFilter: true,
		Fields: []resource.Field{
			{Header: "ID", Key: "publicId"},
			{Header: "NAME", Key: "name"},
			{Header: "SOFTWARE", Key: "softwareVersion"},
			{Header: "UPDATED", Key: "updatedAt"},
		},
	})
	command.AddCommand(
		newCmdCreate(factory),
		newCmdUpdate(factory),
		newCmdVariables(factory),
		newCmdPhpSettings(factory),
		newCmdBasicAuth(factory),
		newCmdFirewallRules(factory),
		newCmdComponents(factory),
		newCmdRestart(factory),
		newCmdDeploy(factory),
		newCmdMetrics(factory),
		newCmdLogs(factory),
	)
	return command
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var file string
	var appID string
	var name string
	var branch string
	var directory string
	var rootPath string
	var buildCommands []string
	var postDeployCommands []string
	var deploy bool
	var softwareVersion string
	var phpVersion string
	var noCloudDomain bool
	var componentValues []string
	var autoscaling bool
	var sourceEnvironment string
	var environmentVariableValues []string
	var printJSON bool
	var yes bool

	command := &cobra.Command{
		Use:   "create",
		Short: "Create an environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := environmentCreatePayload(cmd, factory, file, appID, name, branch, directory, rootPath, buildCommands, postDeployCommands, deploy, softwareVersion, phpVersion, noCloudDomain, componentValues, autoscaling, sourceEnvironment, environmentVariableValues)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			confirmed, err := cmdutil.ConfirmBilled(cmd, factory, client, "/v1/environments", payload, yes)
			if err != nil {
				return err
			}
			if !confirmed {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return err
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/environments", payload)
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "Read the complete request body from a JSON file ('-' for stdin)")
	command.Flags().StringVar(&appID, "app", "", "Owning app public ID")
	command.Flags().StringVar(&name, "name", "", "Environment name")
	command.Flags().StringVar(&branch, "branch", "", "Git branch")
	command.Flags().StringVar(&directory, "directory", "", "Git source directory")
	command.Flags().StringVar(&rootPath, "root-path", "", "Folder of the repository the web server serves, e.g. public")
	command.Flags().StringArrayVar(&buildCommands, "build-command", nil, "Build command (repeatable)")
	command.Flags().StringArrayVar(&postDeployCommands, "post-deploy-command", nil, "Post-deploy command (repeatable)")
	command.Flags().BoolVar(&deploy, "deploy", false, "Start the first deployment after creation")
	command.Flags().StringVar(&softwareVersion, "software-version", "", "Major software version")
	command.Flags().StringVar(&phpVersion, "php-version", "", "PHP runtime version")
	command.Flags().BoolVar(&noCloudDomain, "no-cloud-domain", false, "Create the environment without a cloud domain")
	command.Flags().StringArrayVar(&componentValues, "component", nil, "Component plan as SLUG=SIZE (repeatable)")
	command.Flags().BoolVar(&autoscaling, "autoscaling", true, "Enable autoscaling")
	command.Flags().StringVar(&sourceEnvironment, "source-environment", "", "Environment public ID whose component plans should be cloned")
	command.Flags().StringArrayVar(&environmentVariableValues, "env", nil, "Environment variable as NAME=VALUE (repeatable)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	command.Flags().BoolVar(&yes, "yes", false, "Confirm billing without asking")
	return command
}

func environmentCreatePayload(command *cobra.Command, factory *app.Factory, file string, appID string, name string, branch string, directory string, rootPath string, buildCommands []string, postDeployCommands []string, deploy bool, softwareVersion string, phpVersion string, noCloudDomain bool, componentValues []string, autoscaling bool, sourceEnvironment string, environmentVariableValues []string) (any, error) {
	inputFlags := []string{"app", "name", "branch", "directory", "root-path", "build-command", "post-deploy-command", "deploy", "software-version", "php-version", "no-cloud-domain", "component", "autoscaling", "source-environment", "env"}
	if file != "" {
		if cmdutil.AnyFlagChanged(command, inputFlags...) {
			return nil, fmt.Errorf("--file cannot be combined with request field flags")
		}
		return cmdutil.ReadJSONObject(file, factory.IOStreams.In)
	}
	if appID == "" || name == "" {
		return nil, fmt.Errorf("--app and --name are required unless --file is used")
	}
	components, err := cmdutil.ParseAssignments(componentValues)
	if err != nil {
		return nil, err
	}
	environmentVariables, err := cmdutil.ParseAssignments(environmentVariableValues)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"appId":                appID,
		"name":                 name,
		"deployment":           nil,
		"softwareVersion":      nil,
		"components":           components,
		"autoscaling":          autoscaling,
		"phpVersion":           nil,
		"sourceEnvironment":    nil,
		"environmentVariables": environmentVariables,
	}
	if softwareVersion != "" {
		payload["softwareVersion"] = softwareVersion
	}
	if phpVersion != "" {
		payload["phpVersion"] = phpVersion
	}
	if noCloudDomain {
		payload["cloudDomain"] = nil
	}
	if sourceEnvironment != "" {
		payload["sourceEnvironment"] = sourceEnvironment
	}
	if command.Flags().Changed("root-path") {
		payload["rootPath"] = rootPath
	}

	deploymentChanged := cmdutil.AnyFlagChanged(command, "branch", "directory", "build-command", "post-deploy-command", "deploy")
	if deploymentChanged {
		if branch == "" {
			return nil, fmt.Errorf("--branch is required when configuring deployment")
		}
		git := map[string]any{"branch": branch}
		if directory != "" {
			git["directory"] = directory
		}
		deployment := map[string]any{"git": git}
		if command.Flags().Changed("build-command") {
			deployment["buildCommands"] = buildCommands
		}
		if command.Flags().Changed("post-deploy-command") {
			deployment["postDeployCommands"] = postDeployCommands
		}
		if deploy {
			deployment["startFirstDeployment"] = true
		}
		payload["deployment"] = deployment
	}
	return payload, nil
}

type updateOptions struct {
	file                    string
	name                    string
	branch                  string
	directory               string
	rootPath                string
	buildCommands           []string
	postDeployCommands      []string
	clearBuildCommands      bool
	clearPostDeployCommands bool
	strategy                string
	excludePatterns         []string
	clearExcludePatterns    bool
	replacePatterns         []string
	clearReplacePatterns    bool
	cachedDirectories       []string
	clearCachedDirectories  bool
	trigger                 string
	permission              string
	nodeVersion             string
	softwareVersion         string
	phpVersion              string
	deployHook              bool
}

func newCmdUpdate(factory *app.Factory) *cobra.Command {
	var opts updateOptions
	var printJSON bool

	command := &cobra.Command{
		Use:   "update <public-id>",
		Short: "Update an environment's name, root path, or deployment configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := environmentUpdatePayload(cmd, factory, opts)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/environments/"+args[0], payload)
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
		},
	}
	flags := command.Flags()
	flags.StringVarP(&opts.file, "file", "f", "", "Read the complete request body from a JSON file ('-' for stdin)")
	flags.StringVar(&opts.name, "name", "", "New environment name")
	flags.StringVar(&opts.branch, "branch", "", "New Git branch")
	flags.StringVar(&opts.directory, "directory", "", "New Git source directory")
	flags.StringVar(&opts.rootPath, "root-path", "", "Folder of the repository the web server serves, e.g. public (empty resets it)")
	flags.StringArrayVar(&opts.buildCommands, "build-command", nil, "Replacement build command (repeatable)")
	flags.StringArrayVar(&opts.postDeployCommands, "post-deploy-command", nil, "Replacement post-deploy command (repeatable)")
	flags.BoolVar(&opts.clearBuildCommands, "clear-build-commands", false, "Remove all build commands")
	flags.BoolVar(&opts.clearPostDeployCommands, "clear-post-deploy-commands", false, "Remove all post-deploy commands")
	flags.StringVar(&opts.strategy, "strategy", "", "Deployment strategy: merge or replace")
	flags.StringArrayVar(&opts.excludePatterns, "exclude-pattern", nil, "Path pattern excluded from deployments (repeatable)")
	flags.BoolVar(&opts.clearExcludePatterns, "clear-exclude-patterns", false, "Remove all exclude patterns")
	flags.StringArrayVar(&opts.replacePatterns, "replace-pattern", nil, "Path pattern replaced on deployment, merge strategy only (repeatable)")
	flags.BoolVar(&opts.clearReplacePatterns, "clear-replace-patterns", false, "Remove all replace patterns")
	flags.StringArrayVar(&opts.cachedDirectories, "cached-directory", nil, "Directory cached between deployments (repeatable)")
	flags.BoolVar(&opts.clearCachedDirectories, "clear-cached-directories", false, "Remove all cached directories")
	flags.StringVar(&opts.trigger, "trigger", "", "What starts a deployment: push, manual, or hook")
	flags.StringVar(&opts.permission, "permission", "", "Who may deploy: connectedDevelopers or repositoryAccess")
	flags.StringVar(&opts.nodeVersion, "node-version", "", "Node.js version, see 'frbit node-versions list'")
	flags.StringVar(&opts.softwareVersion, "software-version", "", "Major software version")
	flags.StringVar(&opts.phpVersion, "php-version", "", "PHP runtime version (stops automatic PHP updates)")
	flags.BoolVar(&opts.deployHook, "deploy-hook", false, "Enable the deploy hook (--deploy-hook=false disables it)")
	flags.BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

type listSetting struct {
	flag    string
	clear   string
	key     string
	values  []string
	cleared bool
}

func environmentUpdatePayload(command *cobra.Command, factory *app.Factory, opts updateOptions) (any, error) {
	inputFlags := []string{"name", "branch", "directory", "root-path", "build-command", "post-deploy-command", "clear-build-commands", "clear-post-deploy-commands", "strategy", "exclude-pattern", "clear-exclude-patterns", "replace-pattern", "clear-replace-patterns", "cached-directory", "clear-cached-directories", "trigger", "permission", "node-version", "deploy-hook", "software-version", "php-version"}
	if opts.file != "" {
		if cmdutil.AnyFlagChanged(command, inputFlags...) {
			return nil, fmt.Errorf("--file cannot be combined with request field flags")
		}
		return cmdutil.ReadJSONObject(opts.file, factory.IOStreams.In)
	}
	if opts.clearBuildCommands && command.Flags().Changed("build-command") {
		return nil, fmt.Errorf("--clear-build-commands cannot be combined with --build-command")
	}
	if opts.clearPostDeployCommands && command.Flags().Changed("post-deploy-command") {
		return nil, fmt.Errorf("--clear-post-deploy-commands cannot be combined with --post-deploy-command")
	}
	if opts.directory != "" && opts.branch == "" {
		return nil, fmt.Errorf("--branch is required when --directory is supplied")
	}

	payload := map[string]any{}
	if command.Flags().Changed("name") {
		payload["name"] = opts.name
	}
	if command.Flags().Changed("root-path") {
		payload["rootPath"] = opts.rootPath
	}
	if command.Flags().Changed("software-version") {
		payload["softwareVersion"] = opts.softwareVersion
	}
	if command.Flags().Changed("php-version") {
		payload["phpVersion"] = opts.phpVersion
	}
	if command.Flags().Changed("branch") {
		git := map[string]any{"branch": opts.branch}
		if command.Flags().Changed("directory") {
			git["directory"] = opts.directory
		}
		payload["git"] = git
	}
	if command.Flags().Changed("build-command") {
		payload["buildCommands"] = opts.buildCommands
	} else if opts.clearBuildCommands {
		payload["buildCommands"] = []string{}
	}
	if command.Flags().Changed("post-deploy-command") {
		payload["postDeployCommands"] = opts.postDeployCommands
	} else if opts.clearPostDeployCommands {
		payload["postDeployCommands"] = []string{}
	}

	deployment := map[string]any{}
	if command.Flags().Changed("strategy") {
		if opts.strategy != "merge" && opts.strategy != "replace" {
			return nil, fmt.Errorf("--strategy must be merge or replace")
		}
		deployment["strategy"] = opts.strategy
	}
	if command.Flags().Changed("trigger") {
		if opts.trigger != "push" && opts.trigger != "manual" && opts.trigger != "hook" {
			return nil, fmt.Errorf("--trigger must be push, manual, or hook")
		}
		deployment["trigger"] = opts.trigger
	}
	if command.Flags().Changed("permission") {
		if opts.permission != "connectedDevelopers" && opts.permission != "repositoryAccess" {
			return nil, fmt.Errorf("--permission must be connectedDevelopers or repositoryAccess")
		}
		deployment["permission"] = opts.permission
	}
	if command.Flags().Changed("node-version") {
		deployment["nodeVersion"] = opts.nodeVersion
	}
	if command.Flags().Changed("deploy-hook") {
		deployment["deployHook"] = opts.deployHook
	}
	// The API clears a list with null, not [].
	for _, setting := range []listSetting{
		{"exclude-pattern", "clear-exclude-patterns", "excludePatterns", opts.excludePatterns, opts.clearExcludePatterns},
		{"replace-pattern", "clear-replace-patterns", "replacePatterns", opts.replacePatterns, opts.clearReplacePatterns},
		{"cached-directory", "clear-cached-directories", "cachedDirectories", opts.cachedDirectories, opts.clearCachedDirectories},
	} {
		if setting.cleared && command.Flags().Changed(setting.flag) {
			return nil, fmt.Errorf("--%s cannot be combined with --%s", setting.clear, setting.flag)
		}
		if command.Flags().Changed(setting.flag) {
			deployment[setting.key] = setting.values
		} else if setting.cleared {
			deployment[setting.key] = nil
		}
	}
	if len(deployment) > 0 {
		payload["deployment"] = deployment
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("provide an update flag or --file")
	}
	return payload, nil
}

func newCmdVariables(factory *app.Factory) *cobra.Command {
	command := &cobra.Command{Use: "variables", Short: "Read and update environment variables", Args: cobra.NoArgs}
	command.AddCommand(newCmdVariablesGet(factory), newCmdVariablesUpdate(factory))
	return command
}

func newCmdVariablesGet(factory *app.Factory) *cobra.Command {
	var printJSON bool
	var reveal bool
	command := &cobra.Command{
		Use:   "get <public-id>",
		Short: "Get custom and platform environment variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.GetResource(cmd.Context(), "/v1/environments/"+args[0]+"/environment-variables")
			if err != nil {
				return err
			}
			if printJSON && reveal {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			if printJSON {
				return writeEnvironmentVariablesJSON(cmd.OutOrStdout(), response.Resource)
			}
			return writeEnvironmentVariables(cmd.OutOrStdout(), response.Resource, reveal)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print environment variables as JSON (values remain masked unless --reveal is set)")
	command.Flags().BoolVar(&reveal, "reveal", false, "Show environment variable values")
	return command
}

func newCmdVariablesUpdate(factory *app.Factory) *cobra.Command {
	var file string
	var setValues []string
	var deleteNames []string
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <public-id>",
		Short: "Add, change, or delete custom environment variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var payload any
			if file != "" {
				if cmdutil.AnyFlagChanged(cmd, "set", "delete") {
					return fmt.Errorf("--file cannot be combined with --set or --delete")
				}
				input, err := cmdutil.ReadJSONObject(file, factory.IOStreams.In)
				if err != nil {
					return err
				}
				payload = input
			} else {
				set, err := cmdutil.ParseAssignments(setValues)
				if err != nil {
					return err
				}
				for _, name := range deleteNames {
					if _, exists := set[name]; exists {
						return fmt.Errorf("%s cannot be set and deleted in the same request", name)
					}
				}
				if len(set) == 0 && len(deleteNames) == 0 {
					return fmt.Errorf("provide --set, --delete, or --file")
				}
				payload = map[string]any{"set": set, "delete": deleteNames}
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/environments/"+args[0]+"/environment-variables", payload)
			if err != nil {
				return err
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return writeEnvironmentVariables(cmd.OutOrStdout(), response.Resource, false)
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "Read the complete request body from a JSON file ('-' for stdin)")
	command.Flags().StringArrayVar(&setValues, "set", nil, "Variable to create or update as NAME=VALUE (repeatable; use --file or --file - for secrets)")
	command.Flags().StringArrayVar(&deleteNames, "delete", nil, "Variable name to delete (repeatable)")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func newCmdRestart(factory *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <public-id>",
		Short: "Request an environment restart",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if _, err := client.PostAction(cmd.Context(), "/v1/environments/"+args[0]+"/restart"); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Restart requested for %s.\n", args[0])
			return err
		},
	}
}

func newCmdDeploy(factory *app.Factory) *cobra.Command {
	var printJSON bool
	command := &cobra.Command{
		Use:   "deploy <public-id>",
		Short: "Create a deployment for an environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.PostAction(cmd.Context(), "/v1/environments/"+args[0]+"/deployments")
			if err != nil {
				return err
			}
			return writeResourceResponse(cmd, response, printJSON)
		},
	}
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func writeResourceResponse(command *cobra.Command, response api.ResourceResponse, printJSON bool) error {
	if printJSON {
		_, err := fmt.Fprintf(command.OutOrStdout(), "%s\n", response.Raw)
		return err
	}
	return resource.WriteResource(command.OutOrStdout(), response.Resource)
}

func writeEnvironmentVariables(output io.Writer, value api.Resource, reveal bool) error {
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	wroteHeader := false
	for _, kind := range []string{"custom", "platform"} {
		variables, _ := value[kind].([]any)
		for _, raw := range variables {
			variable, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !wroteHeader {
				if _, err := fmt.Fprintln(table, "TYPE\tNAME\tVALUE"); err != nil {
					return err
				}
				wroteHeader = true
			}
			variableValue := "***"
			if reveal && variable["value"] != nil {
				variableValue = fmt.Sprint(variable["value"])
			}
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", kind, fmt.Sprint(variable["name"]), variableValue); err != nil {
				return err
			}
		}
	}
	if !wroteHeader {
		_, err := fmt.Fprintln(output, "No environment variables found.")
		return err
	}
	return table.Flush()
}

func writeEnvironmentVariablesJSON(output io.Writer, value api.Resource) error {
	masked := make(api.Resource, len(value))
	for key, raw := range value {
		variables, ok := raw.([]any)
		if !ok || (key != "custom" && key != "platform") {
			masked[key] = raw
			continue
		}
		entries := make([]any, len(variables))
		for index, entry := range variables {
			variable, ok := entry.(map[string]any)
			if !ok {
				entries[index] = entry
				continue
			}
			copy := make(map[string]any, len(variable))
			for field, fieldValue := range variable {
				copy[field] = fieldValue
			}
			if copy["value"] != nil {
				copy["value"] = "***"
			}
			entries[index] = copy
		}
		masked[key] = entries
	}
	encoded, err := json.Marshal(masked)
	if err != nil {
		return fmt.Errorf("encode environment variables: %w", err)
	}
	_, err = fmt.Fprintf(output, "%s\n", encoded)
	return err
}
