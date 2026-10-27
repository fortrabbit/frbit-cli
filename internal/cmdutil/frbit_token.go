package cmdutil

import (
	"os"
	"strings"
)

// FrbitTokenWarning tells a person that an environment token hides the stored sign-in.
const FrbitTokenWarning = "FRBIT_TOKEN is set and is used instead of a stored sign-in. Unset it, or revoke the token under API tokens."

// FrbitTokenSet reports whether FRBIT_TOKEN overrides the stored credential.
func FrbitTokenSet() bool {
	return strings.TrimSpace(os.Getenv("FRBIT_TOKEN")) != ""
}
