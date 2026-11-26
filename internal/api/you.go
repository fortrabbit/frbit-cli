package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type Step struct {
	Step        string  `json:"step"`
	Instruction string  `json:"instruction"`
	URL         *string `json:"url"`
}

type OptionalStep struct {
	Step        string  `json:"step"`
	Done        bool    `json:"done"`
	Instruction string  `json:"instruction"`
	URL         *string `json:"url"`
}

type You struct {
	PublicID string         `json:"publicId"`
	Email    string         `json:"email"`
	Name     string         `json:"name"`
	Next     *Step          `json:"next"`
	Optional []OptionalStep `json:"optional"`
}

type YouResponse struct {
	You You
	// Next keeps the server's hint verbatim for JSON output.
	Next json.RawMessage
	Raw  []byte
}

func (c Client) You(ctx context.Context) (YouResponse, error) {
	body, err := c.get(ctx, "/v1/you", nil)
	if err != nil {
		return YouResponse{}, err
	}
	object := body
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return YouResponse{}, fmt.Errorf("decode /v1/you response: %w", err)
	}
	if data, ok := envelope["data"]; ok && envelope["publicId"] == nil && envelope["email"] == nil {
		object = data
		envelope = nil
		if err := json.Unmarshal(object, &envelope); err != nil {
			return YouResponse{}, fmt.Errorf("decode /v1/you response: %w", err)
		}
	}
	var you You
	if err := json.Unmarshal(object, &you); err != nil {
		return YouResponse{}, fmt.Errorf("decode /v1/you response: %w", err)
	}
	next := envelope["next"]
	if next == nil {
		next = json.RawMessage("null")
	}
	return YouResponse{You: you, Next: next, Raw: body}, nil
}

// Link is the URL of the step when it is an https URL, else "".
func (s OptionalStep) Link() string {
	if s.URL == nil || !strings.HasPrefix(*s.URL, "https://") || strings.IndexFunc(*s.URL, unicode.IsControl) >= 0 {
		return ""
	}
	return *s.URL
}

// OptionalStep returns the optional step named name, or nil when the server lists none.
func (y You) OptionalStep(name string) *OptionalStep {
	for index := range y.Optional {
		if y.Optional[index].Step == name {
			return &y.Optional[index]
		}
	}
	return nil
}
