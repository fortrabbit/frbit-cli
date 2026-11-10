package api

import (
	"encoding/json"
	"mime"
	"strings"
)

// Problem is an RFC 9457 problem details document returned on API errors.
type Problem struct {
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Detail     string      `json:"detail"`
	Status     int         `json:"status"`
	Violations []Violation `json:"violations"`
	Raw        []byte      `json:"-"`
}

type Violation struct {
	PropertyPath string `json:"propertyPath"`
	Message      string `json:"message"`
}

// parseProblem returns nil unless body is a problem document: problem+json, or a JSON object with a title or type.
func parseProblem(contentType string, body []byte) *Problem {
	var problem Problem
	if err := json.Unmarshal(body, &problem); err != nil {
		return nil
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType != "application/problem+json" && problem.Title == "" && problem.Type == "" {
		return nil
	}
	problem.Raw = body
	return &problem
}

// message is the title, then one line per violation, or the detail when there are none.
func (p *Problem) message() string {
	title := strings.TrimSpace(p.Title)
	detail := strings.TrimSpace(p.Detail)
	lines := make([]string, 0, len(p.Violations))
	for _, violation := range p.Violations {
		switch {
		case violation.PropertyPath != "":
			lines = append(lines, "  "+violation.PropertyPath+": "+violation.Message)
		case violation.Message != "":
			lines = append(lines, "  "+violation.Message)
		}
	}
	if len(lines) > 0 {
		if title == "" {
			title = "Validation failed"
		}
		return title + "\n" + strings.Join(lines, "\n")
	}
	switch {
	case title == "":
		return detail
	case detail == "" || detail == title:
		return title
	default:
		return title + ": " + detail
	}
}
