package apps

import (
	"fmt"
	"strings"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdApps(factory *app.Factory) *cobra.Command {
	command := resource.NewCmdGroup(factory, resource.Spec{
		Use:      "apps",
		Singular: "app",
		Path:     "/apps",
		Short:    "List and inspect apps",
		Delete: &resource.DeleteSpec{
			Warning: "Deleting this app permanently removes all of its environments and their infrastructure. All files and database contents will be erased.",
		},
		SupportsPage:   true,
		SupportsFilter: true,
		Fields: []resource.Field{
			{Header: "ID", Key: "publicId"},
			{Header: "NAME", Key: "name"},
			{Header: "DESCRIPTION", Key: "description"},
			{Header: "TEAMS", Key: "teams"},
			{Header: "TRIAL", Key: "trial"},
			{Header: "UPDATED", Key: "updatedAt"},
		},
	})
	command.AddCommand(newCmdCreate(factory), newCmdUpdate(factory))
	for _, sub := range command.Commands() {
		if sub.Name() == "list" {
			run := sub.RunE
			sub.RunE = func(cmd *cobra.Command, args []string) error {
				if err := run(cmd, args); err != nil {
					return err
				}
				cmdutil.SuggestClaudePlugin(cmd.ErrOrStderr())
				return nil
			}
		}
	}
	return command
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var file string
	var name string
	var region string
	var teamID string
	var softwarePreset string
	var softwareVersion string
	var noCloudDomain bool
	var components []string
	var autoscaling bool
	var repository string
	var branch string
	var directory string
	var rootPath string
	var buildCommands []string
	var postDeployCommands []string
	var deploy bool
	var paymentMethodID string
	var printJSON bool
	var yes bool

	command := &cobra.Command{
		Use:   "create",
		Short: "Create an app and its initial environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := appCreatePayload(cmd, factory, file, name, region, teamID, softwarePreset, softwareVersion, noCloudDomain, components, autoscaling, repository, branch, directory, rootPath, buildCommands, postDeployCommands, deploy, paymentMethodID)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			confirmed, err := cmdutil.ConfirmBilled(cmd, factory, client, "/v1/apps", payload, yes)
			if err != nil {
				return err
			}
			if !confirmed {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return err
			}
			response, err := client.CreateResource(cmd.Context(), "/v1/apps", payload)
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
	command.Flags().StringVarP(&file, "file", "f", "", "Read the complete request body from a JSON file ('-' for stdin)")
	command.Flags().StringVar(&name, "name", "", "App name")
	command.Flags().StringVar(&region, "region", "", "Region identifier")
	command.Flags().StringVar(&teamID, "team", "", "Owning team public ID")
	command.Flags().StringVar(&softwarePreset, "software", "", "Software preset slug")
	command.Flags().StringVar(&softwareVersion, "software-version", "", "Major software version")
	command.Flags().BoolVar(&noCloudDomain, "no-cloud-domain", false, "Create the initial environment without a cloud domain")
	command.Flags().StringArrayVar(&components, "component", nil, "Component plan as SLUG=SIZE (repeatable)")
	command.Flags().BoolVar(&autoscaling, "autoscaling", true, "Enable autoscaling for the initial environment")
	command.Flags().StringVar(&repository, "repository", "", "Connected Git repository in OWNER/REPOSITORY form")
	command.Flags().StringVar(&branch, "branch", "", "Git branch for the initial environment")
	command.Flags().StringVar(&directory, "directory", "", "Git source directory")
	command.Flags().StringVar(&rootPath, "root-path", "", "Folder of the repository the web server serves, e.g. public")
	command.Flags().StringArrayVar(&buildCommands, "build-command", nil, "Build command (repeatable)")
	command.Flags().StringArrayVar(&postDeployCommands, "post-deploy-command", nil, "Post-deploy command (repeatable)")
	command.Flags().BoolVar(&deploy, "deploy", false, "Start the first deployment after creation")
	command.Flags().StringVar(&paymentMethodID, "payment-method", "", "Payment method public ID")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	command.Flags().BoolVar(&yes, "yes", false, "Confirm billing without asking")
	return command
}

func appCreatePayload(command *cobra.Command, factory *app.Factory, file string, name string, region string, teamID string, softwarePreset string, softwareVersion string, noCloudDomain bool, componentValues []string, autoscaling bool, repository string, branch string, directory string, rootPath string, buildCommands []string, postDeployCommands []string, deploy bool, paymentMethodID string) (any, error) {
	inputFlags := []string{"name", "region", "team", "software", "software-version", "no-cloud-domain", "component", "autoscaling", "repository", "branch", "directory", "root-path", "build-command", "post-deploy-command", "deploy", "payment-method"}
	if file != "" {
		if cmdutil.AnyFlagChanged(command, inputFlags...) {
			return nil, fmt.Errorf("--file cannot be combined with request field flags")
		}
		return cmdutil.ReadJSONObject(file, factory.IOStreams.In)
	}
	if name == "" || region == "" {
		return nil, fmt.Errorf("--name and --region are required unless --file is used")
	}

	components, err := cmdutil.ParseAssignments(componentValues)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"teamId":             nil,
		"name":               name,
		"region":             region,
		"softwarePresetName": nil,
		"initialEnvironment": nil,
		"paymentMethodId":    nil,
	}
	if teamID != "" {
		payload["teamId"] = teamID
	}
	if softwarePreset != "" {
		payload["softwarePresetName"] = softwarePreset
	}
	if paymentMethodID != "" {
		payload["paymentMethodId"] = paymentMethodID
	}

	deploymentChanged := cmdutil.AnyFlagChanged(command, "repository", "branch", "directory", "build-command", "post-deploy-command", "deploy")
	if deploymentChanged && (repository == "" || branch == "") {
		return nil, fmt.Errorf("--repository and --branch are required when configuring deployment")
	}
	initialChanged := softwareVersion != "" || noCloudDomain || len(components) > 0 || command.Flags().Changed("autoscaling") || command.Flags().Changed("root-path") || deploymentChanged
	if initialChanged {
		initial := map[string]any{}
		if softwareVersion != "" {
			initial["softwareVersion"] = softwareVersion
		}
		if noCloudDomain {
			initial["cloudDomain"] = nil
		}
		if len(components) > 0 {
			initial["components"] = components
		}
		if command.Flags().Changed("autoscaling") {
			initial["autoscaling"] = autoscaling
		}
		if command.Flags().Changed("root-path") {
			initial["rootPath"] = rootPath
		}
		if deploymentChanged {
			git := map[string]any{"repository": repository, "branch": branch}
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
			initial["deployment"] = deployment
		}
		payload["initialEnvironment"] = initial
	}
	return payload, nil
}

// Warnings for app updates that change every environment; they follow what the API does on these changes.
const (
	connectRepositoryWarning    = "Connecting a repository clears the deploy branch and the deploy file settings of every environment of this app. No environment deploys until a branch is set again."
	softwarePresetWarning       = "Changing the software preset sets the software version of every environment of this app to the preset default and clears pinned versions. The next deployment uses that version."
	disconnectRepositoryWarning = "Disconnecting the repository resets the branches of all environments and stops deployments until a repository is connected again."
)

func newCmdUpdate(factory *app.Factory) *cobra.Command {
	var file string
	var name string
	var paymentMethodID string
	var softwarePreset string
	var gitDeployComments bool
	var technicalContacts []string
	var clearTechnicalContacts bool
	var repository string
	var disconnectRepository bool
	var confirmation string
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <public-id>",
		Short: "Update an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			publicID := args[0]
			fieldFlags := []string{"name", "payment-method", "software-preset", "git-deploy-comments", "technical-contact", "clear-technical-contacts"}
			var payload any
			if file != "" {
				if cmdutil.AnyFlagChanged(cmd, append(fieldFlags, "repository", "disconnect-repository")...) {
					return fmt.Errorf("--file cannot be combined with request field flags")
				}
				input, err := cmdutil.ReadJSONObject(file, factory.IOStreams.In)
				if err != nil {
					return err
				}
				payload = input
			} else {
				if repository != "" && disconnectRepository {
					return fmt.Errorf("--repository cannot be combined with --disconnect-repository")
				}
				if clearTechnicalContacts && len(technicalContacts) > 0 {
					return fmt.Errorf("--clear-technical-contacts cannot be combined with --technical-contact")
				}
				fields := map[string]any{}
				if cmd.Flags().Changed("name") {
					fields["name"] = name
				}
				if cmd.Flags().Changed("payment-method") {
					fields["paymentMethodId"] = paymentMethodID
				}
				if cmd.Flags().Changed("software-preset") {
					fields["softwarePresetName"] = softwarePreset
				}
				if cmd.Flags().Changed("git-deploy-comments") {
					fields["gitDeployComments"] = gitDeployComments
				}
				if clearTechnicalContacts {
					fields["technicalContacts"] = []string{}
				} else if len(technicalContacts) > 0 {
					fields["technicalContacts"] = technicalContacts
				}
				if len(fields) == 0 && repository == "" && !disconnectRepository {
					return fmt.Errorf("provide an update flag such as --name, --repository or --software-preset, or --file")
				}
				if len(fields) > 0 {
					payload = fields
				}
			}
			var warnings []string
			if repository != "" {
				warnings = append(warnings, connectRepositoryWarning)
			}
			if file == "" && cmd.Flags().Changed("software-preset") {
				warnings = append(warnings, softwarePresetWarning)
			}
			if disconnectRepository {
				warnings = append(warnings, disconnectRepositoryWarning)
			}
			if cmd.Flags().Changed("confirm") && len(warnings) == 0 {
				return fmt.Errorf("--confirm is only used with --repository, --software-preset or --disconnect-repository")
			}
			const action = "this app update"
			if len(warnings) > 0 {
				if err := cmdutil.CheckDestructive(factory, action, publicID, confirmation); err != nil {
					return err
				}
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			if len(warnings) > 0 {
				confirmed, err := cmdutil.ConfirmDestructiveContext(cmd.Context(), factory, cmd.ErrOrStderr(), action, publicID, confirmation, strings.Join(warnings, "\n"))
				if err != nil {
					return err
				}
				if !confirmed {
					_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
					return err
				}
			}
			var response api.ResourceResponse
			if payload != nil {
				if response, err = client.UpdateResource(cmd.Context(), "/v1/apps/"+publicID, payload); err != nil {
					return err
				}
			}
			if repository != "" {
				if response, err = client.PutResource(cmd.Context(), "/v1/apps/"+publicID+"/git-repository", map[string]any{"repository": repository}); err != nil {
					return partialFailure(payload != nil, "connecting", err)
				}
			}
			if disconnectRepository {
				if response, err = client.DeleteForResource(cmd.Context(), "/v1/apps/"+publicID+"/git-repository"); err != nil {
					return partialFailure(payload != nil, "disconnecting", err)
				}
			}
			if printJSON {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", response.Raw)
				return err
			}
			return resource.WriteResource(cmd.OutOrStdout(), response.Resource)
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "Read the complete request body from a JSON file ('-' for stdin)")
	command.Flags().StringVar(&name, "name", "", "New app name")
	command.Flags().StringVar(&paymentMethodID, "payment-method", "", "New payment method public ID")
	command.Flags().StringVar(&softwarePreset, "software-preset", "", "Software preset slug; resets the software version of every environment to the preset default (asks first)")
	command.Flags().BoolVar(&gitDeployComments, "git-deploy-comments", false, "Comment on GitHub pull requests with the deployment (true or false)")
	command.Flags().StringArrayVar(&technicalContacts, "technical-contact", nil, "Technical contact person public ID, replaces the list (repeatable)")
	command.Flags().BoolVar(&clearTechnicalContacts, "clear-technical-contacts", false, "Remove all technical contacts")
	command.Flags().StringVar(&repository, "repository", "", "Connect a GitHub repository in OWNER/REPOSITORY form; clears every environment's deploy branch (asks first)")
	command.Flags().BoolVar(&disconnectRepository, "disconnect-repository", false, "Disconnect the GitHub repository; stops deployments until one is connected again (asks first)")
	command.Flags().StringVar(&confirmation, "confirm", "", "Confirm --repository, --software-preset or --disconnect-repository by repeating the app ID")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func partialFailure(settingsApplied bool, verb string, err error) error {
	if !settingsApplied {
		return err
	}
	return fmt.Errorf("app settings updated, but %s the repository failed: %w", verb, err)
}
