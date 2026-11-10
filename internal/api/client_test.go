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
	client.sleep = func(context.Context, time.Duration) error { return nil }
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
		if request.Header.Get("Accept") != "application/ld+json" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
		_, _ = writer.Write([]byte(`{"hydra:member":[{"publicId":"en-abc123","name":"production"}],"hydra:totalItems":23}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ListResourcesWithTotal(context.Background(), "/v1/environments", 3, []string{"en-abc123", "en-def456"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Resources) != 1 || response.Resources[0]["name"] != "production" {
		t.Fatalf("resources = %#v", response.Resources)
	}
	if response.TotalItems != 23 {
		t.Fatalf("total items = %d, want 23", response.TotalItems)
	}
}

func TestListResourcesUsesMemberCollectionTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"member":[{"publicId":"ap-abc123"}],"totalItems":42}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ListResourcesWithTotal(context.Background(), "/v1/apps", 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Resources) != 1 || response.TotalItems != 42 {
		t.Fatalf("response = %#v", response)
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

func TestCreateSendsSameIdempotencyKeyOnRetry(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		if len(keys) == 1 {
			writer.Header().Set("Retry-After", "3")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"title":"Too Many Requests","status":429,"detail":"try again"}`))
			return
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	var waited []time.Duration
	client.sleep = func(ctx context.Context, wait time.Duration) error {
		waited = append(waited, wait)
		return nil
	}
	response, err := client.CreateResource(context.Background(), "/v1/apps", map[string]any{"name": "Store"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Resource["publicId"] != "ap-abc123" {
		t.Fatalf("resource = %#v", response.Resource)
	}
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("idempotency keys = %#v, want the same non-empty key twice", keys)
	}
	if len(waited) != 1 || waited[0] != 3*time.Second {
		t.Fatalf("waited = %v, want one wait of 3s", waited)
	}
}

func TestCreateDoesNotRetry(t *testing.T) {
	for name, respond := range map[string]func(http.ResponseWriter){
		"5xx": func(writer http.ResponseWriter) { writer.WriteHeader(http.StatusBadGateway) },
		"429 with a long Retry-After": func(writer http.ResponseWriter) {
			writer.Header().Set("Retry-After", "3600")
			writer.WriteHeader(http.StatusTooManyRequests)
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				respond(writer)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
			if err != nil {
				t.Fatal(err)
			}
			client.sleep = func(context.Context, time.Duration) error { return nil }
			if _, err := client.CreateResource(context.Background(), "/v1/apps", map[string]any{}); err == nil {
				t.Fatal("expected an error")
			}
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
		})
	}
}

func TestGetWaitsOutOneRateLimit(t *testing.T) {
	cases := map[string]struct {
		retryAfter string
		wantCalls  int
		wantWait   time.Duration
	}{
		"short Retry-After": {retryAfter: "13", wantCalls: 2, wantWait: 13 * time.Second},
		"long Retry-After":  {retryAfter: "3600", wantCalls: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if calls == 1 {
					writer.Header().Set("Retry-After", tc.retryAfter)
					writer.WriteHeader(http.StatusTooManyRequests)
					return
				}
				_, _ = writer.Write([]byte(`{"publicId":"en-abc123"}`))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
			if err != nil {
				t.Fatal(err)
			}
			var stderr strings.Builder
			client.Warnings = &stderr
			var waited time.Duration
			client.sleep = func(_ context.Context, wait time.Duration) error { waited = wait; return nil }
			_, _ = client.GetResource(context.Background(), "/v1/environments/en-abc123")
			if calls != tc.wantCalls || waited != tc.wantWait {
				t.Fatalf("calls = %d, waited %s", calls, waited)
			}
			if tc.wantCalls == 2 && stderr.String() != "rate limited by the API; retrying in 13s\n" {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestPostWithoutKeyIsNotRetriedOnRateLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.Header().Set("Retry-After", "1")
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := client.UpdateResource(context.Background(), "/v1/environments/en-abc123", map[string]any{}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestCreateRetriesOnlyOnce(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := client.CreateResource(context.Background(), "/v1/apps", map[string]any{}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestCreateWithoutIdempotencyKeySendsNoKeyAndDoesNotRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if key := request.Header.Get("Idempotency-Key"); key != "" {
			t.Fatalf("idempotency key = %q, want none", key)
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := client.DryRunCreate(context.Background(), "/v1/apps", map[string]any{}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestIdempotencyKeyIsRandomHex(t *testing.T) {
	first, err := newIdempotencyKey()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := newIdempotencyKey()
	if len(first) != 32 || first == second || strings.Trim(first, "0123456789abcdef") != "" {
		t.Fatalf("keys = %q, %q", first, second)
	}
}

func TestWarningHeadersGoToStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Add("Warning", `299 - "gitRepository filter is deprecated"`)
		writer.Header().Set("Deprecation", "@1767225600")
		writer.Header().Set("Sunset", "Wed, 01 Jul 2026 00:00:00 GMT")
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	client.Warnings = stderr
	if _, err := client.GetResource(context.Background(), "/v1/apps/ap-abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetResource(context.Background(), "/v1/apps/ap-abc123"); err != nil {
		t.Fatal(err)
	}
	want := "warning: 299 - \"gitRepository filter is deprecated\"\nwarning: this API route is deprecated; update frbit\nwarning: this API route stops working on Wed, 01 Jul 2026 00:00:00 GMT\n"
	if stderr.String() != want {
		t.Fatalf("warnings = %q, want %q", stderr.String(), want)
	}
}

type rotatingTestToken struct{ refreshed bool }

func (s *rotatingTestToken) Token(context.Context) (string, error) { return "old", nil }

func (s *rotatingTestToken) Refresh(context.Context, string) (string, error) {
	s.refreshed = true
	return "new", nil
}

func TestCreateReusesIdempotencyKeyAfterTokenRefresh(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		if request.Header.Get("Authorization") != "Bearer new" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123"}`))
	}))
	defer server.Close()

	tokens := &rotatingTestToken{}
	client, err := NewClientWithTokens(server.URL, tokens, server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateResource(context.Background(), "/v1/apps", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !tokens.refreshed || len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("refreshed = %v, keys = %#v", tokens.refreshed, keys)
	}
}
