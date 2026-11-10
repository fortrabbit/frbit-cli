package cmdutil

import (
	"encoding/json"
	"fmt"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/spf13/cobra"
)

// ConfirmBilled runs a dry-run POST of payload to path, prints the monthly price and asks before a billed create.
// Prices and the question go to stderr so --json output stays clean.
func ConfirmBilled(cmd *cobra.Command, f *app.Factory, client *api.Client, path string, payload any, yes bool) (bool, error) {
	body, err := dryRunBody(payload)
	if err != nil {
		return false, err
	}
	response, err := client.CreateResource(cmd.Context(), path, body, api.WithoutIdempotencyKey())
	if err != nil {
		return false, err
	}
	pricing, ok := response.Resource["pricing"].(map[string]any)
	cents, isNumber := float64(0), false
	if ok {
		cents, isNumber = pricing["monthlyPriceInCents"].(float64)
	}
	if !ok || !isNumber {
		return false, fmt.Errorf("dry run returned no price; refusing to create")
	}
	currency, _ := pricing["currency"].(string)
	billed, billedKnown := pricing["billed"].(bool)
	if cents == 0 || billedKnown && !billed && pricing["trialEndsAt"] != nil {
		return true, nil
	}
	summary := fmt.Sprintf("Monthly price: %.2f %s\nPayment method: %s", cents/100, currency, paymentMethodLabel(body))
	return AskBilled(cmd, f, summary, yes)
}

// AskBilled prints summary and gets consent: --yes, a y/N answer on a terminal, else an error.
func AskBilled(cmd *cobra.Command, f *app.Factory, summary string, yes bool) (bool, error) {
	return AskBilledWith(cmd, f, summary, "Create and start billing? [y/N] ", yes)
}

// AskBilledWith is AskBilled with a custom y/N prompt.
func AskBilledWith(cmd *cobra.Command, f *app.Factory, summary, prompt string, yes bool) (bool, error) {
	out := cmd.ErrOrStderr()
	if _, err := fmt.Fprintln(out, summary); err != nil {
		return false, err
	}
	if yes {
		return true, nil
	}
	if !f.IOStreams.IsTTY {
		return false, fmt.Errorf("billed change needs --yes")
	}
	return ConfirmContext(cmd.Context(), f.IOStreams.In, out, prompt)
}

func dryRunBody(payload any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	body["dryRun"] = true
	return body, nil
}

// The dry run names no payment method, so show the one chosen in the request or the default.
func paymentMethodLabel(body map[string]any) string {
	if id, _ := body["paymentMethodId"].(string); id != "" {
		return id
	}
	return "default payment method"
}
