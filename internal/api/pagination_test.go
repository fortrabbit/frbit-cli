package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
)

// pagedServer serves 7 apps in pages of 3 with Link and X-Total-Count headers.
func pagedServer(t *testing.T, requests *int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(requests, 1)
		if request.URL.Path != "/v1/apps" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("gitRepository") != "acme/shop" {
			t.Fatalf("filter lost on %s", request.URL)
		}
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		const total, perPage = 7, 3
		var items []string
		for index := (page-1)*perPage + 1; index <= total && index <= page*perPage; index++ {
			items = append(items, fmt.Sprintf(`{"publicId":"ap-%06d"}`, index))
		}
		link := `</v1/apps?gitRepository=acme%2Fshop&page=1>; rel="first"`
		if page*perPage < total {
			link += fmt.Sprintf(`, </v1/apps?gitRepository=acme%%2Fshop&page=%d>; rel="next"`, page+1)
		}
		writer.Header().Set("Link", link)
		writer.Header().Set("X-Total-Count", strconv.Itoa(total))
		writer.Header().Set("Content-Type", "application/json")
		body := "["
		for index, item := range items {
			if index > 0 {
				body += ","
			}
			body += item
		}
		_, _ = writer.Write([]byte(body + "]"))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestListAllFollowsLinkNext(t *testing.T) {
	var requests int32
	server := pagedServer(t, &requests)
	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.ListAll(context.Background(), "/v1/apps", url.Values{"gitRepository": {"acme/shop"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if len(response.Resources) != 7 || response.Resources[6]["publicId"] != "ap-000007" {
		t.Fatalf("resources = %#v", response.Resources)
	}
	if response.TotalItems != 7 {
		t.Fatalf("total = %d, want 7", response.TotalItems)
	}
	if string(response.Raw) == "" || response.Raw[0] != '[' {
		t.Fatalf("raw = %q, want a JSON array", response.Raw)
	}
}

func TestListAllStopsAtLimit(t *testing.T) {
	var requests int32
	server := pagedServer(t, &requests)
	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.ListAll(context.Background(), "/v1/apps", url.Values{"gitRepository": {"acme/shop"}}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(response.Resources) != 4 {
		t.Fatalf("resources = %d, want 4", len(response.Resources))
	}
	if response.TotalItems != 7 {
		t.Fatalf("total = %d, want 7", response.TotalItems)
	}
}

func TestListAllRefusesNextLinkToAnotherOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Link", `<https://evil.example/v1/apps?page=2>; rel="next"`)
		_, _ = writer.Write([]byte(`[{"publicId":"ap-000001"}]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAll(context.Background(), "/v1/apps", nil, 0); err == nil {
		t.Fatal("expected a cross-origin next link to be refused")
	}
}

func TestNextLinkParsesRFC8288Header(t *testing.T) {
	header := `</v1/apps?page=1>; rel="first", </v1/apps?page=3>; rel="next", </v1/apps?page=9>; rel="last"`
	if got := nextLink(header); got != "/v1/apps?page=3" {
		t.Fatalf("next = %q", got)
	}
	if got := nextLink(`</v1/apps?page=1>; rel="first"`); got != "" {
		t.Fatalf("next = %q, want empty", got)
	}
	if got := nextLink(`</a>; rel="prev next"`); got != "/a" {
		t.Fatalf("next = %q", got)
	}
}
