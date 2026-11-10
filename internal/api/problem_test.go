package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func problemClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestHTTPErrorPrintsTitleDetailAndViolations(t *testing.T) {
	const body = `{"title":"Validation failed","detail":"name: too short","violations":[{"propertyPath":"name","message":"too short"}]}`
	client := problemClient(t, http.StatusUnprocessableEntity, body)

	_, err := client.CreateResource(context.Background(), "/v1/apps", map[string]any{})
	if err == nil {
		t.Fatal("expected an error")
	}
	text := err.Error()
	if !strings.Contains(text, "Validation failed") {
		t.Fatalf("error %q does not contain the title", text)
	}
	if count := strings.Count(text, "name: too short"); count != 1 {
		t.Fatalf("error %q contains %q %d times, want once", text, "name: too short", count)
	}
	if !strings.Contains(text, "\n  name: too short") {
		t.Fatalf("error %q does not print the violation on its own line", text)
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Problem == nil {
		t.Fatalf("error = %#v, want HTTPError with Problem", err)
	}
	problem := httpErr.Problem
	if problem.Title != "Validation failed" || problem.Detail != "name: too short" || len(problem.Violations) != 1 || problem.Violations[0].PropertyPath != "name" || problem.Violations[0].Message != "too short" {
		t.Fatalf("problem = %#v", problem)
	}
	if string(problem.Raw) != body {
		t.Fatalf("raw = %q, want %q", problem.Raw, body)
	}
}

func TestHTTPErrorFallsBackWithoutLegacyMembers(t *testing.T) {
	client := problemClient(t, http.StatusForbidden, `{"type":"about:blank","title":"Forbidden","status":403}`)

	_, err := client.GetResource(context.Background(), "/v1/apps/ap-abc123")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403: Forbidden") {
		t.Fatalf("error = %v, want title", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Problem == nil || httpErr.Problem.Status != 403 || httpErr.Problem.Type != "about:blank" {
		t.Fatalf("error = %#v", err)
	}
}

func TestHTTPErrorAppendsDetailToTitle(t *testing.T) {
	client := problemClient(t, http.StatusNotFound, `{"type":"about:blank","title":"Not Found","status":404,"detail":"Resource not found","error":"Resource not found"}`)

	_, err := client.GetResource(context.Background(), "/v1/apps/ap-abc123")
	if err == nil || !strings.HasSuffix(err.Error(), "HTTP 404: Not Found: Resource not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPErrorWithoutJSONBodyHasNoProblem(t *testing.T) {
	client := problemClient(t, http.StatusBadGateway, `<html>bad gateway</html>`)

	err := client.DeleteResource(context.Background(), "/v1/apps/ap-abc123")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Problem != nil {
		t.Fatalf("error = %#v, want HTTPError without Problem", err)
	}
}
