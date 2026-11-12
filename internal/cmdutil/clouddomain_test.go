package cmdutil

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/api"
)

func cloudDomainClient(t *testing.T) *api.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/environments/en-abc123":
			_, _ = writer.Write([]byte(`{"publicId":"en-abc123","domains":["do-ext001","do-cld001"]}`))
		case "/v1/domains/do-ext001":
			_, _ = writer.Write([]byte(`{"publicId":"do-ext001","type":"external","name":"www.example.com"}`))
		case "/v1/domains/do-cld001":
			_, _ = writer.Write([]byte(`{"publicId":"do-cld001","type":"cloud","name":"acme-shop.frbit.cloud"}`))
		default:
			t.Fatalf("unexpected request %s", request.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client, err := api.NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestWriteCloudDomainNamesTheDomainOfANewEnvironment(t *testing.T) {
	var out bytes.Buffer
	created := api.Resource{"publicId": "en-abc123", "domains": []any{"do-ext001", "do-cld001"}}
	if err := WriteCloudDomain(context.Background(), cloudDomainClient(t), &out, created, "en-abc123", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Cloud domain: https://acme-shop.frbit.cloud") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestWriteCloudDomainLooksUpTheEnvironmentOfANewApp(t *testing.T) {
	var out bytes.Buffer
	created := api.Resource{"publicId": "ap-abc123", "environments": []any{"en-abc123"}}
	env := FirstEnvironment(created)
	if err := WriteCloudDomain(context.Background(), cloudDomainClient(t), &out, created, env, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "acme-shop.frbit.cloud") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestWriteCloudDomainExplainsASkippedName(t *testing.T) {
	var out bytes.Buffer
	created := api.Resource{"publicId": "en-abc123", "skippedCloudDomain": map[string]any{"label": "shop", "reason": "taken"}}
	if err := WriteCloudDomain(context.Background(), nil, &out, created, "en-abc123", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"shop" is already taken`) || !strings.Contains(out.String(), "frbit domains create en-abc123 <name> --cloud") {
		t.Fatalf("output = %q", out.String())
	}
}
