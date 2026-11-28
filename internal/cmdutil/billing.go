package cmdutil

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fortrabbit/frbit-cli/internal/api"
	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/spf13/cobra"
)

// billedAction is a billed create (POST) or change (PATCH) that the API prices when the body carries dryRun: true.
type billedAction struct {
	send      func(ctx context.Context, path string, payload any) (api.ResourceResponse, error)
	prompt    string
	trialLead string
	unchanged string
}

func creation(client *api.Client, noun string) billedAction {
	return billedAction{
		send:      client.DryRunCreate,
		prompt:    "Create and start billing? [y/N] ",
		trialLead: fmt.Sprintf("The %s would be created as a trial at no cost", noun),
		unchanged: "Dry run: nothing was created.",
	}
}

func change(client *api.Client) billedAction {
	return billedAction{
		send:      client.UpdateResource,
		prompt:    "Change and bill the new price? [y/N] ",
		trialLead: "The change is free during the trial",
		unchanged: "Dry run: nothing was changed.",
	}
}

// ConfirmBilled runs a dry-run POST of payload to path, prints the monthly price and asks before a billed create.
// Prices and the question go to stderr so --json output stays clean.
func ConfirmBilled(cmd *cobra.Command, f *app.Factory, client *api.Client, path string, payload any, yes bool) (bool, error) {
	return confirmBilled(cmd, f, creation(client, ""), path, payload, yes)
}

// ConfirmBilledChange runs a dry-run PATCH of payload to path, prints the monthly price after the change and asks before a billed change.
func ConfirmBilledChange(cmd *cobra.Command, f *app.Factory, client *api.Client, path string, payload any, yes bool) (bool, error) {
	return confirmBilled(cmd, f, change(client), path, payload, yes)
}

func confirmBilled(cmd *cobra.Command, f *app.Factory, action billedAction, path string, payload any, yes bool) (bool, error) {
	quote, err := priceQuote(cmd, action, path, payload)
	if err != nil {
		return false, err
	}
	if quote.cents == 0 || quote.trial {
		return true, nil
	}
	return AskBilled(cmd, f, quote.summary(), action.prompt, yes)
}

// DryRunBilled sends only the dry-run POST and prints the price, or the API response with asJSON; noun names what would be created.
func DryRunBilled(cmd *cobra.Command, client *api.Client, path string, payload any, noun string, asJSON bool) error {
	return dryRunBilled(cmd, creation(client, noun), path, payload, asJSON)
}

// DryRunBilledChange sends only the dry-run PATCH and prints the price after the change, or the API response with asJSON.
func DryRunBilledChange(cmd *cobra.Command, client *api.Client, path string, payload any, asJSON bool) error {
	return dryRunBilled(cmd, change(client), path, payload, asJSON)
}

func dryRunBilled(cmd *cobra.Command, action billedAction, path string, payload any, asJSON bool) error {
	quote, err := priceQuote(cmd, action, path, payload)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		_, err = fmt.Fprintf(out, "%s\n", quote.raw)
		return err
	}
	text := quote.summary()
	if quote.trial {
		text = action.trialLead
		if until, ok := quote.trialEndsAt.(string); ok && until != "" {
			text += " until " + until
		}
		if quote.cents > 0 {
			text += fmt.Sprintf("; after the trial: %.2f %s/month", quote.cents/100, quote.currency)
		}
		text += "."
	}
	_, err = fmt.Fprintf(out, "%s\n%s\n", text, action.unchanged)
	return err
}

type quote struct {
	cents         float64
	currency      string
	trial         bool
	trialEndsAt   any
	paymentMethod string
	raw           []byte
}

func (q quote) summary() string {
	return fmt.Sprintf("Monthly price: %.2f %s\nPayment method: %s", q.cents/100, q.currency, q.paymentMethod)
}

// priceQuote sends payload with dryRun: true and without an Idempotency-Key, and reads the price.
func priceQuote(cmd *cobra.Command, action billedAction, path string, payload any) (quote, error) {
	body, err := dryRunBody(payload)
	if err != nil {
		return quote{}, err
	}
	response, err := action.send(cmd.Context(), path, body)
	if err != nil {
		return quote{}, err
	}
	pricing, ok := response.Resource["pricing"].(map[string]any)
	cents, isNumber := float64(0), false
	if ok {
		cents, isNumber = pricing["monthlyPriceInCents"].(float64)
	}
	if !ok || !isNumber {
		return quote{}, fmt.Errorf("dry run returned no price; refusing to continue")
	}
	currency, _ := pricing["currency"].(string)
	billed, billedKnown := pricing["billed"].(bool)
	return quote{
		cents:         cents,
		currency:      currency,
		trial:         billedKnown && !billed && pricing["trialEndsAt"] != nil,
		trialEndsAt:   pricing["trialEndsAt"],
		paymentMethod: paymentMethodLabel(response.Resource, body),
		raw:           response.Raw,
	}, nil
}

// AskBilled prints summary and gets consent: --yes, a y/N answer to prompt on a terminal, else an error.
func AskBilled(cmd *cobra.Command, f *app.Factory, summary, prompt string, yes bool) (bool, error) {
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

// paymentMethodLabel prefers the payment method the dry run names, then the one chosen in the request, else the default.
func paymentMethodLabel(response api.Resource, body map[string]any) string {
	if named, present := response["paymentMethod"]; present {
		if id, _ := named.(string); id != "" {
			return id
		}
		return "none"
	}
	if id, _ := body["paymentMethodId"].(string); id != "" {
		return id
	}
	return "default payment method"
}
