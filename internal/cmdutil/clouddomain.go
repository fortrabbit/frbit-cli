package cmdutil

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

var skipReasons = map[string]string{
	"invalid":  "not a valid name; use a-z, 0-9 and hyphens",
	"reserved": "reserved",
	"taken":    "already taken",
}

// WriteCloudDomain says after a create which cloud domain the new environment got, or why it got none. created is the API response of an app or an environment; environmentID names the environment in the hint; lookupID, set for an app, is the environment whose domains are fetched. A lookup that fails prints nothing: the create itself succeeded.
func WriteCloudDomain(ctx context.Context, client *api.Client, out io.Writer, created api.Resource, environmentID, lookupID string) error {
	if skipped, ok := created["skippedCloudDomain"].(map[string]any); ok {
		label, _ := skipped["label"].(string)
		reason, _ := skipped["reason"].(string)
		if text, known := skipReasons[reason]; known {
			reason = text
		}
		_, err := fmt.Fprintf(out, "\nNo cloud domain: %q is %s.\nAdd one with another name: frbit domains create %s <name> --cloud\n", label, reason, environmentID)
		return err
	}
	domainIDs, hasDomains := created["domains"].([]any)
	if !hasDomains && lookupID != "" {
		// An app response names its environments, not their domains.
		environment, err := client.GetResource(ctx, "/v1/environments/"+url.PathEscape(lookupID))
		if err != nil {
			return nil
		}
		domainIDs, _ = environment.Resource["domains"].([]any)
	}
	for _, raw := range domainIDs {
		id, _ := raw.(string)
		if id == "" {
			continue
		}
		domain, err := client.GetResource(ctx, "/v1/domains/"+url.PathEscape(id))
		if err != nil {
			return nil
		}
		if domain.Resource["type"] == "cloud" {
			_, err := fmt.Fprintf(out, "\nCloud domain: https://%s\n", domain.Resource["name"])
			return err
		}
	}
	return nil
}

// FirstEnvironment returns the first environment ID of an app response.
func FirstEnvironment(app api.Resource) string {
	environments, _ := app["environments"].([]any)
	if len(environments) == 0 {
		return ""
	}
	id, _ := environments[0].(string)
	return id
}
