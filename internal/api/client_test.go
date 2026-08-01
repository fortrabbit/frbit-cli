package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
