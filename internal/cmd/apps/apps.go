package apps

import (
	"fmt"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/resource"
	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdApps(factory *app.Factory) *cobra.Command {
	command := resource.NewCmdGroup(factory, resource.Spec{
		Use:            "apps",
		Singular:       "app",
		Path:           "/apps",
		Short:          "List and inspect apps",
		SupportsPage:   true,
		SupportsFilter: true,
		Fields: []resource.Field{
			{Header: "ID", Key: "publicId"},
			{Header: "NAME", Key: "name"},
			{Header: "DESCRIPTION", Key: "description"},
			{Header: "TRIAL", Key: "trial"},
			{Header: "UPDATED", Key: "updatedAt"},
		},
	})
	command.AddCommand(newCmdCreate(factory), newCmdUpdate(factory))
	return command
}

func newCmdCreate(factory *app.Factory) *cobra.Command {
	var file string
	var name string
	var region string
	var teamID string
	var softwarePreset string
	var softwareVersion string
	var components []string
	var autoscaling bool
	var repository string
	var branch string
	var directory string
	var buildCommands []string
	var postDeployCommands []string
	var deploy bool
	var paymentMethodID string
	var printJSON bool

	command := &cobra.Command{
		Use:   "create",
		Short: "Create an app and its initial environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := appCreatePayload(cmd, factory, file, name, region, teamID, softwarePreset, softwareVersion, components, autoscaling, repository, branch, directory, buildCommands, postDeployCommands, deploy, paymentMethodID)
			if err != nil {
				return err
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
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
	command.Flags().StringArrayVar(&components, "component", nil, "Component plan as SLUG=SIZE (repeatable)")
	command.Flags().BoolVar(&autoscaling, "autoscaling", true, "Enable autoscaling for the initial environment")
	command.Flags().StringVar(&repository, "repository", "", "Connected Git repository in OWNER/REPOSITORY form")
	command.Flags().StringVar(&branch, "branch", "", "Git branch for the initial environment")
	command.Flags().StringVar(&directory, "directory", "", "Git source directory")
	command.Flags().StringArrayVar(&buildCommands, "build-command", nil, "Build command (repeatable)")
	command.Flags().StringArrayVar(&postDeployCommands, "post-deploy-command", nil, "Post-deploy command (repeatable)")
	command.Flags().BoolVar(&deploy, "deploy", false, "Start the first deployment after creation")
	command.Flags().StringVar(&paymentMethodID, "payment-method", "", "Payment method public ID")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}

func appCreatePayload(command *cobra.Command, factory *app.Factory, file string, name string, region string, teamID string, softwarePreset string, softwareVersion string, componentValues []string, autoscaling bool, repository string, branch string, directory string, buildCommands []string, postDeployCommands []string, deploy bool, paymentMethodID string) (any, error) {
	inputFlags := []string{"name", "region", "team", "software", "software-version", "component", "autoscaling", "repository", "branch", "directory", "build-command", "post-deploy-command", "deploy", "payment-method"}
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
	initialChanged := softwareVersion != "" || len(components) > 0 || command.Flags().Changed("autoscaling") || deploymentChanged
	if initialChanged {
		initial := map[string]any{}
		if softwareVersion != "" {
			initial["softwareVersion"] = softwareVersion
		}
		if len(components) > 0 {
			initial["components"] = components
		}
		if command.Flags().Changed("autoscaling") {
			initial["autoscaling"] = autoscaling
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

func newCmdUpdate(factory *app.Factory) *cobra.Command {
	var file string
	var name string
	var paymentMethodID string
	var printJSON bool
	command := &cobra.Command{
		Use:   "update <public-id>",
		Short: "Update an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var payload any
			if file != "" {
				if cmdutil.AnyFlagChanged(cmd, "name", "payment-method") {
					return fmt.Errorf("--file cannot be combined with request field flags")
				}
				input, err := cmdutil.ReadJSONObject(file, factory.IOStreams.In)
				if err != nil {
					return err
				}
				payload = input
			} else {
				fields := map[string]any{}
				if cmd.Flags().Changed("name") {
					fields["name"] = name
				}
				if cmd.Flags().Changed("payment-method") {
					fields["paymentMethodId"] = paymentMethodID
				}
				if len(fields) == 0 {
					return fmt.Errorf("provide --name, --payment-method, or --file")
				}
				payload = fields
			}
			client, err := cmdutil.APIClient(cmd, factory)
			if err != nil {
				return err
			}
			response, err := client.UpdateResource(cmd.Context(), "/v1/apps/"+args[0], payload)
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
	command.Flags().StringVar(&name, "name", "", "New app name")
	command.Flags().StringVar(&paymentMethodID, "payment-method", "", "New payment method public ID")
	command.Flags().BoolVar(&printJSON, "json", false, "Print the API response as JSON")
	return command
}
