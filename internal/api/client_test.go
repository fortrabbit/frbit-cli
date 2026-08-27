package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientRequiresHTTPSOutsideLoopback(t *testing.T) {
	_, err := NewClient("http://api.fortrabbit.com", "test-token", nil, "frbit/test")
	if err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("error = %v, want HTTPS requirement", err)
	}

	for _, host := range []string{"http://localhost:8085", "http://127.0.0.1:8085", "http://[::1]:8085"} {
		if _, err := NewClient(host, "test-token", nil, "frbit/test"); err != nil {
			t.Fatalf("NewClient(%q): %v", host, err)
		}
	}
}

func TestNewClientRejectsHostCredentialsAndPaths(t *testing.T) {
	const credentialHost = "https://user:password@api.fortrabbit.com"
	_, err := NewClient(credentialHost, "test-token", nil, "frbit/test")
	if err == nil {
		t.Fatalf("NewClient(%q) succeeded", credentialHost)
	}
	if strings.Contains(err.Error(), "password") {
		t.Fatalf("error exposed URL credentials: %v", err)
	}

	for _, host := range []string{
		"https://api.fortrabbit.com/prefix",
		"https://api.fortrabbit.com?debug=true",
		"https://api.fortrabbit.com#fragment",
	} {
		if _, err := NewClient(host, "test-token", nil, "frbit/test"); err == nil {
			t.Fatalf("NewClient(%q) succeeded", host)
		}
	}
}

func TestClientRefusesCrossOriginRedirectWithoutSendingToken(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetCalled = true
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("redirected authorization = %q", authorization)
		}
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/v1/apps", http.StatusFound)
	}))
	defer source.Close()

	client, err := NewClient(source.URL, "test-token", source.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Apps(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "different origin") {
		t.Fatalf("error = %v, want cross-origin redirect refusal", err)
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was called")
	}
}

func TestAppsSendsPublicAPIRequestAndDecodesArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/apps" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("page") != "2" {
			t.Fatalf("page = %q", request.URL.Query().Get("page"))
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[{"publicId":"ap-abc123","name":"Store","description":null,"trial":false,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"}]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Apps(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Apps) != 1 || response.Apps[0].PublicID != "ap-abc123" {
		t.Fatalf("apps = %#v", response.Apps)
	}
	if string(response.Raw) == "" {
		t.Fatal("raw response is empty")
	}
}

func TestAppsDecodesAPIPlatformMemberCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"member":[{"publicId":"ap-abc123","name":"Store","trial":true}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Apps(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Apps) != 1 || !response.Apps[0].Trial {
		t.Fatalf("apps = %#v", response.Apps)
	}
}

func TestAppsReturnsRetryAfterForRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Retry-After", "12")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"detail":"Rate limit exceeded"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Apps(context.Background(), 1)
	var httpError *HTTPError
	if !errors.As(err, &httpError) {
		t.Fatalf("error = %v, want HTTPError", err)
	}
	if httpError.Status != http.StatusTooManyRequests || httpError.RetryAfter != 12*time.Second {
		t.Fatalf("http error = %#v", httpError)
	}
}

func TestHTTPErrorIncludesValidationFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = writer.Write([]byte(`{"errors":{"region":"Unknown region","components":["Missing php plan"]}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateResource(context.Background(), "/v1/apps", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "region: Unknown region") || !strings.Contains(err.Error(), "components") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPErrorIncludesPublicAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":"Cannot delete the last environment in an app."}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	err = client.DeleteResource(context.Background(), "/v1/environments/en-abc123")
	if err == nil || !strings.Contains(err.Error(), "Cannot delete the last environment in an app.") {
		t.Fatalf("error = %v", err)
	}
}

func TestListResourcesSendsFilterAndDecodesHydraCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/environments" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("page") != "3" {
			t.Fatalf("page = %q", request.URL.Query().Get("page"))
		}
		if got := request.URL.Query()["publicId[]"]; len(got) != 2 || got[0] != "en-abc123" || got[1] != "en-def456" {
			t.Fatalf("publicId[] = %#v", got)
		}
		_, _ = writer.Write([]byte(`{"hydra:member":[{"publicId":"en-abc123","name":"production"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ListResources(context.Background(), "/v1/environments", 3, []string{"en-abc123", "en-def456"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Resources) != 1 || response.Resources[0]["name"] != "production" {
		t.Fatalf("resources = %#v", response.Resources)
	}
}

func TestGetResourceDecodesObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/deployments/dp-abc123/logs" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"logs":[{"time":"2026-01-01T00:00:00Z","log":"Build started"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.GetResource(context.Background(), "/v1/deployments/dp-abc123/logs")
	if err != nil {
		t.Fatal(err)
	}
	if response.Resource["logs"] == nil || string(response.Raw) == "" {
		t.Fatalf("response = %#v", response)
	}
}

func TestResourceWritesUseExpectedMethodsAndContentTypes(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		body := &bytes.Buffer{}
		_, _ = body.ReadFrom(request.Body)
		switch requests {
		case 1:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/apps" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			if request.Header.Get("Content-Type") != "application/json" || body.String() != `{"name":"Store"}` {
				t.Fatalf("post content type/body = %q %q", request.Header.Get("Content-Type"), body.String())
			}
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"Store"}`))
		case 2:
			if request.Method != http.MethodPatch || request.URL.Path != "/v1/apps/ap-abc123" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			if request.Header.Get("Content-Type") != "application/merge-patch+json" || body.String() != `{"name":"Shop"}` {
				t.Fatalf("patch content type/body = %q %q", request.Header.Get("Content-Type"), body.String())
			}
			_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"Shop"}`))
		case 3:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/environments/en-abc123/restart" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			if body.Len() != 0 {
				t.Fatalf("action body = %q", body.String())
			}
			writer.WriteHeader(http.StatusAccepted)
		case 4:
			if request.Method != http.MethodDelete || request.URL.Path != "/v1/apps/ap-abc123" {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			if body.Len() != 0 {
				t.Fatalf("delete body = %q", body.String())
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateResource(context.Background(), "/v1/apps", map[string]any{"name": "Store"})
	if err != nil || created.Resource["publicId"] != "ap-abc123" {
		t.Fatalf("create response/error = %#v / %v", created, err)
	}
	updated, err := client.UpdateResource(context.Background(), "/v1/apps/ap-abc123", map[string]any{"name": "Shop"})
	if err != nil || updated.Resource["name"] != "Shop" {
		t.Fatalf("update response/error = %#v / %v", updated, err)
	}
	action, err := client.PostAction(context.Background(), "/v1/environments/en-abc123/restart")
	if err != nil || action.Resource != nil || len(action.Raw) != 0 {
		t.Fatalf("action response/error = %#v / %v", action, err)
	}
	if err := client.DeleteResource(context.Background(), "/v1/apps/ap-abc123"); err != nil {
		t.Fatalf("delete resource: %v", err)
	}
}
