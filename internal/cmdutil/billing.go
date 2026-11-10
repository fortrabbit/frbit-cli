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
	quote, err := priceQuote(cmd, client, path, payload)
	if err != nil {
		return false, err
	}
	if quote.cents == 0 || quote.trial {
		return true, nil
	}
	return AskBilled(cmd, f, quote.summary(), "Create and start billing? [y/N] ", yes)
}

// DryRunBilled sends only the dry-run POST and prints the price, or the API response with asJSON; noun names what would be created.
func DryRunBilled(cmd *cobra.Command, client *api.Client, path string, payload any, noun string, asJSON bool) error {
	quote, err := priceQuote(cmd, client, path, payload)
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
		text = fmt.Sprintf("The %s would be created as a trial at no cost", noun)
		if until, ok := quote.trialEndsAt.(string); ok && until != "" {
			text += " until " + until
		}
		if quote.cents > 0 {
			text += fmt.Sprintf("; after the trial: %.2f %s/month", quote.cents/100, quote.currency)
		}
		text += "."
	}
	_, err = fmt.Fprintf(out, "%s\nDry run: nothing was created.\n", text)
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
func priceQuote(cmd *cobra.Command, client *api.Client, path string, payload any) (quote, error) {
	body, err := dryRunBody(payload)
	if err != nil {
		return quote{}, err
	}
	response, err := client.DryRunCreate(cmd.Context(), path, body)
	if err != nil {
		return quote{}, err
	}
	pricing, ok := response.Resource["pricing"].(map[string]any)
	cents, isNumber := float64(0), false
	if ok {
		cents, isNumber = pricing["monthlyPriceInCents"].(float64)
	}
	if !ok || !isNumber {
		return quote{}, fmt.Errorf("dry run returned no price; refusing to create")
	}
	currency, _ := pricing["currency"].(string)
	billed, billedKnown := pricing["billed"].(bool)
	return quote{
		cents:         cents,
		currency:      currency,
		trial:         billedKnown && !billed && pricing["trialEndsAt"] != nil,
		trialEndsAt:   pricing["trialEndsAt"],
		paymentMethod: paymentMethodLabel(body),
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

// The dry run names no payment method, so show the one chosen in the request or the default.
func paymentMethodLabel(body map[string]any) string {
	if id, _ := body["paymentMethodId"].(string); id != "" {
		return id
	}
	return "default payment method"
}
